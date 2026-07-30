package service

import (
	"strings"
	"testing"
)

func TestLogCleanupDefaults(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	if got := svc.LogCleanupCron(); got != DefaultLogCleanupCron {
		t.Errorf("未设置时应返回默认调度 %q，实际 %q", DefaultLogCleanupCron, got)
	}
	if !svc.LogCleanupEnabled() {
		t.Error("未设置时应默认启用定时清理")
	}
}

func TestLogCleanupCronPersists(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	if _, err := svc.Update(UpdateSettingsInput{LogCleanupCron: "0 0 5 * * *"}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if got := svc.LogCleanupCron(); got != "0 0 5 * * *" {
		t.Errorf("应读回 %q，实际 %q", "0 0 5 * * *", got)
	}
}

// 5 段表达式要被补成 6 段（robfig/cron 以秒开头），
// 否则用户按 Linux crontab 习惯填 5 段会被判非法。
func TestLogCleanupCronAcceptsFiveFields(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	if _, err := svc.Update(UpdateSettingsInput{LogCleanupCron: "30 3 * * *"}); err != nil {
		t.Fatalf("5 段表达式应被接受: %v", err)
	}
	if got := svc.LogCleanupCron(); got != "0 30 3 * * *" {
		t.Errorf("应补成 6 段 %q，实际 %q", "0 30 3 * * *", got)
	}
}

// 非法表达式必须在落库前被拒，否则会留下一个界面显示、实际装不上的坏值
func TestLogCleanupCronRejectsInvalid(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	before := svc.LogCleanupCron()
	_, err := svc.Update(UpdateSettingsInput{LogCleanupCron: "不是 cron 表达式"})
	if err == nil {
		t.Fatal("非法表达式应返回错误")
	}
	if !strings.Contains(err.Error(), "日志清理") {
		t.Errorf("错误信息应指明是日志清理的表达式，实际 %q", err.Error())
	}
	if got := svc.LogCleanupCron(); got != before {
		t.Errorf("被拒的值不得落库，应仍为 %q，实际 %q", before, got)
	}
}

func TestLogCleanupCanBeDisabled(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	off := false
	if _, err := svc.Update(UpdateSettingsInput{LogCleanupEnabled: &off}); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	if svc.LogCleanupEnabled() {
		t.Error("应已禁用")
	}

	on := true
	if _, err := svc.Update(UpdateSettingsInput{LogCleanupEnabled: &on}); err != nil {
		t.Fatalf("重新启用失败: %v", err)
	}
	if !svc.LogCleanupEnabled() {
		t.Error("应已重新启用")
	}
}

// 数据库里的值被手工改坏时，回退默认值而不是让清理彻底停摆
func TestLogCleanupCronFallsBackOnCorruptedValue(t *testing.T) {
	svc, db := newTestSettingsService(t)

	if err := db.SetSetting("applog.cleanup_cron", "garbage garbage"); err != nil {
		t.Fatalf("准备失败: %v", err)
	}
	if got := svc.LogCleanupCron(); got != DefaultLogCleanupCron {
		t.Errorf("坏值应回退默认调度 %q，实际 %q", DefaultLogCleanupCron, got)
	}
}

// 改动调度后必须触发重装，否则新表达式要等到重启才生效
func TestUpdateTriggersScheduleReload(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	var gotEnabled bool
	var gotCron string
	calls := 0
	svc.SetLogCleanupReloadFunc(func(enabled bool, cronExpr string) error {
		calls++
		gotEnabled, gotCron = enabled, cronExpr
		return nil
	})

	if _, err := svc.Update(UpdateSettingsInput{LogCleanupCron: "0 0 6 * * *"}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if calls == 0 {
		t.Fatal("改动调度后应触发重装")
	}
	if gotCron != "0 0 6 * * *" || !gotEnabled {
		t.Errorf("重装应收到新值，实际 enabled=%v cron=%q", gotEnabled, gotCron)
	}
}

// 不涉及清理调度的改动不应触发重装——无谓地摘挂任务会打断正在跑的那一轮
func TestUpdateWithoutScheduleChangeSkipsReload(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	calls := 0
	svc.SetLogCleanupReloadFunc(func(bool, string) error {
		calls++
		return nil
	})

	days := 14
	if _, err := svc.Update(UpdateSettingsInput{LogRetentionDays: &days}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if calls != 0 {
		t.Errorf("只改保留天数不应重装任务，实际调用 %d 次", calls)
	}
}

func TestApplyLogCleanupScheduleWithoutReloadFunc(t *testing.T) {
	svc, _ := newTestSettingsService(t)
	// 未注册回调时应为空操作而非 panic（启动早期可能还没接上）
	if err := svc.ApplyLogCleanupSchedule(); err != nil {
		t.Errorf("未注册回调时应为空操作: %v", err)
	}
}
