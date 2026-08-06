package service

import (
	"testing"
)

// 未持久化任何监控设置时应回落到默认值：开关开启、间隔 3s
func TestMonitorSettingsDefaults(t *testing.T) {
	svc, _ := newTestSettingsService(t)
	if !svc.MonitorEnabled() {
		t.Fatal("默认应开启资源监控")
	}
	if got := svc.MonitorIntervalSec(); got != 3 {
		t.Fatalf("默认刷新间隔应为 3s，实际 %d", got)
	}
}

// 通过 Update 持久化开关与间隔后应能读回，且不影响其它设置
func TestMonitorSettingsUpdatePersists(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	enabled := false
	interval := 10
	if _, err := svc.Update(UpdateSettingsInput{
		MonitorEnabled:     &enabled,
		MonitorIntervalSec: &interval,
	}); err != nil {
		t.Fatalf("更新监控设置失败: %v", err)
	}
	if svc.MonitorEnabled() {
		t.Fatal("开关应已关闭")
	}
	if got := svc.MonitorIntervalSec(); got != 10 {
		t.Fatalf("刷新间隔应为 10s，实际 %d", got)
	}
}

// 只改间隔时不应动开关，反之亦然（nil 表示不修改）
func TestMonitorSettingsPartialUpdate(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	enabled := false
	if _, err := svc.Update(UpdateSettingsInput{MonitorEnabled: &enabled}); err != nil {
		t.Fatalf("更新开关失败: %v", err)
	}
	interval := 30
	if _, err := svc.Update(UpdateSettingsInput{MonitorIntervalSec: &interval}); err != nil {
		t.Fatalf("更新间隔失败: %v", err)
	}
	if svc.MonitorEnabled() {
		t.Fatal("仅改间隔不应改变开关状态")
	}
	if got := svc.MonitorIntervalSec(); got != 30 {
		t.Fatalf("刷新间隔应为 30s，实际 %d", got)
	}
}

// 非法档位（如 2、7）应回退默认 3s，而不是持久化一个界面选不出来的值
func TestMonitorSettingsInvalidIntervalFallsBack(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	bad := 7
	if _, err := svc.Update(UpdateSettingsInput{MonitorIntervalSec: &bad}); err != nil {
		t.Fatalf("更新非法间隔不应报错: %v", err)
	}
	if got := svc.MonitorIntervalSec(); got != 3 {
		t.Fatalf("非法间隔应回退 3s，实际 %d", got)
	}
}

// 合法档位判定：1/3/5/10/30 为真，其余为假
func TestValidMonitorInterval(t *testing.T) {
	for _, ok := range MonitorIntervalOptions {
		if !validMonitorInterval(ok) {
			t.Fatalf("档位 %d 应合法", ok)
		}
	}
	for _, bad := range []int{0, 2, 4, 6, 15, 60, -1} {
		if validMonitorInterval(bad) {
			t.Fatalf("档位 %d 应非法", bad)
		}
	}
}
