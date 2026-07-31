package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"auroramihomo/backend/internal/domain"
	"auroramihomo/backend/internal/model"
)

// 正常合并路径：base 与远程订阅无冲突时应成功生成 config.yaml，
// 并优先尝试 external-controller 热重载（而非直接重启进程）。
func TestMergeAndApplyDetailedHappyPath(t *testing.T) {
	svc, db, mgr := newTestConfigService(t)
	ctx := context.Background()

	baseYAML := `
mode: rule
external-controller: 127.0.0.1:9090
secret: mysecret
proxies:
  - name: "HK01"
    type: ss
    server: a.com
    port: 443
`
	if err := svc.UpdateBaseConfig(baseYAML); err != nil {
		t.Fatalf("写入 base 配置失败: %v", err)
	}

	res, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0))
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	if res == nil {
		t.Fatal("合并结果不应为空")
	}

	// config.yaml 应已生成且包含本地代理
	raw, err := os.ReadFile(filepath.Join(svc.configDir, "config.yaml"))
	if err != nil {
		t.Fatalf("读取生成的配置失败: %v", err)
	}
	if !strings.Contains(string(raw), "HK01") {
		t.Fatalf("生成配置应包含本地代理，实际:\n%s", raw)
	}

	// 应走热重载而非重启
	reloadCalls, restartCalls, controller, secret := mgr.snapshot()
	if reloadCalls != 1 {
		t.Fatalf("应调用一次 ReloadConfig，实际 %d", reloadCalls)
	}
	if restartCalls != 0 {
		t.Fatalf("正常路径不应触发进程重启，实际 %d 次", restartCalls)
	}
	if controller != "127.0.0.1:9090" || secret != "mysecret" {
		t.Fatalf("热重载应带上 base 配置里的 controller/secret，实际 controller=%q secret=%q", controller, secret)
	}

	// 应写入一条版本记录与一条 merged 快照
	versions, err := db.ListConfigVersions(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("应有 1 条版本记录，实际 %d", len(versions))
	}
}

// 校验失败时必须回滚：写入前的 config.yaml 内容应原样保留，
// 且不得继续走版本落库/热重载路径。
func TestMergeAndApplyDetailedValidateFailureRollsBack(t *testing.T) {
	svc, _, mgr := newTestConfigService(t)
	ctx := context.Background()

	// 先跑一次成功的合并，制造出"已存在的旧配置"
	if err := svc.UpdateBaseConfig(`
proxies:
  - name: "OLD"
    type: ss
    server: old.com
    port: 1
`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0)); err != nil {
		t.Fatalf("首次合并失败: %v", err)
	}
	oldContent, err := os.ReadFile(svc.configPath())
	if err != nil {
		t.Fatal(err)
	}

	// 第二次合并让校验失败，模拟内核判定配置非法
	mgr.mu.Lock()
	mgr.validateErr = errMockValidateFailed
	mgr.mu.Unlock()

	if err := svc.UpdateBaseConfig(`
proxies:
  - name: "NEW"
    type: ss
    server: new.com
    port: 2
`); err != nil {
		t.Fatal(err)
	}
	_, err = svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0))
	if err == nil {
		t.Fatal("校验失败时 MergeAndApplyDetailed 应返回错误")
	}

	// 回滚后磁盘内容应与旧配置一致（而不是新的、未通过校验的内容）
	rolledBack, err := os.ReadFile(svc.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(rolledBack) != string(oldContent) {
		t.Fatalf("校验失败后应回滚为旧配置，\n旧内容:\n%s\n实际内容:\n%s", oldContent, rolledBack)
	}

	// 校验失败不应触发热重载
	reloadCalls, _, _, _ := mgr.snapshot()
	if reloadCalls != 1 { // 仅第一次成功合并那一次
		t.Fatalf("校验失败后不应再触发热重载，实际累计 %d 次", reloadCalls)
	}
}

// 单条订阅拉取失败不应中断整次合并：其余订阅仍应正常参与合并。
func TestBuildRemoteConfigSkipsFailedSubscription(t *testing.T) {
	svc, db, _ := newTestConfigService(t)
	ctx := context.Background()

	good := &model.Subscription{Name: "good", URL: "", Content: "ss://YWVzLTI1Ni1nY206cHc=@1.1.1.1:8388#Good\n", Enabled: 1}
	bad := &model.Subscription{Name: "bad", URL: "http://127.0.0.1:1/definitely-unreachable", Enabled: 1}
	for _, s := range []*model.Subscription{good, bad} {
		if err := db.CreateSubscription(s); err != nil {
			t.Fatalf("创建订阅失败: %v", err)
		}
	}

	// 远程来源默认已是 none（不使用远程配置），
	// 验证多订阅聚合行为需显式选择 all
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceAll}
	})

	if err := svc.buildRemoteConfig(ctx, 0); err != nil {
		t.Fatalf("存在可用订阅时不应整体失败: %v", err)
	}

	// 按 name 取聚合行：type="remote" 下还有每条订阅的单独快照
	merged, err := db.GetRemoteMergedConfig()
	if err != nil {
		t.Fatalf("应生成 remote-merged 配置: %v", err)
	}
	if !strings.Contains(merged.Content, "Good") {
		t.Fatalf("合并结果应包含成功订阅的节点，实际:\n%s", merged.Content)
	}

	badRow, err := db.GetSubscription(bad.ID)
	if err != nil {
		t.Fatal(err)
	}
	if badRow.Status != "error" {
		t.Fatalf("失败订阅应标记为 error，实际 %q", badRow.Status)
	}
	goodRow, err := db.GetSubscription(good.ID)
	if err != nil {
		t.Fatal(err)
	}
	if goodRow.Status != "ok" {
		t.Fatalf("成功订阅应标记为 ok，实际 %q", goodRow.Status)
	}
}

// 全部订阅均失败时必须报错，不能用空配置悄悄覆盖掉可用的旧配置。
func TestBuildRemoteConfigAllFailedReturnsError(t *testing.T) {
	svc, db, _ := newTestConfigService(t)
	ctx := context.Background()

	bad := &model.Subscription{Name: "bad", URL: "http://127.0.0.1:1/definitely-unreachable", Enabled: 1}
	if err := db.CreateSubscription(bad); err != nil {
		t.Fatal(err)
	}

	// 需显式选择 all：默认的 none 不会去拉订阅，也就无从失败
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceAll}
	})

	if err := svc.buildRemoteConfig(ctx, 0); err == nil {
		t.Fatal("全部订阅失败时应返回错误")
	}
}

// RestoreVersion 应把磁盘配置还原为指定版本内容，并按还原后的配置热重载。
func TestRestoreVersionRestoresContentAndReloads(t *testing.T) {
	svc, db, mgr := newTestConfigService(t)
	ctx := context.Background()

	versionContent := `
external-controller: 127.0.0.1:9091
secret: restoredsecret
proxies:
  - name: "RESTORED"
    type: ss
    server: r.com
    port: 1
`
	v := &model.ConfigVersion{Hash: "h1", Content: versionContent, Note: "test", CreatedAt: time.Now()}
	if err := db.SaveConfigVersion(v); err != nil {
		t.Fatal(err)
	}

	// 先写入一份"当前配置"，验证恢复会覆盖它
	if err := svc.writeConfigAtomically([]byte("proxies: []\n")); err != nil {
		t.Fatal(err)
	}

	if err := svc.RestoreVersion(ctx, v.ID); err != nil {
		t.Fatalf("恢复版本失败: %v", err)
	}

	raw, err := os.ReadFile(svc.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "RESTORED") {
		t.Fatalf("恢复后配置应为指定版本内容，实际:\n%s", raw)
	}

	_, _, controller, secret := mgr.snapshot()
	if controller != "127.0.0.1:9091" || secret != "restoredsecret" {
		t.Fatalf("应按恢复后的配置热重载，实际 controller=%q secret=%q", controller, secret)
	}
}

// 恢复版本时若校验失败，必须回滚磁盘内容，且备份文件不能被移动丢失。
func TestRestoreVersionValidateFailureRollsBack(t *testing.T) {
	svc, db, mgr := newTestConfigService(t)
	ctx := context.Background()

	if err := svc.writeConfigAtomically([]byte("proxies: []\n# original\n")); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(svc.configPath())
	if err != nil {
		t.Fatal(err)
	}

	v := &model.ConfigVersion{Hash: "h2", Content: "proxies: []\n# broken\n", CreatedAt: time.Now()}
	if err := db.SaveConfigVersion(v); err != nil {
		t.Fatal(err)
	}

	mgr.mu.Lock()
	mgr.validateErr = errMockValidateFailed
	mgr.mu.Unlock()

	if err := svc.RestoreVersion(ctx, v.ID); err == nil {
		t.Fatal("校验失败时 RestoreVersion 应返回错误")
	}

	rolledBack, err := os.ReadFile(svc.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(rolledBack) != string(original) {
		t.Fatalf("校验失败后应回滚为原始配置，\n原始:\n%s\n实际:\n%s", original, rolledBack)
	}

	// 备份目录中应仍能找到该次备份（用拷贝回滚，而非移动丢失）
	entries, err := os.ReadDir(svc.backupDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("回滚应保留备份文件，而不是把它移走丢失")
	}
}

// 定时拉取到点即执行合并：不再有「订阅是否到期」这层判断，
// 节流完全由用户配置的 Cron 表达式表达。
func TestRunScheduledPullMergesWhenSourceSet(t *testing.T) {
	svc, db, mgr := newTestConfigService(t)
	ctx := context.Background()

	if err := svc.UpdateBaseConfig("proxies: []\n"); err != nil {
		t.Fatal(err)
	}

	// 手动粘贴节点的订阅无需回源，可稳定验证「拉取即合并并热重载」
	sub := &model.Subscription{
		Name:    "manual",
		Content: "ss://YWVzLTI1Ni1nY206cHc=@1.1.1.1:8388#Due\n",
		Enabled: 1,
	}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatal(err)
	}
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceSubscription, ID: sub.ID}
	})

	if err := svc.RunScheduledPull(ctx); err != nil {
		t.Fatalf("定时拉取应正常完成: %v", err)
	}
	reloadCalls, _, _, _ := mgr.snapshot()
	if reloadCalls != 1 {
		t.Fatalf("定时拉取应触发一次合并并热重载，实际 %d 次", reloadCalls)
	}
}

var errMockValidateFailed = &mockErr{"mock validate failed"}

// GetLastDiff 在内存 lastDiff 为空时（如进程重启）应从数据库最近两条版本记录
// 重新计算，而不是永久返回空结果。
func TestGetLastDiffFallsBackToVersionHistory(t *testing.T) {
	svc, db, _ := newTestConfigService(t)

	older := &model.ConfigVersion{
		Hash:    "h1",
		Content: "proxies:\n  - name: OLD\n    type: ss\n    server: a.com\n    port: 1\n",
	}
	if err := db.SaveConfigVersion(older); err != nil {
		t.Fatal(err)
	}
	newer := &model.ConfigVersion{
		Hash:    "h2",
		Content: "proxies:\n  - name: NEW\n    type: ss\n    server: b.com\n    port: 2\n",
	}
	if err := db.SaveConfigVersion(newer); err != nil {
		t.Fatal(err)
	}

	// 模拟进程重启：lastDiff 为空
	diff := svc.GetLastDiff()
	if len(diff.Added) == 0 && len(diff.Removed) == 0 {
		t.Fatalf("应从版本历史回退计算出 diff，实际为空: %+v", diff)
	}

	foundAdded, foundRemoved := false, false
	for _, d := range diff.Added {
		if d.Kind == "proxy" && d.Name == "NEW" {
			foundAdded = true
		}
	}
	for _, d := range diff.Removed {
		if d.Kind == "proxy" && d.Name == "OLD" {
			foundRemoved = true
		}
	}
	if !foundAdded || !foundRemoved {
		t.Fatalf("回退计算结果应体现新增 NEW 与删除 OLD，实际 %+v", diff)
	}
}

// 版本记录不足两条时，回退计算应安全返回空结果而不是报错或 panic
func TestGetLastDiffFallbackWithInsufficientHistory(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	diff := svc.GetLastDiff()
	if len(diff.Added)+len(diff.Removed)+len(diff.Changed) != 0 {
		t.Fatalf("无版本历史时应返回空 diff，实际 %+v", diff)
	}
}

// 只刷新单条订阅时，其余订阅必须复用缓存一起参与合并。
// 此前 onlyID 会让其他订阅被直接跳过，导致用户点一次"立即更新"
// 就把其他机场的节点全部从 config.yaml 里弄丢。
func TestBuildRemoteConfigOnlyIDKeepsOtherSubscriptions(t *testing.T) {
	svc, db, _ := newTestConfigService(t)
	ctx := context.Background()

	a := &model.Subscription{Name: "A", Content: "ss://YWVzLTI1Ni1nY206cHc=@1.1.1.1:8388#NodeA\n", Enabled: 1}
	b := &model.Subscription{Name: "B", Content: "ss://YWVzLTI1Ni1nY206cHc=@2.2.2.2:8388#NodeB\n", Enabled: 1}
	for _, s := range []*model.Subscription{a, b} {
		if err := db.CreateSubscription(s); err != nil {
			t.Fatal(err)
		}
	}

	// onlyID 与来源选择是两个正交概念，这里验证前者，故显式选 all
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceAll}
	})

	if err := svc.buildRemoteConfig(ctx, 0); err != nil {
		t.Fatalf("全量合并失败: %v", err)
	}
	if err := svc.buildRemoteConfig(ctx, a.ID); err != nil {
		t.Fatalf("单条刷新失败: %v", err)
	}

	merged, err := db.GetConfigByType("remote")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(merged.Content, "NodeA") {
		t.Error("被刷新订阅 A 的节点应在合并结果中")
	}
	if !strings.Contains(merged.Content, "NodeB") {
		t.Error("未被刷新的订阅 B 的节点也必须保留在合并结果中")
	}
}

// 定时拉取时上游不可达的订阅会被标记为 error 并跳过，
// 只要还有订阅拉取成功，整次拉取就应成功——否则一条坏订阅
// 就能让所有人的配置停止更新。
//
// （原先此处测的是「失败后按 interval 退避、避免每轮重试」，
// 随每分钟轮询一并移除：现在重试节奏由用户配置的 Cron 表达式决定。）
func TestRunScheduledPullSkipsUnreachableSubscription(t *testing.T) {
	svc, db, _ := newTestConfigService(t)

	if err := svc.UpdateBaseConfig("proxies: []\n"); err != nil {
		t.Fatal(err)
	}
	// 端口 1 上不会有服务，回源必然失败
	bad := &model.Subscription{Name: "failing", URL: "http://127.0.0.1:1/x", Enabled: 1}
	if err := db.CreateSubscription(bad); err != nil {
		t.Fatal(err)
	}
	// 手动粘贴节点的订阅无需回源，必定成功
	good := &model.Subscription{
		Name:    "manual",
		Content: "ss://YWVzLTI1Ni1nY206cHc=@1.1.1.1:8388#Ok\n",
		Enabled: 1,
	}
	if err := db.CreateSubscription(good); err != nil {
		t.Fatal(err)
	}

	// 用 all 类型让两条订阅同时参与：组合类来源需要注入渲染器，
	// 而本测试要验证的是订阅逐条容错，与渲染无关。
	// （all 已从界面移除，但后端仍需支持存量数据。）
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceAll}
	})

	if err := svc.RunScheduledPull(context.Background()); err != nil {
		t.Fatalf("仍有订阅可用时不应让定时拉取整体失败: %v", err)
	}

	got, err := db.GetSubscription(bad.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "error" {
		t.Errorf("回源失败应把订阅标记为 error，实际 %q", got.Status)
	}
}

// external-controller 变更必须走进程重启，不能只发热重载。
//
// 固化的是一个实测到的内核行为：mihomo 的 PUT /configs 会返回 204，
// 但不会重开 API 监听套接字。因此把 external-controller 从
// 127.0.0.1:19090 改成 0.0.0.0:19090 之后，监听仍停在回环，
// 局域网内其它设备打不开 zashboard——而界面已经提示"配置已生效"。
func TestMergeAndApplyRestartsWhenControllerChanged(t *testing.T) {
	svc, _, mgr := newTestConfigService(t)
	ctx := context.Background()

	if err := svc.UpdateBaseConfig("external-controller: 127.0.0.1:19090\nproxies: []\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0)); err != nil {
		t.Fatalf("首次合并失败: %v", err)
	}
	// 首次合并（磁盘上原本无配置）应走热重载，不该无谓重启
	if reloadCalls, restartCalls, _, _ := mgr.snapshot(); reloadCalls != 1 || restartCalls != 0 {
		t.Fatalf("首次合并应只热重载，实际 reload=%d restart=%d", reloadCalls, restartCalls)
	}

	// 只改监听地址
	if err := svc.UpdateBaseConfig("external-controller: 0.0.0.0:19090\nproxies: []\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0)); err != nil {
		t.Fatalf("第二次合并失败: %v", err)
	}

	reloadCalls, restartCalls, _, _ := mgr.snapshot()
	if restartCalls != 1 {
		t.Errorf("controller 变更应触发一次进程重启，实际 %d 次", restartCalls)
	}
	if reloadCalls != 1 {
		t.Errorf("controller 变更时不应再发热重载，ReloadConfig 累计应仍为 1，实际 %d", reloadCalls)
	}
}

// secret 变更同样只有重启才生效：实测热重载后旧密钥仍然放行、
// 新密钥不被要求，等于用户以为设了鉴权其实没设，是个安全问题。
func TestMergeAndApplyRestartsWhenSecretChanged(t *testing.T) {
	svc, _, mgr := newTestConfigService(t)
	ctx := context.Background()

	if err := svc.UpdateBaseConfig("external-controller: 127.0.0.1:19090\nproxies: []\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0)); err != nil {
		t.Fatal(err)
	}

	if err := svc.UpdateBaseConfig("external-controller: 127.0.0.1:19090\nsecret: newsecret\nproxies: []\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0)); err != nil {
		t.Fatal(err)
	}

	if _, restartCalls, _, _ := mgr.snapshot(); restartCalls != 1 {
		t.Errorf("secret 变更应触发一次进程重启，实际 %d 次", restartCalls)
	}
}

// 反面用例：控制接口没动时，其余配置变更仍应走热重载。
// 否则每次保存配置都会断掉全部现有代理连接。
func TestMergeAndApplyHotReloadsWhenControllerUnchanged(t *testing.T) {
	svc, _, mgr := newTestConfigService(t)
	ctx := context.Background()

	if err := svc.UpdateBaseConfig("external-controller: 127.0.0.1:19090\nproxies: []\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0)); err != nil {
		t.Fatal(err)
	}

	// 只改与控制接口无关的项
	if err := svc.UpdateBaseConfig("external-controller: 127.0.0.1:19090\nmode: global\nproxies: []\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0)); err != nil {
		t.Fatal(err)
	}

	reloadCalls, restartCalls, _, _ := mgr.snapshot()
	if restartCalls != 0 {
		t.Errorf("控制接口未变时不应重启进程（会断开所有连接），实际 %d 次", restartCalls)
	}
	if reloadCalls != 2 {
		t.Errorf("两次合并都应走热重载，实际 ReloadConfig %d 次", reloadCalls)
	}
}
