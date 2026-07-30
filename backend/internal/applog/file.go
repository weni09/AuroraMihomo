package applog

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 落盘的默认参数。
//
// 单文件 8MB × 保留 5 份 = 最多约 40MB，与项目里配置备份"保留 10 份"的
// 克制程度相当。日志比配置更容易膨胀，故份数少一些。
const (
	defaultMaxFileBytes int64 = 8 << 20 // 8MB
	defaultMaxBackups         = 5
)

// fileSink 把日志按行写入文件，超过大小上限时轮转。
//
// 为什么自己实现而不用 logx 的 file 模式：logx 的 Mode: file 会接管
// 全部输出并按天切分，且会把控制台输出也一并转走（开发时看不到日志了）。
// 这里要的是"控制台照常 + 另存一份可回溯的文件"，与 logx 的模式不重叠。
// 需求也简单（追加写 + 按大小轮转），不值得引入额外依赖。
type fileSink struct {
	mu sync.Mutex

	path       string
	maxBytes   int64
	maxBackups int

	f    *os.File
	w    *bufio.Writer
	size int64

	// lastFlush 用于限制 flush 频率：每条都 flush 会让高频日志变成
	// 逐行同步写盘，明显拖慢业务；完全不 flush 则崩溃时丢最近的日志。
	lastFlush time.Time
}

// flushInterval 是缓冲刷盘的间隔。取 1 秒是折中：
// 崩溃最多丢 1 秒日志，而正常情况下写盘次数被压到每秒一次。
const flushInterval = time.Second

func newFileSink(path string, maxBytes int64, maxBackups int) (*fileSink, error) {
	if maxBytes <= 0 {
		maxBytes = defaultMaxFileBytes
	}
	if maxBackups < 0 {
		maxBackups = defaultMaxBackups
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}
	fs := &fileSink{path: path, maxBytes: maxBytes, maxBackups: maxBackups}
	if err := fs.open(); err != nil {
		return nil, err
	}
	return fs, nil
}

func (fs *fileSink) open() error {
	f, err := os.OpenFile(fs.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("读取日志文件信息失败: %w", err)
	}
	fs.f = f
	fs.w = bufio.NewWriterSize(f, 32<<10)
	fs.size = st.Size()
	return nil
}

// write 追加一行。
//
// 所有错误都被丢弃，这是有意的：本方法由 logx.Writer 调用，
// 而 logx.Writer 内绝不能再调 logx（会无限递归）。磁盘写失败时
// 内存缓冲仍然可用，前端照样能看日志，不该为此打扰主流程。
func (fs *fileSink) write(e Entry) {
	line := formatLine(e)

	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.w == nil {
		return
	}
	if fs.size+int64(len(line)) > fs.maxBytes {
		fs.rotateLocked()
	}
	n, err := fs.w.WriteString(line)
	fs.size += int64(n)
	if err != nil {
		return
	}
	// 错误级别立即刷盘：这类日志往往紧跟着崩溃或退出，
	// 留在缓冲里就等于丢了最关键的那几行
	if e.Level == LevelError || e.Level == LevelSevere ||
		time.Since(fs.lastFlush) >= flushInterval {
		_ = fs.w.Flush()
		fs.lastFlush = time.Now()
	}
}

// formatLine 用固定宽度的纯文本，便于人工阅读与 grep。
// 不用 JSON：这份文件的用途是出问题时人工翻看，
// 结构化查询场景由内存缓冲经接口提供。
func formatLine(e Entry) string {
	var b strings.Builder
	b.WriteString(e.Time.Format("2006-01-02 15:04:05.000"))
	b.WriteString(" [")
	// 级别左对齐补齐到 6 字符，让正文起始位置对齐
	lv := string(e.Level)
	b.WriteString(lv)
	for i := len(lv); i < 6; i++ {
		b.WriteByte(' ')
	}
	b.WriteString("] ")
	if e.Caller != "" {
		b.WriteString(e.Caller)
		b.WriteString(" ")
	}
	// 多行消息（如堆栈）缩进续行，避免与下一条日志混淆
	b.WriteString(strings.ReplaceAll(e.Message, "\n", "\n    "))
	b.WriteByte('\n')
	return b.String()
}

// rotateLocked 把当前文件改名归档并新建。调用方须持有 fs.mu。
func (fs *fileSink) rotateLocked() {
	if fs.w != nil {
		_ = fs.w.Flush()
	}
	if fs.f != nil {
		_ = fs.f.Close()
	}

	// 带时间戳归档而非 .1/.2 逐级改名：逐级改名在份数多时要做 N 次
	// rename，且中断时容易留下空洞；时间戳命名天然有序、便于定位。
	archived := fmt.Sprintf("%s.%s", fs.path, time.Now().Format("20060102-150405"))
	if err := os.Rename(fs.path, archived); err != nil {
		// 改名失败（文件被占用等）时截断重开，宁可丢历史也不能停止记录
		_ = os.Truncate(fs.path, 0)
	}
	fs.pruneLocked()

	if err := fs.open(); err != nil {
		// 打不开就停止落盘，内存缓冲继续工作
		fs.f, fs.w = nil, nil
	}
}

// pruneLocked 只保留最近 maxBackups 份归档。
func (fs *fileSink) pruneLocked() {
	if fs.maxBackups <= 0 {
		return
	}
	pattern := fs.path + ".*"
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) <= fs.maxBackups {
		return
	}
	// 文件名含时间戳，字典序即时间序
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-fs.maxBackups] {
		_ = os.Remove(old)
	}
}

func (fs *fileSink) Close() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.w != nil {
		_ = fs.w.Flush()
	}
	if fs.f != nil {
		err := fs.f.Close()
		fs.f, fs.w = nil, nil
		return err
	}
	return nil
}
