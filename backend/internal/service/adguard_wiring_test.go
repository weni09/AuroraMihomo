package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildWiringPlan_TProxyConflictRedirectsMihomo(t *testing.T) {
	opts := WiringOptions{
		RedirectTProxy:  true,
		ResolveConflict: true,
		PatchUpstream:   true,
	}
	cur := currentDNSState{
		AGHDNSPort:       1053,
		MihomoDNSListen:  "0.0.0.0:1053",
		MihomoDNSPort:    1053,
		MihomoDNSEnabled: true,
		TProxyEnabled:    true,
	}
	plan, err := buildWiringPlan(opts, cur)
	if err != nil {
		t.Fatalf("buildWiringPlan: %v", err)
	}
	if !plan.DidRedirect {
		t.Fatal("期望 DidRedirect=true")
	}
	if !plan.DidResolveConflict {
		t.Fatal("期望 DidResolveConflict=true（同端口 1053）")
	}
	if plan.MihomoDNSListen != "127.0.0.1:1054" {
		t.Fatalf("冲突后 mihomo listen 期望 127.0.0.1:1054，实际 %q", plan.MihomoDNSListen)
	}
	if !plan.DidPatchUpstream {
		t.Fatal("期望 DidPatchUpstream=true")
	}
	if !plan.WiringOn {
		t.Fatal("计划应标记 WiringOn")
	}
	if plan.OriginalDNSPort != 1053 {
		t.Fatalf("OriginalDNSPort 期望 1053，实际 %d", plan.OriginalDNSPort)
	}
	if plan.OriginalMihomoListen != "0.0.0.0:1053" {
		t.Fatalf("OriginalMihomoListen 不符: %q", plan.OriginalMihomoListen)
	}

	joined := strings.Join(plan.Actions, " | ")
	if !strings.Contains(joined, "TProxy DNS") {
		t.Fatalf("Actions 应含 TProxy DNS 重定向: %v", plan.Actions)
	}
	if !strings.Contains(joined, "127.0.0.1:1054") {
		t.Fatalf("Actions 应含改 mihomo listen: %v", plan.Actions)
	}
	if !strings.Contains(joined, "上游") {
		t.Fatalf("Actions 应含上游补丁: %v", plan.Actions)
	}
}

func TestBuildWiringPlan_NoTProxySkipsRedirectWithWarning(t *testing.T) {
	opts := WiringOptions{
		RedirectTProxy:  true,
		ResolveConflict: false,
		PatchUpstream:   false,
	}
	cur := currentDNSState{
		AGHDNSPort:    1053,
		TProxyEnabled: false,
	}
	plan, err := buildWiringPlan(opts, cur)
	if err != nil {
		t.Fatalf("buildWiringPlan: %v", err)
	}
	if plan.DidRedirect {
		t.Fatal("无 TProxy 时不应 DidRedirect")
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("无有效动作时 Actions 应空，实际 %v", plan.Actions)
	}
	if len(plan.Warnings) == 0 {
		t.Fatal("应有 warning 说明跳过 Redirect")
	}
	found := false
	for _, w := range plan.Warnings {
		if strings.Contains(w, "TProxy") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warning 应提及 TProxy: %v", plan.Warnings)
	}
}

func TestBuildWiringPlan_NoConflictSkipsListenChange(t *testing.T) {
	opts := WiringOptions{ResolveConflict: true, RedirectTProxy: true}
	cur := currentDNSState{
		AGHDNSPort:      1053,
		MihomoDNSListen: "0.0.0.0:5353",
		MihomoDNSPort:   5353,
		TProxyEnabled:   true,
	}
	plan, err := buildWiringPlan(opts, cur)
	if err != nil {
		t.Fatal(err)
	}
	if plan.DidResolveConflict {
		t.Fatal("端口不同时不应改 listen")
	}
	if plan.MihomoDNSListen != "" {
		t.Fatalf("无冲突时 MihomoDNSListen 应空，实际 %q", plan.MihomoDNSListen)
	}
	if !plan.DidRedirect {
		t.Fatal("TProxy 启用时应 Redirect")
	}
}

func TestBuildWiringPlan_InvalidAGHPort(t *testing.T) {
	_, err := buildWiringPlan(WiringOptions{}, currentDNSState{AGHDNSPort: 0})
	if err == nil {
		t.Fatal("AGH 端口为 0 应报错")
	}
}

func TestWiringSnapshotJSONRoundtrip(t *testing.T) {
	plan := WiringPlan{
		Actions:              []string{"TProxy DNS 重定向目标 → AdGuard :1053", "改 mihomo dns.listen → 127.0.0.1:1054"},
		AGHDNSPort:           1053,
		MihomoDNSListen:      "127.0.0.1:1054",
		OriginalDNSPort:      1053,
		OriginalMihomoListen: "0.0.0.0:1053",
		OriginalUpstream:     []string{"https://dns.example/dns-query"},
		WiringOn:             true,
		DidRedirect:          true,
		DidResolveConflict:   true,
		DidPatchUpstream:     true,
	}
	raw, err := marshalWiringSnapshot(plan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// 任务约定的 snapshot 关键字段必须存在
	var generic map[string]any
	if err := json.Unmarshal([]byte(raw), &generic); err != nil {
		t.Fatalf("json: %v", err)
	}
	for _, key := range []string{"originalDNSPort", "originalMihomoListen", "wiringOn", "actions"} {
		if _, ok := generic[key]; !ok {
			t.Fatalf("snapshot 缺少字段 %q，raw=%s", key, raw)
		}
	}
	got, err := unmarshalWiringSnapshot(raw)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.OriginalDNSPort != 1053 || got.OriginalMihomoListen != "0.0.0.0:1053" {
		t.Fatalf("roundtrip 端口/listen 丢失: %+v", got)
	}
	if !got.WiringOn || !got.DidRedirect || !got.DidResolveConflict {
		t.Fatalf("roundtrip 标志丢失: %+v", got)
	}
	if len(got.Actions) != 2 {
		t.Fatalf("actions roundtrip: %v", got.Actions)
	}
	if len(got.OriginalUpstream) != 1 || got.OriginalUpstream[0] != "https://dns.example/dns-query" {
		t.Fatalf("upstream roundtrip: %v", got.OriginalUpstream)
	}
}

func TestWiringUpstreamForPlan(t *testing.T) {
	plan := WiringPlan{DidResolveConflict: true, MihomoDNSListen: "127.0.0.1:1054"}
	up := wiringUpstreamForPlan(plan, 1053)
	if len(up) != 1 || up[0] != "127.0.0.1:1054" {
		t.Fatalf("冲突解决后上游应指向新端口, got %v", up)
	}
	plan2 := WiringPlan{}
	up2 := wiringUpstreamForPlan(plan2, 1053)
	if len(up2) != 1 || up2[0] != "127.0.0.1:1053" {
		t.Fatalf("无冲突时上游应指向原端口, got %v", up2)
	}
}

func TestConflictMihomoListen(t *testing.T) {
	if got := conflictMihomoListen(1053); got != "127.0.0.1:1054" {
		t.Fatalf("1053 → %q", got)
	}
	if got := conflictMihomoListen(5353); got != "127.0.0.1:5354" {
		t.Fatalf("5353 → %q", got)
	}
}

func TestDNSPortOverridePriority(t *testing.T) {
	// 覆盖路径：override 优先于 dnsPortFn
	s := &TransparentService{}
	s.SetDNSPortFn(func() int { return 1053 })
	s.SetDNSPortOverride(func() int { return 1053 + 100 }) // 任意 AGH 端口
	if p := s.dnsPort(); p != 1153 {
		t.Fatalf("override 应优先，got %d", p)
	}
	s.SetDNSPortOverride(nil)
	if p := s.dnsPort(); p != 1053 {
		t.Fatalf("清除 override 后应回落 dnsPortFn，got %d", p)
	}
	s.SetDNSPortOverride(func() int { return 0 })
	if p := s.dnsPort(); p != 1053 {
		t.Fatalf("override 返回 0 应回落 dnsPortFn，got %d", p)
	}
}
