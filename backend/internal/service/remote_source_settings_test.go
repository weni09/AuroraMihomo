package service

import (
	"strings"
	"testing"

	"auroramihomo/backend/internal/domain"
	"auroramihomo/backend/internal/model"
)

// newTestSettingsService 复用 settings_service_test.go 中的定义

// 未设置过时应读到「不使用远程配置」。
func TestGetRemoteSourceDefaultsToNone(t *testing.T) {
	svc, _ := newTestSettingsService(t)
	got := svc.GetRemoteSource()
	if got.Type != domain.RemoteSourceNone {
		t.Fatalf("默认应为 none，实际 %q", got.Type)
	}
}

// 传空类型等同于「不填」，应存为 none 而非报错。
func TestSetRemoteSourceEmptyTypeMeansNone(t *testing.T) {
	svc, _ := newTestSettingsService(t)
	got, err := svc.SetRemoteSource(RemoteSourceInput{Type: ""})
	if err != nil {
		t.Fatalf("空类型应被接受: %v", err)
	}
	if got.Type != domain.RemoteSourceNone {
		t.Errorf("空类型应存为 none，实际 %q", got.Type)
	}
}

// 外部订阅链接需持久化并可回读。
func TestSetRemoteSourceURLRoundTrip(t *testing.T) {
	svc, _ := newTestSettingsService(t)
	const raw = "https://example.com/sub?token=abc"
	got, err := svc.SetRemoteSource(RemoteSourceInput{Type: domain.RemoteSourceURL, URL: raw})
	if err != nil {
		t.Fatalf("设置外部链接失败: %v", err)
	}
	if got.Type != domain.RemoteSourceURL || got.URL != raw {
		t.Fatalf("回读不一致: type=%q url=%q", got.Type, got.URL)
	}
	// 重新读取应保持一致
	again := svc.GetRemoteSource()
	if again.URL != raw {
		t.Errorf("持久化后 URL 丢失，实际 %q", again.URL)
	}
}

// 缺地址、非 http(s)、缺主机名都必须被拒。
// 提前拦掉可避免存下一个每次合并都必然失败的地址。
func TestSetRemoteSourceURLValidation(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	if _, err := svc.SetRemoteSource(RemoteSourceInput{Type: domain.RemoteSourceURL}); err == nil {
		t.Error("缺地址应报错")
	} else if !strings.Contains(err.Error(), "必须填写地址") {
		t.Errorf("错误信息应提示填地址，实际: %v", err)
	}

	// file:// 可读本地文件，必须拒绝
	if _, err := svc.SetRemoteSource(RemoteSourceInput{Type: domain.RemoteSourceURL, URL: "file:///etc/passwd"}); err == nil {
		t.Error("file:// 应被拒绝")
	}
	// 其它协议走私
	if _, err := svc.SetRemoteSource(RemoteSourceInput{Type: domain.RemoteSourceURL, URL: "gopher://x/1"}); err == nil {
		t.Error("gopher:// 应被拒绝")
	}
	// 缺主机名
	if _, err := svc.SetRemoteSource(RemoteSourceInput{Type: domain.RemoteSourceURL, URL: "https:///path"}); err == nil {
		t.Error("缺主机名应被拒绝")
	}
}

// 选择本地实体时必须确认其存在，否则用户会得到一份
// 静默退化为「仅本地配置」的结果，而界面仍显示他选中的来源。
func TestSetRemoteSourceValidatesEntityExists(t *testing.T) {
	svc, db := newTestSettingsService(t)

	if _, err := svc.SetRemoteSource(RemoteSourceInput{Type: domain.RemoteSourceSubscription, ID: 999}); err == nil {
		t.Error("不存在的订阅应被拒绝")
	}
	if _, err := svc.SetRemoteSource(RemoteSourceInput{Type: domain.RemoteSourceCollection, ID: 999}); err == nil {
		t.Error("不存在的组合应被拒绝")
	}
	if _, err := svc.SetRemoteSource(RemoteSourceInput{Type: domain.RemoteSourceFile, ID: 999}); err == nil {
		t.Error("不存在的文件应被拒绝")
	}

	// 原样输出型文件不能作为配置来源
	raw := &model.SubFile{Name: "plain", Content: "x", ConfigType: model.FileConfigTypeFile}
	if err := db.SaveFile(raw); err != nil {
		t.Fatalf("保存文件失败: %v", err)
	}
	if _, err := svc.SetRemoteSource(RemoteSourceInput{Type: domain.RemoteSourceFile, ID: raw.ID}); err == nil {
		t.Error("原样输出型文件应被拒绝作为远程来源")
	}

	// mihomo 型文件可以
	tpl := &model.SubFile{
		Name: "tpl", Content: "proxies: []",
		ConfigType: model.FileConfigTypeMihomo,
		SourceType: model.SourceTypeSubscription, SourceID: 1,
	}
	if err := db.SaveFile(tpl); err != nil {
		t.Fatalf("保存模板失败: %v", err)
	}
	got, err := svc.SetRemoteSource(RemoteSourceInput{Type: domain.RemoteSourceFile, ID: tpl.ID})
	if err != nil {
		t.Fatalf("mihomo 型文件应被接受: %v", err)
	}
	if got.Type != domain.RemoteSourceFile || got.ID != tpl.ID {
		t.Errorf("回读不一致: %+v", got)
	}
}

// 从 url 切回 none 后，残留的 URL 不应再生效。
func TestSwitchFromURLToNone(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	if _, err := svc.SetRemoteSource(RemoteSourceInput{Type: domain.RemoteSourceURL, URL: "https://example.com/s"}); err != nil {
		t.Fatalf("设置外部链接失败: %v", err)
	}
	got, err := svc.SetRemoteSource(RemoteSourceInput{Type: domain.RemoteSourceNone})
	if err != nil {
		t.Fatalf("切回 none 失败: %v", err)
	}
	if got.Type != domain.RemoteSourceNone {
		t.Fatalf("应为 none，实际 %q", got.Type)
	}
	// 类型为 none 时 URL 不参与构建，但也不该残留旧值误导界面
	if got.URL != "" {
		t.Errorf("切回 none 后 URL 应被清空，实际 %q", got.URL)
	}
}

// Cron 校验：调度器启用了秒级字段，内部统一 6 段。
// 用户按标准 crontab 写 5 段应被自动补秒位，而不是报错。
func TestNormalizeCron(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		// 空串原样返回，由调用方决定默认值
		{"", "", false},
		{"   ", "", false},
		// 5 段自动补秒位
		{"0 4 * * *", "0 0 4 * * *", false},
		{"*/30 * * * *", "0 */30 * * * *", false},
		// 6 段原样保留
		{"0 0 4 * * *", "0 0 4 * * *", false},
		{"*/10 * * * * *", "*/10 * * * * *", false},
		// 段数不对
		{"* * *", "", true},
		{"* * * * * * *", "", true},
		// 段数对但内容非法
		{"99 99 99 99 99 99", "", true},
		{"abc def ghi jkl mno", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeCron(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizeCron(%q) 应报错", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeCron(%q) 意外报错: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeCron(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 未设置时应回落到默认调度并默认启用。
func TestRemoteSourceCronDefaults(t *testing.T) {
	svc, _ := newTestSettingsService(t)
	src := svc.GetRemoteSource()
	if src.Cron != defaultRemoteSourceCron {
		t.Errorf("默认调度应为 %q，实际 %q", defaultRemoteSourceCron, src.Cron)
	}
	if !src.CronEnabled {
		t.Error("定时拉取应默认启用")
	}
}

// Cron 设置需持久化，且 5 段写法应被规范化后存储。
func TestSetRemoteSourceCronRoundTrip(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	got, err := svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceURL,
		URL:  "https://example.com/sub",
		Cron: "30 3 * * *", // 5 段
	})
	if err != nil {
		t.Fatalf("设置失败: %v", err)
	}
	if got.Cron != "0 30 3 * * *" {
		t.Errorf("5 段应被补为 6 段，实际 %q", got.Cron)
	}
	// 回读一致
	if again := svc.GetRemoteSource(); again.Cron != "0 30 3 * * *" {
		t.Errorf("持久化后不一致，实际 %q", again.Cron)
	}
}

// 非法 Cron 必须被拒，而不是存下一个永不触发的调度。
func TestSetRemoteSourceRejectsInvalidCron(t *testing.T) {
	svc, _ := newTestSettingsService(t)
	_, err := svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceURL,
		URL:  "https://example.com/sub",
		Cron: "not a cron",
	})
	if err == nil {
		t.Fatal("非法 Cron 应被拒绝")
	}
}

// Cron 留空表示不修改，应沿用已存的值。
func TestSetRemoteSourceEmptyCronKeepsExisting(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	if _, err := svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceURL, URL: "https://example.com/a", Cron: "0 15 2 * * *",
	}); err != nil {
		t.Fatalf("首次设置失败: %v", err)
	}
	// 只改地址，不传 Cron
	got, err := svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceURL, URL: "https://example.com/b",
	})
	if err != nil {
		t.Fatalf("二次设置失败: %v", err)
	}
	if got.Cron != "0 15 2 * * *" {
		t.Errorf("Cron 留空应沿用旧值，实际 %q", got.Cron)
	}
}

// 关闭定时拉取后应能持久化，并可再次开启。
func TestSetRemoteSourceCronEnabledToggle(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	off := false
	got, err := svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceURL, URL: "https://example.com/s", CronEnabled: &off,
	})
	if err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if got.CronEnabled {
		t.Error("应已关闭定时拉取")
	}
	if again := svc.GetRemoteSource(); again.CronEnabled {
		t.Error("关闭状态未持久化")
	}

	on := true
	got, err = svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceURL, URL: "https://example.com/s", CronEnabled: &on,
	})
	if err != nil {
		t.Fatalf("开启失败: %v", err)
	}
	if !got.CronEnabled {
		t.Error("应已重新开启定时拉取")
	}
}

// 设置变更后必须立刻重装调度任务，否则要等进程重启才生效。
func TestSetRemoteSourceReloadsSchedule(t *testing.T) {
	svc, _ := newTestSettingsService(t)

	var gotEnabled bool
	var gotCron string
	calls := 0
	svc.SetRemotePullReloadFunc(func(enabled bool, cronExpr string) error {
		calls++
		gotEnabled, gotCron = enabled, cronExpr
		return nil
	})

	if _, err := svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceURL, URL: "https://example.com/s", Cron: "0 */5 * * * *",
	}); err != nil {
		t.Fatalf("设置失败: %v", err)
	}
	if calls == 0 {
		t.Fatal("设置后应重装调度任务")
	}
	if !gotEnabled {
		t.Error("已配置来源且启用时应传 enabled=true")
	}
	if gotCron != "0 */5 * * * *" {
		t.Errorf("重装用的 Cron 不对，实际 %q", gotCron)
	}

	// 切到 none：不该再挂任务，省掉每轮空转
	if _, err := svc.SetRemoteSource(RemoteSourceInput{Type: domain.RemoteSourceNone}); err != nil {
		t.Fatalf("切到 none 失败: %v", err)
	}
	if gotEnabled {
		t.Error("none 来源时应传 enabled=false")
	}
}

// 来源失效回落时不应把用户配的 Cron 一起丢掉：
// 调度设置与来源选择是两件独立的事。
func TestInvalidSourceKeepsCronSettings(t *testing.T) {
	svc, db := newTestSettingsService(t)

	if _, err := svc.SetRemoteSource(RemoteSourceInput{
		Type: domain.RemoteSourceURL, URL: "https://example.com/s", Cron: "0 20 1 * * *",
	}); err != nil {
		t.Fatalf("设置失败: %v", err)
	}
	// 手工把来源改成非法状态（模拟实体被删或版本回退）
	if err := db.SetSetting("remote.source.type", domain.RemoteSourceSubscription); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := db.SetSetting("remote.source.id", "0"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	got := svc.GetRemoteSource()
	if got.Type != domain.RemoteSourceNone {
		t.Errorf("非法来源应回落为 none，实际 %q", got.Type)
	}
	if got.Cron != "0 20 1 * * *" {
		t.Errorf("回落不应丢弃 Cron 设置，实际 %q", got.Cron)
	}
}
