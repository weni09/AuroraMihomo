package realtime

import (
	"sync"
	"testing"
	"time"
)

// 并发订阅 / 发布 / 退订，用 -race 检测数据竞争与向已关闭 channel 写入
func TestHubConcurrentPublishSubscribe(t *testing.T) {
	h := NewHub()
	var wg sync.WaitGroup

	stop := make(chan struct{})
	// 持续发布
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					h.Publish("tick", n)
				}
			}
		}(i)
	}

	// 反复订阅后立即退订，制造 close 与 send 的竞争窗口
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				ch, cancel := h.Subscribe(1)
				select {
				case <-ch:
				default:
				}
				cancel()
			}
		}()
	}

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	if got := h.SubscriberCount(); got != 0 {
		t.Fatalf("全部退订后订阅者应为 0，实际 %d", got)
	}
}

// 重复调用退订函数不应 panic（close of closed channel）
func TestHubDoubleUnsubscribe(t *testing.T) {
	h := NewHub()
	_, cancel := h.Subscribe(1)
	cancel()
	cancel() // 第二次必须是空操作
}

// 慢消费者不应阻塞发布
func TestHubSlowConsumerDoesNotBlock(t *testing.T) {
	h := NewHub()
	_, cancel := h.Subscribe(1)
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			h.Publish("flood", i)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("慢消费者阻塞了发布方")
	}
}

// Close 让所有订阅通道关闭，订阅方的读循环由此自然退出。
//
// 这是关停流程的必要条件：WebSocket 连接被 hijack 后已从
// http.Server.activeConn 中移除，Shutdown 既不等它们也不唤醒它们，
// 只能靠 Hub 关闭通道来通知。
func TestHubCloseReleasesSubscribers(t *testing.T) {
	h := NewHub()
	ch1, _ := h.Subscribe(4)
	ch2, _ := h.Subscribe(4)

	h.Close()

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case _, ok := <-ch:
			if ok {
				t.Errorf("订阅者 %d 的通道应已关闭", i+1)
			}
		case <-time.After(time.Second):
			t.Errorf("订阅者 %d 的通道未关闭，读循环会挂到进程退出", i+1)
		}
	}
	if n := h.SubscriberCount(); n != 0 {
		t.Errorf("Close 后订阅数应为 0，实际 %d", n)
	}
}

// 重复 Close 不得 panic：关停可能被重复触发（信号与 HTTP 接口同时到达）。
//
// 注意单靠"不 panic"拦不住 bug——Close 先 delete 再 close，第二次遍历的
// 已是空 map，即使去掉 closed 标记也不会 panic。closed 标记真正保护的是
// Close 之后的 Subscribe，故这里一并断言标记仍生效，
// 否则本测试就是个永远通过的空壳。
func TestHubCloseIsIdempotent(t *testing.T) {
	h := NewHub()
	h.Subscribe(4)

	h.Close()
	h.Close() // 不得 panic

	ch, _ := h.Subscribe(4)
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("重复 Close 后 closed 标记应仍生效")
		}
	case <-time.After(time.Second):
		t.Error("重复 Close 后 closed 标记失效，新订阅拿到了未关闭的通道")
	}
}

// Close 之后仍可能有请求在建立 WS（Shutdown 与 Close 之间存在窗口）。
// 此时应返回一个已关闭的通道，让订阅方立刻退出，
// 而不是挂在一个永远收不到事件的通道上。
func TestSubscribeAfterCloseReturnsClosedChannel(t *testing.T) {
	h := NewHub()
	h.Close()

	ch, unsub := h.Subscribe(4)
	defer unsub() // 不得 panic

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("Close 后订阅应直接得到已关闭的通道")
		}
	case <-time.After(time.Second):
		t.Error("Close 后订阅拿到的通道未关闭")
	}
	if n := h.SubscriberCount(); n != 0 {
		t.Errorf("不应把 Close 后的订阅计入，实际 %d", n)
	}
}

// Close 后 Publish 不得 panic（关停期间仍可能有代码发事件）
func TestPublishAfterCloseIsSafe(t *testing.T) {
	h := NewHub()
	h.Subscribe(4)
	h.Close()

	h.Publish("evt", map[string]any{"a": 1}) // 不得 panic
	_ = h.PublishJSON("evt", nil)
}

// 并发 Close 与 Subscribe/Publish 不得 panic 或死锁
func TestHubCloseConcurrent(t *testing.T) {
	h := NewHub()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, unsub := h.Subscribe(2)
			_ = ch
			unsub()
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Publish("evt", nil)
		}()
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Close()
		}()
	}
	wg.Wait()
}
