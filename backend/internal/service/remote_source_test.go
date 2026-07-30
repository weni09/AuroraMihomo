package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auroramihomo/backend/internal/domain"
	"auroramihomo/backend/internal/model"
)

// 非法来源（缺 ID）必须回落为默认的 none 而非让整次合并失败：
// 设置可能因实体被删或版本回退而失效，此时仍应产出可用配置
// （即用户的本地配置），而不是拿一份残缺的远程内容去合并。
func TestRemoteSourceFallsBackWhenInvalid(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		// subscription 类型却没给 ID，属于非法配置
		return domain.RemoteSource{Type: domain.RemoteSourceSubscription}
	})
	if got := svc.remoteSource(); got.Type != domain.RemoteSourceNone {
		t.Fatalf("非法配置应回落为 none，实际 %q", got.Type)
	}
}

func TestRemoteSourceValid(t *testing.T) {
	cases := []struct {
		src  domain.RemoteSource
		want bool
	}{
		{domain.RemoteSource{Type: domain.RemoteSourceAll}, true},
		// all 之外都必须带有效 ID
		{domain.RemoteSource{Type: domain.RemoteSourceSubscription, ID: 1}, true},
		{domain.RemoteSource{Type: domain.RemoteSourceSubscription}, false},
		{domain.RemoteSource{Type: domain.RemoteSourceCollection, ID: 2}, true},
		{domain.RemoteSource{Type: domain.RemoteSourceCollection, ID: 0}, false},
		{domain.RemoteSource{Type: domain.RemoteSourceFile, ID: 3}, true},
		{domain.RemoteSource{Type: "bogus", ID: 1}, false},
		{domain.RemoteSource{}, false},
	}
	for _, c := range cases {
		if got := c.src.Valid(); got != c.want {
			t.Errorf("Valid(%+v) = %v, want %v", c.src, got, c.want)
		}
	}
}

// 指定单条订阅作为来源时，其余订阅一律不得进入最终配置。
// 这是本次需求的核心：多机场场景下不再把所有节点混在一起。
func TestBuildRemoteFromSingleSubscriptionExcludesOthers(t *testing.T) {
	svc, db, _ := newTestConfigService(t)

	// 两条手动粘贴内容的订阅，无需回源即可转换
	subA := &model.Subscription{
		Name: "airport-a", Enabled: 1, Type: "mihomo",
		Content:    "ss://YWVzLTI1Ni1nY206cGFzcw==@a.example.com:8388#NodeA",
		ShareToken: "tok-a",
	}
	subB := &model.Subscription{
		Name: "airport-b", Enabled: 1, Type: "mihomo",
		Content:    "ss://YWVzLTI1Ni1nY206cGFzcw==@b.example.com:8388#NodeB",
		ShareToken: "tok-b",
	}
	if err := db.CreateSubscription(subA); err != nil {
		t.Fatalf("创建订阅A失败: %v", err)
	}
	if err := db.CreateSubscription(subB); err != nil {
		t.Fatalf("创建订阅B失败: %v", err)
	}

	// 只用 A
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceSubscription, ID: subA.ID}
	})
	if err := svc.buildRemoteConfig(context.Background(), 0); err != nil {
		t.Fatalf("构建远程配置失败: %v", err)
	}

	merged, err := db.GetRemoteMergedConfig()
	if err != nil {
		t.Fatalf("读取聚合配置失败: %v", err)
	}
	if !strings.Contains(merged.Content, "NodeA") {
		t.Errorf("应包含所选订阅的节点 NodeA，实际内容:\n%s", merged.Content)
	}
	if strings.Contains(merged.Content, "NodeB") {
		t.Errorf("不应包含未选中订阅的节点 NodeB，实际内容:\n%s", merged.Content)
	}
}

// 默认的 all 来源仍应聚合全部启用订阅，确保改造没破坏原有行为。
func TestBuildRemoteAllAggregatesEverySubscription(t *testing.T) {
	svc, db, _ := newTestConfigService(t)

	for _, spec := range []struct{ name, tag, token string }{
		{"airport-a", "NodeA", "tok-a"},
		{"airport-b", "NodeB", "tok-b"},
	} {
		sub := &model.Subscription{
			Name: spec.name, Enabled: 1, Type: "mihomo",
			Content:    "ss://YWVzLTI1Ni1nY206cGFzcw==@x.example.com:8388#" + spec.tag,
			ShareToken: spec.token,
		}
		if err := db.CreateSubscription(sub); err != nil {
			t.Fatalf("创建订阅失败: %v", err)
		}
	}

	// 默认已改为 none，聚合行为需显式选择 all
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceAll}
	})
	if err := svc.buildRemoteConfig(context.Background(), 0); err != nil {
		t.Fatalf("构建远程配置失败: %v", err)
	}
	merged, err := db.GetRemoteMergedConfig()
	if err != nil {
		t.Fatalf("读取聚合配置失败: %v", err)
	}
	for _, tag := range []string{"NodeA", "NodeB"} {
		if !strings.Contains(merged.Content, tag) {
			t.Errorf("all 来源应包含 %s，实际内容:\n%s", tag, merged.Content)
		}
	}
}

// 选中的订阅被删除或停用时必须显式报错。
// 若静默退化为「无远程配置」，用户会以为选中的订阅仍在生效，
// 而实际生成的是仅含本地配置的文件。
func TestBuildRemoteFailsWhenChosenSubscriptionMissing(t *testing.T) {
	svc, db, _ := newTestConfigService(t)

	sub := &model.Subscription{
		Name: "disabled-one", Enabled: 1, Type: "mihomo",
		Content:    "ss://YWVzLTI1Ni1nY206cGFzcw==@a.example.com:8388#NodeA",
		ShareToken: "tok-x",
	}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}
	// 必须用显式 Update 停用：Enabled 的模型标签是 gorm:"default:1"，
	// 建记录时传 0 属于 Go 零值，GORM 会跳过该字段并写入默认值 1，
	// 结果订阅仍是启用状态（这个坑很容易让测试假通过）。
	if err := db.DB.Model(&model.Subscription{}).Where("id = ?", sub.ID).
		Update("enabled", 0).Error; err != nil {
		t.Fatalf("停用订阅失败: %v", err)
	}

	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceSubscription, ID: sub.ID}
	})
	err := svc.buildRemoteConfig(context.Background(), 0)
	if err == nil {
		t.Fatal("选中已停用的订阅应报错，而不是静默产出空远程配置")
	}
	if !strings.Contains(err.Error(), "不存在或已禁用") {
		t.Errorf("错误信息应说明订阅不可用，实际: %v", err)
	}
}

// 渲染器未注入时应明确报错，而不是 nil 解引用 panic。
func TestBuildRemoteFromRendererRequiresRenderer(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceCollection, ID: 1}
	})
	err := svc.buildRemoteConfig(context.Background(), 0)
	if err == nil {
		t.Fatal("未注入渲染器时应报错")
	}
	if !strings.Contains(err.Error(), "渲染入口未注入") {
		t.Errorf("错误信息应指出渲染器缺失，实际: %v", err)
	}
}

// 组合/文件的输出模板可能被设成 base64 或明文链接等非 YAML 格式。
// 这类内容无法参与配置合并，必须在构建阶段就报错，
// 否则会把垃圾内容写进 config.yaml 再由内核拒绝加载。
func TestBuildRemoteFromRendererRejectsNonYAML(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceCollection, ID: 7}
	})
	svc.SetRenderers(
		func(_ context.Context, _ int64) (string, error) {
			// 形如 base64 订阅的输出：不是合法的 mihomo 配置映射
			return "c3M6Ly9abTl2WW1GeVltRjZDZz09", nil
		},
		nil,
	)
	err := svc.buildRemoteConfig(context.Background(), 0)
	if err == nil {
		t.Fatal("非 YAML 的渲染结果应被拒绝")
	}
	if !strings.Contains(err.Error(), "Mihomo YAML") {
		t.Errorf("错误信息应提示改用 Mihomo YAML 模板，实际: %v", err)
	}
}

// 渲染结果为空同样要报错，避免用空内容覆盖掉可用的旧配置。
func TestBuildRemoteFromRendererRejectsEmpty(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceFile, ID: 3}
	})
	svc.SetRenderers(nil, func(_ context.Context, _ int64) (string, error) {
		return "   \n", nil
	})
	err := svc.buildRemoteConfig(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "渲染结果为空") {
		t.Fatalf("空渲染结果应报错，实际: %v", err)
	}
}

// 组合来源的渲染结果应原样成为远程层内容。
func TestBuildRemoteFromCollectionUsesRenderedYAML(t *testing.T) {
	svc, db, _ := newTestConfigService(t)
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceCollection, ID: 42}
	})
	rendered := "proxies:\n  - name: FromCollection\n    type: ss\n    server: c.example.com\n    port: 8388\n    cipher: aes-256-gcm\n    password: pass\n"
	svc.SetRenderers(func(_ context.Context, id int64) (string, error) {
		if id != 42 {
			t.Errorf("应渲染所选组合 42，实际 %d", id)
		}
		return rendered, nil
	}, nil)

	if err := svc.buildRemoteConfig(context.Background(), 0); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	merged, err := db.GetRemoteMergedConfig()
	if err != nil {
		t.Fatalf("读取聚合配置失败: %v", err)
	}
	if !strings.Contains(merged.Content, "FromCollection") {
		t.Errorf("远程层应为组合的渲染结果，实际:\n%s", merged.Content)
	}
}

// 切换来源后，远程聚合层的内容必须随之改变。
//
// 这条覆盖的是端到端最关键的一环：用户在配置中心改了来源、点了合并，
// 参与合并的远程层是否真的换了。此前只验证过单次构建的结果，
// 没验证过「切换」这个动作本身。
//
// 注：不检查磁盘 config.yaml——没有 mihomo 二进制时校验会失败并回滚，
// 磁盘上不落文件。数据库里的聚合层才是本功能的直接产物。
func TestSwitchingRemoteSourceChangesMergedContent(t *testing.T) {
	svc, db, _ := newTestConfigService(t)

	subA := &model.Subscription{
		Name: "airport-a", Enabled: 1, Type: "mihomo",
		Content:    "ss://YWVzLTI1Ni1nY206cGFzcw==@a.example.com:8388#NodeA",
		ShareToken: "tok-a",
	}
	subB := &model.Subscription{
		Name: "airport-b", Enabled: 1, Type: "mihomo",
		Content:    "ss://YWVzLTI1Ni1nY206cGFzcw==@b.example.com:8388#NodeB",
		ShareToken: "tok-b",
	}
	if err := db.CreateSubscription(subA); err != nil {
		t.Fatalf("创建订阅A失败: %v", err)
	}
	if err := db.CreateSubscription(subB); err != nil {
		t.Fatalf("创建订阅B失败: %v", err)
	}

	// 可变的来源，模拟用户在界面上切换。
	// 起点显式设为 all（默认值已是 none，不适合做本测试的起点）
	current := domain.RemoteSource{Type: domain.RemoteSourceAll}
	svc.SetRemoteSourceProvider(func() domain.RemoteSource { return current })
	svc.SetRenderers(
		func(_ context.Context, id int64) (string, error) {
			return "proxies:\n  - name: FromCollection" +
				"\n    type: ss\n    server: c.example.com\n    port: 8388" +
				"\n    cipher: aes-256-gcm\n    password: pass\n", nil
		},
		nil,
	)

	readMerged := func() string {
		t.Helper()
		m, err := db.GetRemoteMergedConfig()
		if err != nil {
			t.Fatalf("读取聚合配置失败: %v", err)
		}
		return m.Content
	}

	// 1) all：两条订阅都在
	if err := svc.buildRemoteConfig(context.Background(), 0); err != nil {
		t.Fatalf("all 构建失败: %v", err)
	}
	got := readMerged()
	if !strings.Contains(got, "NodeA") || !strings.Contains(got, "NodeB") {
		t.Errorf("all 来源应含两条订阅的节点，实际:\n%s", got)
	}

	// 2) 切到单条订阅 B：只剩 B
	current = domain.RemoteSource{Type: domain.RemoteSourceSubscription, ID: subB.ID}
	if err := svc.buildRemoteConfig(context.Background(), 0); err != nil {
		t.Fatalf("单订阅构建失败: %v", err)
	}
	got = readMerged()
	if !strings.Contains(got, "NodeB") {
		t.Errorf("应含所选订阅B的节点，实际:\n%s", got)
	}
	if strings.Contains(got, "NodeA") {
		t.Errorf("切换后不应残留订阅A的节点，实际:\n%s", got)
	}

	// 3) 切到组合：内容换成渲染结果，订阅节点全部让位
	current = domain.RemoteSource{Type: domain.RemoteSourceCollection, ID: 5}
	if err := svc.buildRemoteConfig(context.Background(), 0); err != nil {
		t.Fatalf("组合构建失败: %v", err)
	}
	got = readMerged()
	if !strings.Contains(got, "FromCollection") {
		t.Errorf("应为组合的渲染结果，实际:\n%s", got)
	}
	if strings.Contains(got, "NodeA") || strings.Contains(got, "NodeB") {
		t.Errorf("组合来源下不应含订阅节点，实际:\n%s", got)
	}

	// 4) 切回 all：恢复聚合
	current = domain.RemoteSource{Type: domain.RemoteSourceAll}
	if err := svc.buildRemoteConfig(context.Background(), 0); err != nil {
		t.Fatalf("回落 all 构建失败: %v", err)
	}
	got = readMerged()
	if !strings.Contains(got, "NodeA") || !strings.Contains(got, "NodeB") {
		t.Errorf("切回 all 应恢复聚合，实际:\n%s", got)
	}
	if strings.Contains(got, "FromCollection") {
		t.Errorf("切回 all 后不应残留组合内容，实际:\n%s", got)
	}
}

// 默认必须是「不使用远程配置」。
//
// 这是需求明确要求的默认语义：用户没做选择时，最终配置就等于
// 配置中心里写的本地配置，不应被订阅内容悄悄改写。
func TestDefaultRemoteSourceIsNone(t *testing.T) {
	if got := domain.DefaultRemoteSource(); got.Type != domain.RemoteSourceNone {
		t.Fatalf("默认应为 none（不使用远程配置），实际 %q", got.Type)
	}
	svc, _, _ := newTestConfigService(t)
	if got := svc.remoteSource(); got.Type != domain.RemoteSourceNone {
		t.Fatalf("未注入提供者时应回落为 none，实际 %q", got.Type)
	}
}

// none 来源下即便库里有启用的订阅，远程层也必须为空，
// 从而让合并走「仅本地配置」路径。
func TestRemoteSourceNoneProducesEmptyRemoteLayer(t *testing.T) {
	svc, db, _ := newTestConfigService(t)

	sub := &model.Subscription{
		Name: "airport", Enabled: 1, Type: "mihomo",
		Content:    "ss://YWVzLTI1Ni1nY206cGFzcw==@a.example.com:8388#NodeA",
		ShareToken: "tok-a",
	}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}

	// 不注入提供者即为默认 none
	if err := svc.buildRemoteConfig(context.Background(), 0); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	merged, err := db.GetRemoteMergedConfig()
	if err != nil {
		t.Fatalf("读取聚合配置失败: %v", err)
	}
	if strings.TrimSpace(merged.Content) != "" {
		t.Errorf("none 来源的远程层必须为空，实际:\n%s", merged.Content)
	}
	if strings.Contains(merged.Content, "NodeA") {
		t.Error("none 来源不应引入任何订阅节点")
	}
}

// 从「有远程内容」切到 none 时，必须把旧的远程层清空。
// 若只是跳过写入，上一次的聚合结果会残留并继续参与合并——
// 用户改成「不使用远程配置」后却发现订阅节点还在。
func TestSwitchingToNoneClearsPreviousRemoteLayer(t *testing.T) {
	svc, db, _ := newTestConfigService(t)

	sub := &model.Subscription{
		Name: "airport", Enabled: 1, Type: "mihomo",
		Content:    "ss://YWVzLTI1Ni1nY206cGFzcw==@a.example.com:8388#NodeA",
		ShareToken: "tok-a",
	}
	if err := db.CreateSubscription(sub); err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}

	current := domain.RemoteSource{Type: domain.RemoteSourceAll}
	svc.SetRemoteSourceProvider(func() domain.RemoteSource { return current })

	// 先产出有内容的远程层
	if err := svc.buildRemoteConfig(context.Background(), 0); err != nil {
		t.Fatalf("all 构建失败: %v", err)
	}
	m, _ := db.GetRemoteMergedConfig()
	if !strings.Contains(m.Content, "NodeA") {
		t.Fatalf("前置条件不成立，远程层应含节点:\n%s", m.Content)
	}

	// 切到 none
	current = domain.RemoteSource{Type: domain.RemoteSourceNone}
	if err := svc.buildRemoteConfig(context.Background(), 0); err != nil {
		t.Fatalf("none 构建失败: %v", err)
	}
	m, err := db.GetRemoteMergedConfig()
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if strings.TrimSpace(m.Content) != "" {
		t.Errorf("切到 none 后残留了旧的远程层:\n%s", m.Content)
	}
}

// url 类型缺地址时应报错，而不是静默产出空配置。
func TestRemoteSourceURLRequiresAddress(t *testing.T) {
	// Valid 层面就应拦住
	if (domain.RemoteSource{Type: domain.RemoteSourceURL}).Valid() {
		t.Error("url 类型缺地址应判定为非法")
	}
	if (domain.RemoteSource{Type: domain.RemoteSourceURL, URL: "   "}).Valid() {
		t.Error("url 为空白应判定为非法")
	}
	if !(domain.RemoteSource{Type: domain.RemoteSourceURL, URL: "https://e.com/s"}).Valid() {
		t.Error("带地址的 url 类型应合法")
	}

	// 非法配置会被 remoteSource() 回落为 none，
	// 因此这里直接调用底层构建函数验证其自身的防御
	svc, _, _ := newTestConfigService(t)
	if err := svc.buildRemoteFromURL(context.Background(), "  "); err == nil {
		t.Error("空地址应报错")
	}
}

// 外部订阅链接抓到的内容若不是合法 mihomo 配置，必须报错，
// 避免把垃圾内容当远程层写进去。
func TestRemoteSourceURLRejectsInvalidContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 既不是节点链接也不是 YAML 映射
		_, _ = w.Write([]byte("!!!not a config!!!"))
	}))
	defer srv.Close()

	svc, _, _ := newTestConfigService(t)
	err := svc.buildRemoteFromURL(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("非法内容应报错")
	}
}

// 外部订阅链接返回完整 mihomo 配置时，应原样成为远程层。
func TestRemoteSourceURLAcceptsMihomoYAML(t *testing.T) {
	body := "proxies:\n  - name: FromExternal\n    type: ss\n    server: e.example.com\n    port: 8388\n    cipher: aes-256-gcm\n    password: pass\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	svc, db, _ := newTestConfigService(t)
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceURL, URL: srv.URL}
	})
	if err := svc.buildRemoteConfig(context.Background(), 0); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	merged, err := db.GetRemoteMergedConfig()
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if !strings.Contains(merged.Content, "FromExternal") {
		t.Errorf("远程层应为外部链接的内容，实际:\n%s", merged.Content)
	}
}

// 外部链接返回明文分享链接时，应经 Sub-Store 引擎转换成 mihomo 配置。
func TestRemoteSourceURLConvertsShareLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ss://YWVzLTI1Ni1nY206cGFzcw==@x.example.com:8388#ExternalNode"))
	}))
	defer srv.Close()

	svc, db, _ := newTestConfigService(t)
	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceURL, URL: srv.URL}
	})
	if err := svc.buildRemoteConfig(context.Background(), 0); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	merged, err := db.GetRemoteMergedConfig()
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if !strings.Contains(merged.Content, "ExternalNode") {
		t.Errorf("应把分享链接转换为节点，实际:\n%s", merged.Content)
	}
	if !strings.Contains(merged.Content, "proxies") {
		t.Errorf("转换结果应是 mihomo 配置，实际:\n%s", merged.Content)
	}
}
