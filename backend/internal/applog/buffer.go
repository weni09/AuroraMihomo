// Package applog 采集本项目自身的运行日志（go-zero logx 的输出），
// 缓存在内存供前端查看，并按需落盘。
//
// 与内核日志的关系：mihomo 的 stdout/stderr 由 internal/mihomo 的
// ProcessManager 采集，走 "log.message" 事件；本包采集的是应用自身日志，
// 走 "applog.message" 事件。两者刻意分开——混在一条流里会让前端无法
// 区分"内核说的"和"本程序说的"，排查时反而更费劲。
package applog

import (
	"strings"
	"sync"
	"time"
)

// Level 是应用日志级别，取值与 logx 的级别一一对应。
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelError Level = "error"
	LevelSlow  Level = "slow"
	LevelStat  Level = "stat"
	// LevelSevere 对应 logx 的 severe/alert/stack，都是需要立刻关注的严重问题
	LevelSevere Level = "severe"
)

// Entry 是一条应用日志。
//
// 字段与 mihomo.LogLine 保持相近的形状（时间 + 分类 + 正文），
// 让前端能用同一套渲染逻辑，只是把 Stream 换成语义更准的 Level。
type Entry struct {
	Time    time.Time `json:"time"`
	Level   Level     `json:"level"`
	Message string    `json:"message"`
	// Caller 是 logx 附带的调用位置（形如 service/config_service.go:123），
	// 排查时比消息本身更能定位问题，故单独存一列而不是拼进 Message。
	Caller string `json:"caller,omitempty"`
}

// Listener 在每条新日志产生时被调用。
type Listener func(Entry)

// Buffer 是带上限的内存日志缓冲，并支持订阅。
//
// 实现方式与 mihomo.ProcessManager 的日志缓冲一致（append 后裁剪，
// 而非真正的环形数组）：日志条数上限不大，裁剪的拷贝成本可忽略，
// 换来的是 Snapshot 可以直接切片、不必处理绕回。
type Buffer struct {
	mu      sync.RWMutex
	entries []Entry
	limit   int

	// 订阅者单独一把锁：投递时不该持有 entries 的锁，
	// 否则一个慢订阅者会阻塞所有日志写入
	subsMu sync.RWMutex
	subs   map[int]Listener
	subSeq int
}

// DefaultLimit 与内核日志缓冲保持一致的量级。
// 应用日志的单条内容通常比内核日志长（带 caller 与格式化后的错误），
// 但 1000 条仍在几百 KB 量级，可接受。
const DefaultLimit = 1000

func NewBuffer(limit int) *Buffer {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return &Buffer{
		entries: make([]Entry, 0, limit),
		limit:   limit,
		subs:    map[int]Listener{},
	}
}

// Append 记录一条日志并通知订阅者。
//
// 关键约束：本方法及其调用的任何代码都不得再打日志。
// 日志写入 → 通知订阅者 → 订阅者打日志 → 再次写入，会形成无限递归。
// 订阅者（见 Hub 桥接处）同样受此约束。
func (b *Buffer) Append(e Entry) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}

	b.mu.Lock()
	b.entries = append(b.entries, e)
	if len(b.entries) > b.limit {
		b.entries = b.entries[len(b.entries)-b.limit:]
	}
	b.mu.Unlock()

	// 先把订阅者拷出来，回调期间不持任何锁。
	//
	// 不能像 mihomo.ProcessManager 那样在 RLock 内直接遍历回调：
	// Go 的 RWMutex 不可重入，且写锁优先——回调里若再次触发 Append
	// （日志订阅者自己打了日志），而此时恰有 Subscribe 在等写锁，
	// 嵌套的 RLock 会被永久饿死，整个日志链路卡住。
	// 已用测试固定这一行为（TestNestedAppendNotStarvedBySubscribe）。
	b.subsMu.RLock()
	fns := make([]Listener, 0, len(b.subs))
	for _, fn := range b.subs {
		fns = append(fns, fn)
	}
	b.subsMu.RUnlock()

	for _, fn := range fns {
		fn(e)
	}
}

// Snapshot 返回最近 limit 条日志（副本）。limit <= 0 表示全部。
//
// 可按级别过滤：level 为空时不过滤。
func (b *Buffer) Snapshot(limit int, level Level) []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// 先按级别筛，再取尾部——顺序反了会导致"筛完不足 limit 条"
	src := b.entries
	if level != "" {
		filtered := make([]Entry, 0, len(src))
		for _, e := range src {
			if e.Level == level {
				filtered = append(filtered, e)
			}
		}
		src = filtered
	}

	if limit > 0 && len(src) > limit {
		src = src[len(src)-limit:]
	}
	out := make([]Entry, len(src))
	copy(out, src)
	return out
}

// Subscribe 注册监听器，返回取消订阅的函数。
func (b *Buffer) Subscribe(fn Listener) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	b.subsMu.Lock()
	b.subSeq++
	id := b.subSeq
	b.subs[id] = fn
	b.subsMu.Unlock()

	return func() {
		b.subsMu.Lock()
		delete(b.subs, id)
		b.subsMu.Unlock()
	}
}

// Clear 清空缓冲，供界面上的"清空"操作使用。
// 不影响已落盘的日志文件。
func (b *Buffer) Clear() {
	b.mu.Lock()
	b.entries = b.entries[:0]
	b.mu.Unlock()
}

// Len 返回当前缓存的条数，用于监控与测试。
func (b *Buffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.entries)
}

// ParseLevel 把外部传入的级别字符串规范化，无法识别时返回空（表示不过滤）。
func ParseLevel(s string) Level {
	switch Level(strings.ToLower(strings.TrimSpace(s))) {
	case LevelDebug:
		return LevelDebug
	case LevelInfo:
		return LevelInfo
	case LevelError:
		return LevelError
	case LevelSlow:
		return LevelSlow
	case LevelStat:
		return LevelStat
	case LevelSevere:
		return LevelSevere
	default:
		return ""
	}
}
