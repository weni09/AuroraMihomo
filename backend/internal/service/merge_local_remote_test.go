package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"auroramihomo/backend/internal/domain"
	"auroramihomo/backend/internal/model"

	"gopkg.in/yaml.v3"
)

// localOnlyBaseYAML 覆盖一批需要原样保留的本地参数，
// 既有强类型字段（dns/tun/proxies/rules），也有归入 General 兜底 map 的
// 长尾官方参数（listeners/experimental/ntp）。
const localOnlyBaseYAML = `
mode: rule
mixed-port: 7890
external-controller: 127.0.0.1:9090
secret: s3cret
find-process-mode: strict
tcp-concurrent: true
dns:
  enable: true
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  nameserver:
    - 223.5.5.5
tun:
  enable: true
  stack: system
  auto-route: true
proxies:
  - name: LOCAL01
    type: ss
    server: local.example.com
    port: 8388
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - LOCAL01
rules:
  - DOMAIN-SUFFIX,local.test,DIRECT
  - MATCH,PROXY
listeners:
  - name: my-http
    type: http
    port: 8080
experimental:
  quic-go-disable-gso: true
ntp:
  enable: false
`

// 需求核心场景一：没有任何订阅时，最终配置应等于本地配置，
// 不能被空的远程层抹掉任何内容，也不应产生冲突。
func TestMergeWithoutSubscriptionsKeepsLocalConfigIntact(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	ctx := context.Background()

	if err := svc.UpdateBaseConfig(localOnlyBaseYAML); err != nil {
		t.Fatalf("写入本地配置失败: %v", err)
	}

	res, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0))
	if err != nil {
		t.Fatalf("无订阅时合并应成功: %v", err)
	}
	if res.ConflictCount != 0 {
		t.Errorf("无远程配置时不应产生冲突，实际 %d 条", res.ConflictCount)
	}

	raw, err := os.ReadFile(svc.configPath())
	if err != nil {
		t.Fatalf("读取生成配置失败: %v", err)
	}
	var got domain.Config
	if err := yaml.Unmarshal(raw, &got); err != nil {
		t.Fatalf("生成的配置不是合法 YAML: %v", err)
	}

	// 强类型字段
	if got.Mode != "rule" || got.MixedPort != 7890 {
		t.Errorf("基础标量参数丢失: mode=%q mixed-port=%d", got.Mode, got.MixedPort)
	}
	if got.FindProcessMode != "strict" || !got.TCPConcurrent {
		t.Errorf("系统级标量参数丢失: find-process-mode=%q tcp-concurrent=%v", got.FindProcessMode, got.TCPConcurrent)
	}
	if !got.DNS.Enable || got.DNS.EnhancedMode != "fake-ip" || got.DNS.FakeIPRange != "198.18.0.1/16" {
		t.Errorf("DNS 配置未原样保留: %+v", got.DNS)
	}
	if !got.TUN.Enable || got.TUN.Stack != "system" || got.TUN.AutoRoute == nil || !*got.TUN.AutoRoute {
		t.Errorf("TUN 配置未原样保留: %+v", got.TUN)
	}
	if len(got.Proxies) != 1 || got.Proxies[0].Name != "LOCAL01" {
		t.Errorf("本地节点未保留: %+v", got.Proxies)
	}
	if len(got.ProxyGroups) != 1 || got.ProxyGroups[0].Name != "PROXY" {
		t.Errorf("本地策略组未保留: %+v", got.ProxyGroups)
	}
	if len(got.Rules) != 2 || got.Rules[0] != "DOMAIN-SUFFIX,local.test,DIRECT" {
		t.Errorf("本地规则未原样保留: %+v", got.Rules)
	}

	// General 兜底 map 承载的长尾官方参数必须一并落盘
	for _, key := range []string{"listeners", "experimental", "ntp"} {
		if _, ok := got.General[key]; !ok {
			t.Errorf("长尾官方参数 %q 未出现在最终配置中；General=%v", key, keysOf(got.General))
		}
	}
}

// 需求核心场景二：远程聚合行内容为空字符串时同样视为"无远程配置"，
// 走仅本地路径。这是订阅全部被禁用后的实际存库状态。
func TestMergeWithEmptyRemoteRowUsesLocalOnly(t *testing.T) {
	svc, db, _ := newTestConfigService(t)
	ctx := context.Background()

	if err := svc.UpdateBaseConfig(localOnlyBaseYAML); err != nil {
		t.Fatalf("写入本地配置失败: %v", err)
	}
	if err := db.SaveConfig(&model.Config{
		Name: "remote-merged", Type: "remote", Content: "", Version: 1,
	}); err != nil {
		t.Fatalf("写入空远程聚合行失败: %v", err)
	}

	res, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0))
	if err != nil {
		t.Fatalf("空远程配置时合并应成功: %v", err)
	}
	if res.ConflictCount != 0 {
		t.Errorf("空远程配置不应产生冲突，实际 %d 条", res.ConflictCount)
	}

	raw, err := os.ReadFile(svc.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "LOCAL01") {
		t.Errorf("应保留本地节点，实际:\n%s", raw)
	}
	if !strings.Contains(string(raw), "listeners") {
		t.Errorf("应保留 General 中的 listeners，实际:\n%s", raw)
	}
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
