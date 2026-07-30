package service

import (
	"context"
	"strings"
	"testing"

	"auroramihomo/backend/internal/domain"
)

// GetFinalConfig / GetRemoteConfig 供「配置差异」页展示只读 YAML。
// 两者在从未合并过时必须返回空字符串而非报错——页面据此判断是否显示
// 「尚未生成」提示，误报错会让整页因单个分区失败而不可用。

func TestGetFinalConfigEmptyBeforeAnyMerge(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	content, err := svc.GetFinalConfig()
	if err != nil {
		t.Fatalf("未合并过时不应报错: %v", err)
	}
	if content != "" {
		t.Errorf("未合并过时应为空字符串，实际: %q", content)
	}
}

func TestGetRemoteConfigEmptyWhenNoRemoteSource(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	content, err := svc.GetRemoteConfig()
	if err != nil {
		t.Fatalf("未配置远程来源时不应报错: %v", err)
	}
	if content != "" {
		t.Errorf("未配置远程来源时应为空字符串，实际: %q", content)
	}
}

// 合并成功后，GetFinalConfig 应能读到刚生成的最终配置。
func TestGetFinalConfigReflectsLastMerge(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
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
	if _, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0)); err != nil {
		t.Fatalf("合并失败: %v", err)
	}

	content, err := svc.GetFinalConfig()
	if err != nil {
		t.Fatalf("读取最终配置失败: %v", err)
	}
	if !strings.Contains(content, "HK01") {
		t.Errorf("最终配置应包含合并进去的节点，实际:\n%s", content)
	}
}

// 配置了远程来源并构建后，GetRemoteConfig 应能读到远程层原始内容
// （合并前，不受本地配置影响）。
func TestGetRemoteConfigReflectsBuiltRemoteLayer(t *testing.T) {
	svc, _, _ := newTestConfigService(t)

	svc.SetRemoteSourceProvider(func() domain.RemoteSource {
		return domain.RemoteSource{Type: domain.RemoteSourceCollection, ID: 1}
	})
	svc.SetRenderers(
		func(_ context.Context, id int64) (string, error) {
			return "proxies:\n  - name: FromRemote" +
				"\n    type: ss\n    server: r.example.com\n    port: 8388" +
				"\n    cipher: aes-256-gcm\n    password: pass\n", nil
		},
		nil,
	)

	if err := svc.buildRemoteConfig(context.Background(), 0); err != nil {
		t.Fatalf("构建远程层失败: %v", err)
	}

	content, err := svc.GetRemoteConfig()
	if err != nil {
		t.Fatalf("读取远程层失败: %v", err)
	}
	if !strings.Contains(content, "FromRemote") {
		t.Errorf("远程层应为构建出的原始内容，实际:\n%s", content)
	}
}
