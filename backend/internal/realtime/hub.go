package realtime

import (
	"encoding/json"
	"sync"
	"time"
)

type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
	At   time.Time   `json:"at"`
}

type Hub struct {
	mu     sync.RWMutex
	subs   map[int]chan Event
	seq    int
	closed bool
}

func NewHub() *Hub {
	return &Hub{subs: map[int]chan Event{}}
}

func (h *Hub) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 32
	}
	ch := make(chan Event, buffer)
	h.mu.Lock()
	// 已关停后仍可能有请求在建立 WS（Shutdown 与 Hub.Close 之间有窗口）。
	// 直接返回一个已关闭的通道，让订阅方的 `ev, ok := <-ch` 立刻拿到 !ok
	// 并退出，而不是挂在一个永远收不到事件的通道上。
	if h.closed {
		h.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	h.seq++
	id := h.seq
	h.subs[id] = ch
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if c, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(c)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) Publish(eventType string, data interface{}) {
	ev := Event{Type: eventType, Data: data, At: time.Now()}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.subs {
		select {
		case ch <- ev:
		default:
			// drop if slow consumer
		}
	}
}

// Close 关闭所有订阅通道，让订阅方的读循环自然退出。
//
// 关停时必需：WebSocket 连接被 hijack 后，标准库会把它从 activeConn 中移除
// （net/http/server.go 的 StateHijacked 分支），因此 http.Server.Shutdown
// 既不等待它们、也不会唤醒它们。没有这个方法的话，WS 的读写 goroutine 会
// 一直挂到进程退出，浏览器那侧也收不到任何关闭提示。
//
// 幂等：第一次调用后 subs 已被清空，且 closed 为 true 使 Subscribe 不再
// 往里添加，因此后续调用遍历的是空 map，不会重复 close 同一个通道。
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for id, ch := range h.subs {
		delete(h.subs, id)
		close(ch)
	}
}

func (h *Hub) PublishJSON(eventType string, data interface{}) []byte {
	h.Publish(eventType, data)
	b, _ := json.Marshal(Event{Type: eventType, Data: data, At: time.Now()})
	return b
}

// SubscriberCount 返回当前订阅者数量，用于监控与测试
func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}
