package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"auroramihomo/backend/internal/model"
)

// newTestRenderService 复用 render_cache_test.go 中的定义

// 原样输出型文件不经模板渲染，内容必须逐字返回。
// 规则片段、Surge 模块等含 {{ }} 的内容若被当模板处理会被破坏。
func TestRenderFileRawReturnsContentVerbatim(t *testing.T) {
	svc, _ := newTestRenderService(t)
	raw := "payload:\n  - DOMAIN-SUFFIX,example.com\n  # 含 {{ 花括号 }} 也不应被解析\n"
	f := &model.SubFile{
		Name:       "rules.yaml",
		Content:    raw,
		ConfigType: model.FileConfigTypeFile,
	}
	got, err := svc.RenderFile(context.Background(), f)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if got != raw {
		t.Errorf("原样输出型文件内容应逐字返回\n want: %q\n got:  %q", raw, got)
	}
}

// 存量数据的 ConfigType 为空串，必须按原样输出处理，
// 否则升级后这些文件会突然被当作模板渲染而失败。
func TestRenderFileEmptyConfigTypeTreatedAsRaw(t *testing.T) {
	svc, _ := newTestRenderService(t)
	f := &model.SubFile{Name: "legacy", Content: "legacy content", ConfigType: ""}
	got, err := svc.RenderFile(context.Background(), f)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if got != "legacy content" {
		t.Errorf("ConfigType 为空应视为原样输出，实际 %q", got)
	}
}

// mihomo 模板套用所选订阅的节点渲染。
func TestRenderFileMihomoTemplateAppliesNodes(t *testing.T) {
	svc, db := newTestRenderService(t)

	sub := &model.Subscription{
		Name: "airport", Enabled: 1, Type: "mihomo",
		Content:    "ss://YWVzLTI1Ni1nY206cGFzcw==@a.example.com:8388#TokyoNode",
		ShareToken: "tok-1",
	}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}

	f := &model.SubFile{
		Name:       "my-config",
		ConfigType: model.FileConfigTypeMihomo,
		SourceType: model.SourceTypeSubscription,
		SourceID:   sub.ID,
		Content:    "proxies:\n{{ range .Nodes }}  - name: \"{{ .Name }}\"\n    server: {{ .Server }}\n{{ end }}",
	}
	got, err := svc.RenderFile(context.Background(), f)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(got, "TokyoNode") {
		t.Errorf("渲染结果应包含订阅节点名，实际:\n%s", got)
	}
	if !strings.Contains(got, "a.example.com") {
		t.Errorf("渲染结果应包含节点服务器地址，实际:\n%s", got)
	}
	// 模板指令必须已被求值，不能残留原文
	if strings.Contains(got, "range .Nodes") {
		t.Errorf("模板未被渲染，仍残留指令:\n%s", got)
	}
}

// 未指定节点来源的模板必须报错，而不是产出一份空配置。
func TestRenderFileMihomoRequiresSource(t *testing.T) {
	svc, _ := newTestRenderService(t)
	f := &model.SubFile{
		Name:       "no-source",
		ConfigType: model.FileConfigTypeMihomo,
		Content:    "proxies: []",
	}
	_, err := svc.RenderFile(context.Background(), f)
	if err == nil {
		t.Fatal("未指定节点来源应报错")
	}
	if !strings.Contains(err.Error(), "未指定节点来源") {
		t.Errorf("错误信息应说明缺少来源，实际: %v", err)
	}
}

// 模板内容为空时报错，避免对外提供空配置。
func TestRenderFileMihomoRejectsEmptyTemplate(t *testing.T) {
	svc, _ := newTestRenderService(t)
	f := &model.SubFile{
		Name:       "empty",
		ConfigType: model.FileConfigTypeMihomo,
		SourceID:   1,
		Content:    "  \n ",
	}
	if _, err := svc.RenderFile(context.Background(), f); err == nil {
		t.Fatal("空模板应报错")
	}
}

// RenderFileTemplate 供「文件作为远程来源」使用，
// 必须拒绝原样输出型文件——它渲染不出可合并的配置。
func TestRenderFileTemplateRejectsRawFile(t *testing.T) {
	svc, db := newTestRenderService(t)
	f := &model.SubFile{
		Name: "plain", Content: "hello",
		ConfigType: model.FileConfigTypeFile,
		ShareToken: "tok-file",
	}
	if err := db.SaveFile(f); err != nil {
		t.Fatalf("保存文件失败: %v", err)
	}
	_, err := svc.RenderFileTemplate(context.Background(), f.ID)
	if err == nil {
		t.Fatal("原样输出型文件不应能作为配置来源")
	}
	if !strings.Contains(err.Error(), "不是 Mihomo 配置") {
		t.Errorf("错误信息应说明类型不符，实际: %v", err)
	}
}

// 组合作为文件模板来源时，组合级处理管道必须生效（与 renderCollection 对齐）。
//
// 回归：此前 fileSourceRequests 只带订阅自身算子、renderFile 以 nil 调用
// ConvertMany，组合上的 flag 等算子在「文件模板作为远程来源」路径被整体丢弃。
// 症状即生产实测：组合预览正常显示国旗，配置中心拉取合并后（remote.source 指向
// 文件模板、模板来源是组合）节点没有国旗。
func TestRenderFileCollectionSourceAppliesCollectionOperators(t *testing.T) {
	svc, db := newTestRenderService(t)

	// 两个成员订阅，节点名含地区关键词（region.go 的 HK/JP 词典命中）
	subs := []*model.Subscription{
		{Name: "hk", Enabled: 1, Content: "ss://YWVzLTI1Ni1nY206cHc=@1.1.1.1:8388#香港 01\n"},
		{Name: "jp", Enabled: 1, Content: "ss://YWVzLTI1Ni1nY206cHc=@2.2.2.2:8388#日本 01\n"},
	}
	for _, s := range subs {
		if err := db.CreateSubscription(s); err != nil {
			t.Fatalf("创建订阅失败: %v", err)
		}
	}

	// 组合挂上两条订阅，并配置自动国旗
	coll := &model.SubCollection{
		Name:      "flag-collection",
		Enabled:   1,
		Operators: `[{"type":"flag","enabled":true,"payload":"{}"}]`,
	}
	if err := db.CreateCollection(coll); err != nil {
		t.Fatalf("创建组合失败: %v", err)
	}
	if err := db.ReplaceCollectionItems(coll.ID, []int64{subs[0].ID, subs[1].ID}); err != nil {
		t.Fatalf("组合挂订阅失败: %v", err)
	}

	f := &model.SubFile{
		Name:         "tpl",
		ConfigType:   model.FileConfigTypeMihomo,
		SourceType:   model.SourceTypeCollection,
		SourceID:     coll.ID,
		TemplateLang: model.TemplateLangGo,
		Content:      "proxies:\n{{ range .Nodes }}  - name: \"{{ .Name }}\"\n    server: {{ .Server }}\n{{ end }}",
	}
	got, err := svc.RenderFile(context.Background(), f)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(got, "🇭🇰") {
		t.Errorf("组合的 flag 算子应作用于文件模板来源节点，结果缺香港国旗:\n%s", got)
	}
	if !strings.Contains(got, "🇯🇵") {
		t.Errorf("组合的 flag 算子应作用于文件模板来源节点，结果缺日本国旗:\n%s", got)
	}
}

func TestShareExpiredHelper(t *testing.T) {
	// 零值表示永不过期
	if shareExpired(time.Time{}) {
		t.Error("零值有效期应视为永不过期")
	}
	if !shareExpired(time.Now().Add(-time.Minute)) {
		t.Error("过去的时间应判定为已过期")
	}
	if shareExpired(time.Now().Add(time.Hour)) {
		t.Error("将来的时间不应判定为过期")
	}
}

// 过期的分享必须以 ErrShareExpired 拒绝，供上层回 410。
func TestRenderByTokenRejectsExpiredShare(t *testing.T) {
	svc, db := newTestRenderService(t)

	sub := &model.Subscription{
		Name: "expiring", Enabled: 1, Type: "mihomo",
		Content:    "ss://YWVzLTI1Ni1nY206cGFzcw==@a.example.com:8388#NodeA",
		ShareToken: "expired-token",
	}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}
	if err := db.UpdateSubscriptionShare(sub.ID, "已过期的分享", time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("设置有效期失败: %v", err)
	}

	_, err := svc.RenderByToken(context.Background(), "expired-token", "", "")
	if !errors.Is(err, ErrShareExpired) {
		t.Fatalf("过期分享应返回 ErrShareExpired，实际: %v", err)
	}
}

// 撤销分享后 token 为空串，空 token 请求不得匹配到任何记录。
// 否则「撤销」形同虚设：访问 /api/v1/share/ 就能拿到已撤销的内容。
func TestEmptyTokenNeverMatches(t *testing.T) {
	_, db := newTestRenderService(t)

	sub := &model.Subscription{
		Name: "revoked", Enabled: 1, Type: "mihomo",
		Content:    "ss://YWVzLTI1Ni1nY206cGFzcw==@a.example.com:8388#NodeA",
		ShareToken: "will-be-cleared",
	}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}
	if err := db.ClearSubscriptionShareToken(sub.ID); err != nil {
		t.Fatalf("撤销失败: %v", err)
	}

	if _, err := db.GetSubscriptionByToken(""); err == nil {
		t.Error("空 token 不应匹配到订阅")
	}
	if _, err := db.GetCollectionByToken(""); err == nil {
		t.Error("空 token 不应匹配到组合")
	}
	if _, err := db.GetFileByToken(""); err == nil {
		t.Error("空 token 不应匹配到文件")
	}
	// 原 token 也应失效
	if _, err := db.GetSubscriptionByToken("will-be-cleared"); err == nil {
		t.Error("撤销后原 token 应失效")
	}
}

// 撤销文件分享后，启动时的 token 补发逻辑不得把它重新激活。
func TestBackfillSkipsRevokedFileShares(t *testing.T) {
	_, db := newTestRenderService(t)

	f := &model.SubFile{Name: "revoked-file", Content: "x", ShareToken: "tok-old"}
	if err := db.SaveFile(f); err != nil {
		t.Fatalf("保存文件失败: %v", err)
	}
	if err := db.ClearFileShareToken(f.ID); err != nil {
		t.Fatalf("撤销失败: %v", err)
	}

	if err := db.BackfillFileShareTokens(func() string { return "regenerated" }); err != nil {
		t.Fatalf("补发失败: %v", err)
	}

	got, err := db.GetFile(f.ID)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if got.ShareToken != "" {
		t.Errorf("已撤销的分享不应被补发凭据，实际 token=%q", got.ShareToken)
	}
}

// 从未有过 token 的历史文件仍应被补发，保证升级后直链可用。
func TestBackfillFillsMissingTokens(t *testing.T) {
	_, db := newTestRenderService(t)

	f := &model.SubFile{Name: "legacy-file", Content: "x"}
	if err := db.SaveFile(f); err != nil {
		t.Fatalf("保存文件失败: %v", err)
	}
	if err := db.BackfillFileShareTokens(func() string { return "regenerated" }); err != nil {
		t.Fatalf("补发失败: %v", err)
	}
	got, err := db.GetFile(f.ID)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if got.ShareToken != "regenerated" {
		t.Errorf("历史文件应被补发凭据，实际 token=%q", got.ShareToken)
	}
}

// 重置文件分享要同时清除撤销标记，否则重新启用后
// 下次重启的补发逻辑又会跳过它（虽然此时已有 token，但标记不一致易埋坑）。
func TestResetFileShareClearsRevokedFlag(t *testing.T) {
	_, db := newTestRenderService(t)

	f := &model.SubFile{Name: "toggle", Content: "x", ShareToken: "t1"}
	if err := db.SaveFile(f); err != nil {
		t.Fatalf("保存文件失败: %v", err)
	}
	if err := db.ClearFileShareToken(f.ID); err != nil {
		t.Fatalf("撤销失败: %v", err)
	}
	if err := db.ResetFileShareToken(f.ID, "t2"); err != nil {
		t.Fatalf("重置失败: %v", err)
	}
	got, err := db.GetFile(f.ID)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.ShareToken != "t2" {
		t.Errorf("token 应为新值，实际 %q", got.ShareToken)
	}
	if got.ShareRevoked != 0 {
		t.Error("重置后应清除撤销标记")
	}
}
