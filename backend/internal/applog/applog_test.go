package applog

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

func TestBufferKeepsWithinLimit(t *testing.T) {
	b := NewBuffer(3)
	for i := 0; i < 10; i++ {
		b.Append(Entry{Level: LevelInfo, Message: string(rune('a' + i))})
	}
	if b.Len() != 3 {
		t.Fatalf("应只保留 3 条，实际 %d", b.Len())
	}
	got := b.Snapshot(0, "")
	// 保留的应是最后 3 条
	want := []string{"h", "i", "j"}
	for i := range want {
		if got[i].Message != want[i] {
			t.Errorf("第 %d 条应为 %q，实际 %q", i, want[i], got[i].Message)
		}
	}
}

// 级别筛选必须先筛再取尾部，顺序反了会出现"筛完不足 limit 条"
func TestSnapshotFiltersBeforeTruncating(t *testing.T) {
	b := NewBuffer(100)
	// 1 条 error 在最前，后面塞 50 条 info
	b.Append(Entry{Level: LevelError, Message: "第一条错误"})
	for i := 0; i < 50; i++ {
		b.Append(Entry{Level: LevelInfo, Message: "普通信息"})
	}
	b.Append(Entry{Level: LevelError, Message: "最后一条错误"})

	got := b.Snapshot(10, LevelError)
	if len(got) != 2 {
		t.Fatalf("应筛出 2 条 error，实际 %d —— 若为 0 说明先截断再筛选", len(got))
	}
	if got[0].Message != "第一条错误" || got[1].Message != "最后一条错误" {
		t.Errorf("筛选结果不对: %+v", got)
	}
}

func TestSnapshotReturnsCopy(t *testing.T) {
	b := NewBuffer(10)
	b.Append(Entry{Level: LevelInfo, Message: "原始"})

	snap := b.Snapshot(0, "")
	snap[0].Message = "被改了"

	again := b.Snapshot(0, "")
	if again[0].Message != "原始" {
		t.Error("Snapshot 应返回副本，调用方改动不得影响内部数据")
	}
}

func TestAppendFillsMissingTime(t *testing.T) {
	b := NewBuffer(10)
	b.Append(Entry{Level: LevelInfo, Message: "无时间"})
	if b.Snapshot(0, "")[0].Time.IsZero() {
		t.Error("缺省时间应被补上，否则前端显示空白")
	}
}

func TestSubscribeAndUnsubscribe(t *testing.T) {
	b := NewBuffer(10)
	var mu sync.Mutex
	got := make([]string, 0, 2)

	unsub := b.Subscribe(func(e Entry) {
		mu.Lock()
		got = append(got, e.Message)
		mu.Unlock()
	})

	b.Append(Entry{Level: LevelInfo, Message: "第一条"})
	unsub()
	b.Append(Entry{Level: LevelInfo, Message: "第二条"})

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "第一条" {
		t.Errorf("取消订阅后不应再收到通知，实际收到 %v", got)
	}
}

func TestSubscribeNilIsSafe(t *testing.T) {
	b := NewBuffer(10)
	unsub := b.Subscribe(nil)
	unsub() // 不得 panic
	b.Append(Entry{Level: LevelInfo, Message: "x"})
}

func TestClear(t *testing.T) {
	b := NewBuffer(10)
	b.Append(Entry{Level: LevelInfo, Message: "x"})
	b.Clear()
	if b.Len() != 0 {
		t.Errorf("Clear 后应为空，实际 %d", b.Len())
	}
	// 清空后仍可继续写入
	b.Append(Entry{Level: LevelInfo, Message: "y"})
	if b.Len() != 1 {
		t.Errorf("Clear 后应能继续写入，实际 %d", b.Len())
	}
}

func TestBufferConcurrent(t *testing.T) {
	b := NewBuffer(100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Append(Entry{Level: LevelInfo, Message: "并发"})
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.Snapshot(10, "")
			_ = b.Len()
		}()
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unsub := b.Subscribe(func(Entry) {})
			unsub()
		}()
	}
	wg.Wait()
	if b.Len() != 50 {
		t.Errorf("应写入 50 条，实际 %d", b.Len())
	}
}

// Writer 必须把 logx 的各级别正确映射到 Entry.Level
func TestWriterMapsLevels(t *testing.T) {
	w, err := New(Options{Limit: 50})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	w.Info("信息")
	w.Error("错误")
	w.Debug("调试")
	w.Slow("慢")
	w.Severe("严重")
	w.Alert("告警")
	w.Stack("堆栈")

	got := w.Buffer().Snapshot(0, "")
	want := []Level{
		LevelInfo, LevelError, LevelDebug, LevelSlow,
		LevelSevere, LevelSevere, LevelSevere,
	}
	if len(got) != len(want) {
		t.Fatalf("应记录 %d 条，实际 %d", len(want), len(got))
	}
	for i := range want {
		if got[i].Level != want[i] {
			t.Errorf("第 %d 条级别应为 %s，实际 %s", i, want[i], got[i].Level)
		}
	}
}

// 默认必须过滤掉 HTTP 访问日志与框架统计：
// go-zero 对每个请求写一条，而前端的实时通道本身就在产生请求，
// 收录后会把业务日志瞬间冲走。
func TestWriterFiltersFrameworkNoiseByDefault(t *testing.T) {
	w, err := New(Options{Limit: 50})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	w.Info("[HTTP] 200 - GET /api/v1/subscriptions - 127.0.0.1:1234 - curl/8.0")
	w.Stat("cpu: 12%, qps: 3")
	w.Info("配置合并完成")

	got := w.Buffer().Snapshot(0, "")
	if len(got) != 1 {
		t.Fatalf("应只保留 1 条业务日志，实际 %d: %+v", len(got), got)
	}
	if got[0].Message != "配置合并完成" {
		t.Errorf("保留的应是业务日志，实际 %q", got[0].Message)
	}
}

// 显式开启时访问日志要能收录（排查请求链路时需要）
func TestWriterCanIncludeAccessLog(t *testing.T) {
	w, err := New(Options{Limit: 50, IncludeAccessLog: true})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	w.Info("[HTTP] 200 - GET /x")
	w.Stat("cpu: 1%")

	if n := w.Buffer().Len(); n != 2 {
		t.Errorf("开启后应收录 2 条，实际 %d", n)
	}
}

// caller 要从 logx 的字段里取出来单独存，而不是拼进消息
func TestWriterExtractsCaller(t *testing.T) {
	w, err := New(Options{Limit: 10})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	w.Error("出错了", logx.Field("caller", "service/config_service.go:123"))

	got := w.Buffer().Snapshot(0, "")
	if len(got) != 1 {
		t.Fatalf("应记录 1 条，实际 %d", len(got))
	}
	if got[0].Caller != "service/config_service.go:123" {
		t.Errorf("caller 应被提取，实际 %q", got[0].Caller)
	}
	if strings.Contains(got[0].Message, "config_service.go") {
		t.Error("caller 不应混进消息正文")
	}
}

// logx 的 Infov 传结构体而非字符串，也要能处理
func TestWriterHandlesNonStringValues(t *testing.T) {
	w, err := New(Options{Limit: 10})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	w.Info(12345)
	w.Error(os.ErrNotExist)

	got := w.Buffer().Snapshot(0, "")
	if len(got) != 2 {
		t.Fatalf("应记录 2 条，实际 %d", len(got))
	}
	if got[0].Message != "12345" {
		t.Errorf("数字应被转成文本，实际 %q", got[0].Message)
	}
	if !strings.Contains(got[1].Message, "not exist") {
		t.Errorf("error 应取 Error()，实际 %q", got[1].Message)
	}
}

func TestWriterSkipsEmptyMessage(t *testing.T) {
	w, _ := New(Options{Limit: 10})
	w.Info("")
	w.Info("\n")
	if n := w.Buffer().Len(); n != 0 {
		t.Errorf("空消息不应入库，实际 %d 条", n)
	}
}

// 落盘：内容要能写进文件且格式可读
func TestFileSinkWritesReadableLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	w, err := New(Options{Limit: 10, FilePath: path})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	w.Error("磁盘写入测试", logx.Field("caller", "a/b.go:1"))
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取日志文件失败: %v", err)
	}
	line := string(data)
	for _, want := range []string{"error", "磁盘写入测试", "a/b.go:1"} {
		if !strings.Contains(line, want) {
			t.Errorf("日志文件应包含 %q，实际内容 %q", want, line)
		}
	}
}

// 多行消息（堆栈）要缩进续行，否则与下一条日志混淆
func TestFileSinkIndentsMultilineMessage(t *testing.T) {
	e := Entry{Time: time.Now(), Level: LevelError, Message: "第一行\n第二行"}
	line := formatLine(e)
	if !strings.Contains(line, "\n    第二行") {
		t.Errorf("续行应缩进，实际 %q", line)
	}
}

// 轮转：超过大小上限要归档并新建，且只保留指定份数
func TestFileSinkRotatesAndPrunes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	// 极小的上限，保证每写几条就轮转
	fs, err := newFileSink(path, 200, 2)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	defer func() { _ = fs.Close() }()

	for i := 0; i < 40; i++ {
		fs.write(Entry{Time: time.Now(), Level: LevelInfo, Message: strings.Repeat("x", 60)})
		// 归档名精确到秒，同秒内轮转会覆盖同名文件，
		// 这里让时间走过至少 1 秒以产生多个归档
		if i%10 == 9 {
			time.Sleep(1100 * time.Millisecond)
		}
	}
	_ = fs.Close()

	archives, err := filepath.Glob(path + ".*")
	if err != nil {
		t.Fatalf("查找归档失败: %v", err)
	}
	if len(archives) == 0 {
		t.Fatal("应产生归档文件，实际一个都没有")
	}
	if len(archives) > 2 {
		t.Errorf("最多保留 2 份归档，实际 %d 份: %v", len(archives), archives)
	}
	// 当前文件必须仍然存在且可写
	if _, err := os.Stat(path); err != nil {
		t.Errorf("轮转后当前日志文件应存在: %v", err)
	}
}

// 落盘路径不可写时降级为仅内存，不能让服务起不来
func TestFileSinkUnwritablePathDegrades(t *testing.T) {
	// 用一个已存在的文件当目录，制造必然失败的路径
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("准备失败: %v", err)
	}

	w, err := New(Options{Limit: 10, FilePath: filepath.Join(blocker, "sub", "app.log")})
	if err == nil {
		t.Skip("该平台允许此路径，跳过")
	}
	// 关键：即使出错也必须返回可用的 Writer（仅内存）
	if w == nil || w.Buffer() == nil {
		t.Fatal("落盘失败时仍须返回可用的内存缓冲")
	}
	w.Info("降级后仍应可记录")
	if w.Buffer().Len() != 1 {
		t.Error("降级后内存缓冲应正常工作")
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]Level{
		"error":  LevelError,
		"ERROR":  LevelError,
		" info ": LevelInfo,
		"severe": LevelSevere,
		"":       "",
		"乱输入":    "",
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) 应为 %q，实际 %q", in, want, got)
		}
	}
}

// 订阅者内重入 Append 不得死锁。
//
// 用原子计数而非 sync.Once 控制"只重入一次"：sync.Once 递归调用自身
// 会永久阻塞（Do 里再 Do 会等第一次的 Do 完成），那会掩盖真正要测的行为。
func TestSubscriberReentrantAppendDoesNotDeadlock(t *testing.T) {
	b := NewBuffer(10)
	var n int32
	done := make(chan struct{})

	b.Subscribe(func(e Entry) {
		if atomic.AddInt32(&n, 1) == 1 {
			b.Append(Entry{Level: LevelInfo, Message: "重入"})
			close(done)
		}
	})

	go b.Append(Entry{Level: LevelInfo, Message: "触发"})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("订阅者内重入 Append 发生死锁")
	}
}

// 回调期间不得持有订阅者锁，否则嵌套 Append 会被等待写锁的 Subscribe 饿死。
//
// Go 的 RWMutex 不可重入且写锁优先：若在 RLock 内遍历回调，而回调里再次
// Append（日志订阅者自己打了日志）、同时另有 Subscribe 在等写锁，
// 嵌套的 RLock 就永久阻塞，整条日志链路卡死。
// 这个场景在生产中罕见但代价极高，故用测试钉住。
func TestNestedAppendNotStarvedBySubscribe(t *testing.T) {
	b := NewBuffer(10)
	inCallback := make(chan struct{})
	release := make(chan struct{})

	b.Subscribe(func(e Entry) {
		select {
		case inCallback <- struct{}{}:
			<-release // 停在回调里
		default:
		}
	})

	go b.Append(Entry{Level: LevelInfo, Message: "第一条"})
	<-inCallback // 确认已进入回调

	// 让一个 Subscribe 排进写锁等待队列
	subDone := make(chan struct{})
	go func() {
		b.Subscribe(func(Entry) {})
		close(subDone)
	}()
	time.Sleep(100 * time.Millisecond)

	// 此时从另一处 Append：不得被上面等待中的写锁阻塞
	nested := make(chan struct{})
	go func() {
		b.Append(Entry{Level: LevelInfo, Message: "嵌套"})
		close(nested)
	}()

	select {
	case <-nested:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("嵌套 Append 被等待写锁的 Subscribe 饿死：回调期间不应持锁")
	}
	close(release)
	<-subDone
}
