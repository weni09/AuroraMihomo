package service

import (
	"path/filepath"
	"testing"

	"auroramihomo/backend/internal/domain"
	"auroramihomo/backend/internal/model"
	"auroramihomo/backend/internal/repository"
	"auroramihomo/backend/internal/updater"
)

func newTestSettingsService(t *testing.T) (*SettingsService, *repository.Database) {
	t.Helper()
	dir := t.TempDir()
	db, err := repository.NewDatabase(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("初始化测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	upd := updater.New(updater.Config{DataDir: dir})
	return NewSettingsService(db, upd), db
}

// 未持久化任何策略时应回落到默认值（全部 local）
func TestGetMergePolicyDefaults(t *testing.T) {
	svc, _ := newTestSettingsService(t)
	p := svc.GetMergePolicy()
	if p.ProxyPriority != "local" || p.RulePriority != "local" || p.DNSPriority != "local" || p.TUNPriority != "local" || p.GeneralPriority != "local" {
		t.Fatalf("默认策略应全部为 local，实际 %+v", p)
	}
}

// 设置后应可读回，且只更新传入的字段，其余字段维持原值
func TestSetMergePolicyPersistsAndReadsBack(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	if _, err := svc.SetMergePolicy("remote", "", "", "", ""); err != nil {
		t.Fatalf("设置 proxy 策略失败: %v", err)
	}
	p := svc.GetMergePolicy()
	if p.ProxyPriority != "remote" {
		t.Fatalf("proxy 策略应已更新为 remote，实际 %q", p.ProxyPriority)
	}
	if p.RulePriority != "local" {
		t.Fatalf("未传入的 rule 策略应维持默认值，实际 %q", p.RulePriority)
	}

	if _, err := svc.SetMergePolicy("", "", "remote", "remote", ""); err != nil {
		t.Fatalf("设置 dns/tun 策略失败: %v", err)
	}
	p = svc.GetMergePolicy()
	if p.DNSPriority != "remote" || p.TUNPriority != "remote" {
		t.Fatalf("dns/tun 策略应已更新为 remote，实际 dns=%q tun=%q", p.DNSPriority, p.TUNPriority)
	}
	// 之前设置的 proxy 策略不应被后续调用覆盖
	if p.ProxyPriority != "remote" {
		t.Fatalf("先前设置的 proxy 策略应保持不变，实际 %q", p.ProxyPriority)
	}
}

// dns/tun 是系统级配置的简单 Local/Remote First 切换（设计 §11/§16），
// 不支持 merge/manual 语义，非法值必须被拒绝。
func TestSetMergePolicyRejectsInvalidDNSTUNValues(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	if _, err := svc.SetMergePolicy("", "", "merge", "", ""); err == nil {
		t.Fatal("dns 策略不支持 merge，应报错")
	}
	if _, err := svc.SetMergePolicy("", "", "", "manual", ""); err == nil {
		t.Fatal("tun 策略不支持 manual，应报错")
	}
	if _, err := svc.SetMergePolicy("", "", "bogus", "", ""); err == nil {
		t.Fatal("非法 dns 策略值应报错")
	}

	// 校验失败不应污染已持久化的值
	p := svc.GetMergePolicy()
	if p.DNSPriority != "local" || p.TUNPriority != "local" {
		t.Fatalf("非法输入不应改变已持久化的策略，实际 dns=%q tun=%q", p.DNSPriority, p.TUNPriority)
	}
}

// proxy/rule 策略支持 merge/manual 等对象级合并语义
func TestSetMergePolicyAcceptsComplexProxyRuleValues(t *testing.T) {
	svc, _ := newTestSettingsService(t)
	if _, err := svc.SetMergePolicy("merge", "manual", "", "", ""); err != nil {
		t.Fatalf("proxy=merge, rule=manual 应被接受: %v", err)
	}
	p := svc.GetMergePolicy()
	if p.ProxyPriority != "merge" || p.RulePriority != "manual" {
		t.Fatalf("策略应已更新，实际 proxy=%q rule=%q", p.ProxyPriority, p.RulePriority)
	}
}

// Update 应把自动更新开关、cron、CDN 列表持久化，并可通过 Get 读回
func TestSettingsUpdatePersists(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	enabled := true
	out, err := svc.Update(UpdateSettingsInput{
		AutoUpdateEnabled: &enabled,
		AutoUpdateCron:    "30 3 * * *",
		CDNProviders:      []string{"ghproxy.com", "github"},
	})
	if err != nil {
		t.Fatalf("更新设置失败: %v", err)
	}
	if !out.AutoUpdateEnabled {
		t.Fatal("自动更新应已启用")
	}

	got := svc.Get()
	if !got.AutoUpdateEnabled {
		t.Fatal("持久化后读回应仍为启用")
	}
	if len(got.CDNProviders) == 0 {
		t.Fatal("CDN 列表应已持久化")
	}
}

// 非法 cron 表达式必须在写入前被拒绝
func TestSettingsUpdateRejectsInvalidCron(t *testing.T) {
	svc, _ := newTestSettingsService(t)
	if _, err := svc.Update(UpdateSettingsInput{AutoUpdateCron: "not a cron"}); err == nil {
		t.Fatal("非法 cron 表达式应报错")
	}
}

// LoadAndApply 应从数据库恢复此前持久化的设置并应用到 updater
func TestLoadAndApplyRestoresPersistedSettings(t *testing.T) {
	svc, db := newTestSettingsService(t)

	enabled := true
	if _, err := svc.Update(UpdateSettingsInput{AutoUpdateEnabled: &enabled, AutoUpdateCron: "0 5 * * *"}); err != nil {
		t.Fatal(err)
	}

	// 模拟进程重启：用同一个库新建一个 SettingsService 实例
	fresh := NewSettingsService(db, svc.updater)
	if err := fresh.LoadAndApply(); err != nil {
		t.Fatalf("LoadAndApply 失败: %v", err)
	}
	got := fresh.Get()
	if !got.AutoUpdateEnabled {
		t.Fatal("重新加载后自动更新开关应恢复为已启用")
	}
}

// 通用参数策略（作用于 mode/端口/Geo/external-controller 等绝大多数顶层键）
// 应能持久化并读回，且只接受 local/remote
func TestSetMergePolicyGeneral(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	if _, err := svc.SetMergePolicy("", "", "", "", "remote"); err != nil {
		t.Fatalf("设置 general 策略失败: %v", err)
	}
	if got := svc.GetMergePolicy().GeneralPriority; got != "remote" {
		t.Fatalf("general 策略应为 remote，实际 %q", got)
	}

	if _, err := svc.SetMergePolicy("", "", "", "", "merge"); err == nil {
		t.Fatal("general 策略不支持 merge，应报错")
	}
	// 非法输入不应污染已持久化的值
	if got := svc.GetMergePolicy().GeneralPriority; got != "remote" {
		t.Fatalf("非法输入后应保持原值 remote，实际 %q", got)
	}
}

// ---- 远程来源的悬空引用 ----
//
// 背景：设置里存的是实体 id，实体被删后这条设置若不修正就成了悬空引用。
// RemoteSource.Valid() 只校验形状（id>0），查不出"实体已不存在"，
// 于是悬空引用一路通到合并阶段才炸——「拉取并合并」永久失败，
// 而界面下拉框因候选项里没有该 id 而显示空白，用户看不出问题在哪。

// TestGetRemoteSourceFallsBackWhenSubscriptionDeleted 锁定：
// 指向已删订阅时必须回落为 none，且把回落持久化。
func TestGetRemoteSourceFallsBackWhenSubscriptionDeleted(t *testing.T) {
	svc, db := newTestSettingsService(t)

	sub := &model.Subscription{Name: "will-be-deleted", Enabled: 1, ShareToken: "tok-del"}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceSubscription, ID: sub.ID,
	}); err != nil {
		t.Fatalf("设置远程来源失败: %v", err)
	}
	// 设置成功后应能读回
	if got := svc.GetRemoteSource(); got.Type != domain.RemoteSourceSubscription || got.ID != sub.ID {
		t.Fatalf("设置后读回不一致: %+v", got)
	}

	if err := db.DeleteSubscription(sub.ID); err != nil {
		t.Fatal(err)
	}

	got := svc.GetRemoteSource()
	if got.Type != domain.RemoteSourceNone {
		t.Errorf("订阅已删，应回落为 none，实际 type=%q id=%d", got.Type, got.ID)
	}
	if got.ID != 0 {
		t.Errorf("回落后 id 应为 0，实际 %d", got.ID)
	}
	// 回落必须落库：否则界面仍显示已删的 id，用户不手动改就一直卡在报错上
	if v, err := db.GetSetting("remote.source.type"); err == nil && v != domain.RemoteSourceNone {
		t.Errorf("回落未持久化，settings 里仍为 %q", v)
	}
	if v, err := db.GetSetting("remote.source.id"); err == nil && v != "0" {
		t.Errorf("回落未持久化，settings 里 id 仍为 %q", v)
	}
}

// TestGetRemoteSourceFallsBackWhenSubscriptionDisabled 被禁用的订阅同样
// 无法充当远程来源（buildRemoteConfig 会跳过它并报"不存在或已禁用"），
// 读取时就该识别出来。
func TestGetRemoteSourceFallsBackWhenSubscriptionDisabled(t *testing.T) {
	svc, db := newTestSettingsService(t)

	sub := &model.Subscription{Name: "to-disable", Enabled: 1, ShareToken: "tok-dis"}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceSubscription, ID: sub.ID,
	}); err != nil {
		t.Fatal(err)
	}
	sub.Enabled = 0
	if err := db.UpdateSubscription(sub); err != nil {
		t.Fatal(err)
	}
	if got := svc.GetRemoteSource(); got.Type != domain.RemoteSourceNone {
		t.Errorf("订阅已禁用，应回落为 none，实际 type=%q id=%d", got.Type, got.ID)
	}
}

// TestGetRemoteSourceKeepsCronOnFallback 来源失效不应把用户配的调度一起丢掉。
func TestGetRemoteSourceKeepsCronOnFallback(t *testing.T) {
	svc, db := newTestSettingsService(t)

	sub := &model.Subscription{Name: "s", Enabled: 1, ShareToken: "tok-cron"}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatal(err)
	}
	enabled := true
	if _, err := svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceSubscription, ID: sub.ID,
		Cron: "0 30 */4 * * *", CronEnabled: &enabled,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteSubscription(sub.ID); err != nil {
		t.Fatal(err)
	}
	got := svc.GetRemoteSource()
	if got.Type != domain.RemoteSourceNone {
		t.Fatalf("应回落为 none，实际 %q", got.Type)
	}
	if got.Cron != "0 30 */4 * * *" {
		t.Errorf("回落不应丢掉 Cron，实际 %q", got.Cron)
	}
	if !got.CronEnabled {
		t.Error("回落不应丢掉 CronEnabled")
	}
}

// TestDeleteSubscriptionClearsRemoteSourceRef 删除时应主动清引用，
// 不能只靠读取时兜底——否则数据库里一直留着悬空引用。
func TestDeleteSubscriptionClearsRemoteSourceRef(t *testing.T) {
	svc, db := newTestSettingsService(t)

	sub := &model.Subscription{Name: "s", Enabled: 1, ShareToken: "tok-clear"}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceSubscription, ID: sub.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteSubscription(sub.ID); err != nil {
		t.Fatal(err)
	}
	// 直接查库，绕开 GetRemoteSource 的读取时兜底
	if v, err := db.GetSetting("remote.source.type"); err == nil && v != domain.RemoteSourceNone {
		t.Errorf("删除订阅后设置类型应为 none，实际 %q", v)
	}
	if v, err := db.GetSetting("remote.source.id"); err == nil && v != "0" {
		t.Errorf("删除订阅后设置 id 应为 0，实际 %q", v)
	}
}

// TestDeleteSubscriptionKeepsUnrelatedRemoteSourceRef 只清指向自己的引用，
// 不能误伤指向别的实体的设置。
func TestDeleteSubscriptionKeepsUnrelatedRemoteSourceRef(t *testing.T) {
	svc, db := newTestSettingsService(t)

	keep := &model.Subscription{Name: "keep", Enabled: 1, ShareToken: "tok-keep"}
	other := &model.Subscription{Name: "other", Enabled: 1, ShareToken: "tok-other"}
	if err := db.CreateSubscription(keep); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSubscription(other); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceSubscription, ID: keep.ID,
	}); err != nil {
		t.Fatal(err)
	}
	// 删掉的是另一条，设置不该被动
	if err := db.DeleteSubscription(other.ID); err != nil {
		t.Fatal(err)
	}
	got := svc.GetRemoteSource()
	if got.Type != domain.RemoteSourceSubscription || got.ID != keep.ID {
		t.Errorf("删除无关订阅后设置被误改: %+v", got)
	}
}

// TestDeleteCollectionClearsRemoteSourceRef 组合走同一套清理逻辑。
func TestDeleteCollectionClearsRemoteSourceRef(t *testing.T) {
	svc, db := newTestSettingsService(t)

	col := &model.SubCollection{Name: "c", Enabled: 1, ShareToken: "tok-col"}
	if err := db.CreateCollection(col); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceCollection, ID: col.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteCollection(col.ID); err != nil {
		t.Fatal(err)
	}
	if v, err := db.GetSetting("remote.source.type"); err == nil && v != domain.RemoteSourceNone {
		t.Errorf("删除组合后设置类型应为 none，实际 %q", v)
	}
	if got := svc.GetRemoteSource(); got.Type != domain.RemoteSourceNone {
		t.Errorf("删除组合后应回落为 none，实际 %+v", got)
	}
}
