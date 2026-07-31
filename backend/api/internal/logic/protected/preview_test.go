package protected

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"auroramihomo/backend/api/internal/config"
	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/model"
)

func newPreviewLogic(t *testing.T) (*PreviewLogic, *svc.ServiceContext) {
	t.Helper()
	dir := t.TempDir()

	cfg := config.Config{}
	cfg.DataSource = filepath.Join(dir, "preview.db")
	cfg.Mihomo.ConfigDir = dir
	cfg.Auth.AccessSecret = "secret12345678901234567890123456"
	// 预览不需要下载组件，关掉启动期的组件拉取
	cfg.Bootstrap.EnsureOnStart = false

	svcCtx := svc.NewServiceContext(cfg)
	t.Cleanup(func() { _ = svcCtx.Database.Close() })
	return NewPreviewLogic(context.Background(), svcCtx), svcCtx
}

// 一条无需回源的手动节点内容，便于在离线环境下稳定预览
const previewNodeContent = "ss://YWVzLTI1Ni1nY206cHc=@1.1.1.1:8388#HK-Node\n"

// 预览必须是纯读操作：它服务于「改完立刻看效果」，
// 若顺手落库，用户点一次预览就等于保存了一份自己还没确认的配置。
func TestPreviewDoesNotPersist(t *testing.T) {
	l, svcCtx := newPreviewLogic(t)

	before, err := svcCtx.Database.GetSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	filesBefore, err := svcCtx.Database.ListFiles()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := l.Preview(&types.PreviewReq{
		Kind:    "subscription",
		Content: previewNodeContent,
	}); err != nil {
		t.Fatalf("预览订阅失败: %v", err)
	}
	if _, err := l.Preview(&types.PreviewReq{
		Kind:       "file",
		ConfigType: model.FileConfigTypeFile,
		Content:    "payload:\n  - DOMAIN-SUFFIX,example.com\n",
	}); err != nil {
		t.Fatalf("预览文件失败: %v", err)
	}

	after, err := svcCtx.Database.GetSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	filesAfter, err := svcCtx.Database.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("预览不应新增订阅记录：%d -> %d", len(before), len(after))
	}
	if len(filesAfter) != len(filesBefore) {
		t.Errorf("预览不应新增文件记录：%d -> %d", len(filesBefore), len(filesAfter))
	}
}

// 预览返回处理前后的对照：管道生效时两侧应不同。
func TestPreviewSubscriptionShowsPipelineEffect(t *testing.T) {
	l, _ := newPreviewLogic(t)

	// 给节点名加前缀，是最容易在输出中辨认的算子
	res, err := l.Preview(&types.PreviewReq{
		Kind:    "subscription",
		Content: previewNodeContent,
		Operators: []types.PipelineOperator{
			{Type: "rename", Enabled: true, Payload: `{"pattern":"HK","replace":"香港"}`},
		},
	})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("应解析出 1 个节点，实际 %d", res.Count)
	}
	if !strings.Contains(res.Original, "HK-Node") {
		t.Errorf("原始侧应为未处理内容，实际:\n%s", res.Original)
	}
	if !strings.Contains(res.Processed, "香港") {
		t.Errorf("处理侧应体现改名算子，实际:\n%s", res.Processed)
	}
}

func TestPreviewRejectsUnknownKind(t *testing.T) {
	l, _ := newPreviewLogic(t)
	if _, err := l.Preview(&types.PreviewReq{Kind: "nope"}); err == nil {
		t.Fatal("未知预览类型应报错")
	}
}

// 订阅既没填地址也没粘贴内容时，应给出可读提示而不是引擎的英文错误
func TestPreviewSubscriptionRequiresSource(t *testing.T) {
	l, _ := newPreviewLogic(t)
	_, err := l.Preview(&types.PreviewReq{Kind: "subscription"})
	if err == nil {
		t.Fatal("缺少来源应报错")
	}
	if !strings.Contains(err.Error(), "订阅地址") {
		t.Errorf("错误信息应说明缺什么，实际: %v", err)
	}
}

func TestPreviewCollectionRequiresSubs(t *testing.T) {
	l, _ := newPreviewLogic(t)
	if _, err := l.Preview(&types.PreviewReq{Kind: "collection"}); err == nil {
		t.Fatal("未选订阅时应报错")
	}
}

// mihomo 配置类型的文件必须有节点来源，否则渲染不出内容
func TestPreviewFileMihomoRequiresSource(t *testing.T) {
	l, _ := newPreviewLogic(t)
	_, err := l.Preview(&types.PreviewReq{
		Kind:       "file",
		ConfigType: model.FileConfigTypeMihomo,
		Content:    "proxies: []\n",
	})
	if err == nil {
		t.Fatal("缺少节点来源应报错")
	}
}

// 文件原样输出时前后一致，不应凭空产生差异
func TestPreviewFilePlainIsIdentical(t *testing.T) {
	l, _ := newPreviewLogic(t)
	body := "payload:\n  - DOMAIN-SUFFIX,example.com\n"
	res, err := l.Preview(&types.PreviewReq{
		Kind:       "file",
		ConfigType: model.FileConfigTypeFile,
		Content:    body,
	})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if res.Original != res.Processed {
		t.Errorf("原样输出型文件的前后内容应一致\n original: %q\n processed: %q", res.Original, res.Processed)
	}
}
