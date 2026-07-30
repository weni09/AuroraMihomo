package applog

import (
	"fmt"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// Writer 实现 logx.Writer，把 go-zero 的日志同时送进内存缓冲与文件。
//
// 为什么实现完整接口而不是 logx.NewWriter(io.Writer)：
// 后者拿到的是已序列化的整行（默认为 JSON），级别只能靠解析字符串还原，
// 多一次编解码且容易随 logx 的格式变动而失效。实现 Writer 接口能直接
// 从方法名得到级别、从 fields 里取 caller，无需解析。
//
// 注册方式是 logx.AddWriter 而非 SetWriter：AddWriter 会把已有的
// console writer 与本 writer 组合（comboWriter），控制台输出得以保留。
// 必须在 rest.MustNewServer 之后调用——logx.SetUp 用 setupOnce 保护，
// 且它会直接覆盖 writer，早于它注册会被丢掉。
type Writer struct {
	buf  *Buffer
	file *fileSink

	// includeAccessLog 决定是否收录 go-zero 的 HTTP 访问日志与框架统计。
	// 默认关闭：go-zero 对每个请求写一条 Info，而前端的 WS 心跳与每 5 秒的
	// 状态推送本身就在产生请求，收录后满屏都是访问记录，真正的业务日志
	// 会被瞬间冲走（前端只保留最近若干条）。
	includeAccessLog bool
}

// Options 配置 Writer。
type Options struct {
	// Limit 是内存缓冲的条数上限，<=0 用 DefaultLimit
	Limit int
	// FilePath 为空表示不落盘
	FilePath string
	// MaxFileBytes 单个文件的大小上限，超过即轮转，<=0 用默认值
	MaxFileBytes int64
	// MaxBackups 保留的历史文件份数
	MaxBackups int
	// IncludeAccessLog 是否收录 HTTP 访问日志与框架统计日志
	IncludeAccessLog bool
}

// New 创建 Writer。落盘失败不视为致命错误：日志查看是辅助功能，
// 不该因为磁盘不可写就让整个服务起不来，降级为仅内存缓冲即可。
func New(opts Options) (*Writer, error) {
	w := &Writer{
		buf:              NewBuffer(opts.Limit),
		includeAccessLog: opts.IncludeAccessLog,
	}
	if opts.FilePath != "" {
		fs, err := newFileSink(opts.FilePath, opts.MaxFileBytes, opts.MaxBackups)
		if err != nil {
			return w, fmt.Errorf("应用日志落盘初始化失败，将仅保留内存缓冲: %w", err)
		}
		w.file = fs
	}
	return w, nil
}

// Buffer 暴露内存缓冲，供 HTTP 接口与 Hub 桥接使用。
func (w *Writer) Buffer() *Buffer { return w.buf }

// FilePath 返回落盘路径，未启用落盘时为空串。
// 供定时清理任务定位归档文件。
func (w *Writer) FilePath() string {
	if w.file == nil {
		return ""
	}
	return w.file.path
}

func (w *Writer) Info(v any, fields ...logx.LogField)  { w.record(LevelInfo, v, fields) }
func (w *Writer) Debug(v any, fields ...logx.LogField) { w.record(LevelDebug, v, fields) }
func (w *Writer) Error(v any, fields ...logx.LogField) { w.record(LevelError, v, fields) }
func (w *Writer) Slow(v any, fields ...logx.LogField)  { w.record(LevelSlow, v, fields) }
func (w *Writer) Stat(v any, fields ...logx.LogField)  { w.record(LevelStat, v, fields) }
func (w *Writer) Alert(v any)                          { w.record(LevelSevere, v, nil) }
func (w *Writer) Severe(v any)                         { w.record(LevelSevere, v, nil) }
func (w *Writer) Stack(v any)                          { w.record(LevelSevere, v, nil) }

// Close 关闭落盘文件。logx.Close() 会通过 io.Closer 断言调到这里。
func (w *Writer) Close() error {
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// record 是唯一的写入入口。
//
// 注意：本方法内绝不能调用 logx 的任何函数。落盘错误只能静默丢弃或
// 写 stderr——调 logx.Errorf 会让日志写入自我触发，形成无限递归。
func (w *Writer) record(level Level, v any, fields []logx.LogField) {
	msg := toMessage(v)
	if msg == "" {
		return
	}
	if !w.includeAccessLog && isFrameworkNoise(level, msg) {
		return
	}

	e := Entry{
		Time:    time.Now(),
		Level:   level,
		Message: msg,
		Caller:  callerOf(fields),
	}

	// 先落盘再进内存：落盘是本地写入且有缓冲，失败也不影响内存部分
	if w.file != nil {
		w.file.write(e)
	}
	w.buf.Append(e)
}

// toMessage 把 logx 传来的任意值转成一行文本。
//
// logx 的 Infof/Errorf 等在调用 Writer 前已完成格式化，传进来就是 string；
// Infov/Errorv 传的是原始结构体。两种都要能处理。
func toMessage(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimRight(t, "\n")
	case error:
		return t.Error()
	case fmt.Stringer:
		return t.String()
	default:
		return strings.TrimRight(fmt.Sprint(v), "\n")
	}
}

// callerOf 从 logx 附带的字段里取调用位置。
// logx 用 addCaller 注入名为 "caller" 的字段（core/logx/logs.go）。
func callerOf(fields []logx.LogField) string {
	for _, f := range fields {
		if f.Key != "caller" {
			continue
		}
		if s, ok := f.Value.(string); ok {
			return s
		}
		return fmt.Sprint(f.Value)
	}
	return ""
}

// isFrameworkNoise 判断是否为框架自身产生的高频日志。
//
// 两类：
//   - HTTP 访问日志：go-zero 的 loghandler 对每个请求写一条 Info，
//     格式形如 `[HTTP] 200 - GET /api/... - 127.0.0.1:1234 - curl/8.0`
//   - stat 日志：每分钟一条 CPU/QPS 统计，级别就是 stat
//
// 判据刻意只看前缀而非正则：这些格式由 go-zero 决定，一旦它改了格式，
// 过滤失效的表现是"访问日志重新出现"，而不是业务日志被误删。
func isFrameworkNoise(level Level, msg string) bool {
	if level == LevelStat {
		return true
	}
	return strings.HasPrefix(msg, "[HTTP]") || strings.HasPrefix(msg, "[RPC]")
}
