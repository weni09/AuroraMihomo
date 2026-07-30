package service

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestReloadRunsAllActionsInOrder(t *testing.T) {
	mgr := NewReloadManager()
	var order []string
	mgr.Register("first", func() error { order = append(order, "first"); return nil })
	mgr.Register("second", func() error { order = append(order, "second"); return nil })

	if err := mgr.Reload(); err != nil {
		t.Fatalf("不应出错: %v", err)
	}
	if strings.Join(order, ",") != "first,second" {
		t.Errorf("执行顺序应与注册顺序一致，实际 %v", order)
	}
}

// 某个动作失败不得中断其余动作：它们相互独立，
// 重装自动更新 Cron 失败不该导致远程拉取 Cron 也不刷新。
func TestReloadContinuesAfterFailure(t *testing.T) {
	mgr := NewReloadManager()
	ran := make([]string, 0, 3)
	mgr.Register("ok-1", func() error { ran = append(ran, "ok-1"); return nil })
	mgr.Register("bad", func() error { ran = append(ran, "bad"); return errors.New("装载失败") })
	mgr.Register("ok-2", func() error { ran = append(ran, "ok-2"); return nil })

	err := mgr.Reload()
	if err == nil {
		t.Fatal("应返回错误")
	}
	if len(ran) != 3 {
		t.Errorf("失败后仍应执行后续动作，实际执行 %v", ran)
	}
	// 错误信息要能定位到具体哪一项
	if !strings.Contains(err.Error(), "bad") || !strings.Contains(err.Error(), "装载失败") {
		t.Errorf("错误应包含动作名与原因，实际 %q", err.Error())
	}
}

// 多个动作同时失败时要一次性全部报出，而不是只报第一个——
// 否则用户修完一个才发现还有下一个。
func TestReloadJoinsAllErrors(t *testing.T) {
	mgr := NewReloadManager()
	mgr.Register("a", func() error { return errors.New("错误A") })
	mgr.Register("b", func() error { return errors.New("错误B") })

	err := mgr.Reload()
	if err == nil {
		t.Fatal("应返回错误")
	}
	for _, want := range []string{"错误A", "错误B"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误应包含 %q，实际 %q", want, err.Error())
		}
	}
}

func TestReloadStats(t *testing.T) {
	mgr := NewReloadManager()
	mgr.Register("noop", func() error { return nil })

	if n, last := mgr.Stats(); n != 0 || !last.IsZero() {
		t.Errorf("初始应为 0 次且无时间，实际 %d / %v", n, last)
	}
	_ = mgr.Reload()
	_ = mgr.Reload()
	n, last := mgr.Stats()
	if n != 2 {
		t.Errorf("应记录 2 次，实际 %d", n)
	}
	if last.IsZero() {
		t.Error("应记录最后一次重载时间")
	}
}

// 没有注册任何动作时 Reload 不能 panic（启动早期就可能被调用）
func TestReloadWithNoActions(t *testing.T) {
	mgr := NewReloadManager()
	if err := mgr.Reload(); err != nil {
		t.Errorf("空动作列表不应出错: %v", err)
	}
}

func TestRegisterIgnoresNil(t *testing.T) {
	mgr := NewReloadManager()
	mgr.Register("nil-fn", nil)
	if err := mgr.Reload(); err != nil {
		t.Errorf("nil 动作应被忽略: %v", err)
	}
}

// RequestQuit 必须幂等：用户连点两次「重启」不该让关停流程跑两遍，
// 否则会出现重复关闭数据库等重入问题。
func TestRequestQuitIsIdempotent(t *testing.T) {
	mgr := NewReloadManager()
	mgr.RequestQuit("第一次")
	mgr.RequestQuit("第二次")

	select {
	case <-mgr.QuitRequested():
	default:
		t.Fatal("QuitRequested 通道应已关闭")
	}
	if got := mgr.QuitReason(); got != "第一次" {
		t.Errorf("应保留首次原因，实际 %q", got)
	}
}

func TestQuitNotRequestedByDefault(t *testing.T) {
	mgr := NewReloadManager()
	select {
	case <-mgr.QuitRequested():
		t.Fatal("未请求退出时通道不应关闭")
	default:
	}
	if mgr.QuitReason() != "" {
		t.Errorf("未请求时原因应为空，实际 %q", mgr.QuitReason())
	}
}

// 并发触发不得 panic 或死锁：HTTP 接口与 SIGHUP 可能同时到来。
func TestConcurrentReloadAndQuit(t *testing.T) {
	mgr := NewReloadManager()
	var hits int
	var mu sync.Mutex
	mgr.Register("count", func() error {
		mu.Lock()
		hits++
		mu.Unlock()
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mgr.Reload()
		}()
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.RequestQuit("并发")
		}()
	}
	wg.Wait()

	mu.Lock()
	got := hits
	mu.Unlock()
	if got != 20 {
		t.Errorf("20 次并发重载应各执行一次，实际 %d", got)
	}
	if n, _ := mgr.Stats(); n != 20 {
		t.Errorf("计数应为 20，实际 %d", n)
	}
}
