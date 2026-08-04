package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"auroramihomo/backend/internal/substore"
)

// mockAirport 模拟 V2Board 类机场：按 flag 参数返回不同响应。
//   - 无参数：base64 节点列表，无 subscription-userinfo 头
//   - flag=clashmeta：Clash YAML 完整节点 + userinfo 头
//   - flag=clash：占位提示节点（"当前Clash客户端不支持本机场协议"）+ userinfo 头
func mockAirport() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("flag") {
		case "clashmeta":
			w.Header().Set("Content-Type", "text/yaml")
			w.Header().Set("subscription-userinfo", "upload=100; download=200; total=1073741824000")
			fmt.Fprint(w, "proxies:\n"+
				"  - name: 节点A\n    type: vless\n    server: a.example.com\n    port: 443\n    uuid: 11111111-1111-1111-1111-111111111111\n    network: ws\n"+
				"  - name: 节点B\n    type: vless\n    server: b.example.com\n    port: 443\n    uuid: 22222222-2222-2222-2222-222222222222\n    network: ws\n")
		case "clash":
			w.Header().Set("Content-Type", "text/yaml")
			w.Header().Set("subscription-userinfo", "upload=100; download=200; total=1073741824000")
			fmt.Fprint(w, "proxies:\n"+
				"  - name: 当前Clash客户端不支持本机场协议\n    type: vless\n    server: c.example.com\n    port: 443\n    uuid: 33333333-3333-3333-3333-333333333333\n")
		default:
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			body := base64.StdEncoding.EncodeToString([]byte(
				"vless://11111111-1111-1111-1111-111111111111@a.example.com:443?type=tcp#节点A\n" +
					"vless://22222222-2222-2222-2222-222222222222@b.example.com:443?type=tcp#节点B\n"))
			fmt.Fprint(w, body)
		}
	}))
}

func TestProbeSubscriptionParams(t *testing.T) {
	srv := mockAirport()
	defer srv.Close()

	cfg := &ConfigService{ssEngine: substore.NewEngine()}
	candidates, bestURL := cfg.ProbeSubscriptionParams(context.Background(), srv.URL, "")

	if len(candidates) != len(probeCandidates) {
		t.Fatalf("候选数 = %d, want %d", len(candidates), len(probeCandidates))
	}

	byParams := map[string]*ProbeCandidate{}
	for i := range candidates {
		byParams[candidates[i].Params] = &candidates[i]
	}

	// 无参数：有节点但无 userinfo
	base := byParams[""]
	if base == nil || base.HasUserInfo {
		t.Fatal("无参数组合不应有 userinfo")
	}
	if base.NodeCount != 2 {
		t.Fatalf("无参数组合节点数 = %d, want 2", base.NodeCount)
	}

	// flag=clashmeta：userinfo + 完整节点，且应为最佳
	meta := byParams["flag=clashmeta"]
	if meta == nil || !meta.HasUserInfo {
		t.Fatal("flag=clashmeta 应有 userinfo")
	}
	if meta.NodeCount != 2 {
		t.Fatalf("flag=clashmeta 节点数 = %d, want 2", meta.NodeCount)
	}
	if meta.TotalBytes != 1073741824000 {
		t.Fatalf("total = %d, want 1073741824000", meta.TotalBytes)
	}
	if meta.UsedBytes != 300 {
		t.Fatalf("used = %d, want 300", meta.UsedBytes)
	}

	// flag=clash：占位节点必须被识别为不可用
	clash := byParams["flag=clash"]
	if clash == nil || !clash.Placeholder {
		t.Fatal("flag=clash 应识别为占位节点")
	}

	// bestURL 应指向 flag=clashmeta 组合
	if bestURL != srv.URL+"?flag=clashmeta" {
		t.Fatalf("bestURL = %q, want %q", bestURL, srv.URL+"?flag=clashmeta")
	}
}

func TestAppendQueryParam(t *testing.T) {
	cases := []struct{ raw, params, want string }{
		{"https://a.com/sub", "flag=clashmeta", "https://a.com/sub?flag=clashmeta"},
		{"https://a.com/sub?OwO=abc", "flag=clashmeta", "https://a.com/sub?OwO=abc&flag=clashmeta"},
		{"https://a.com/sub?OwO=abc", "", "https://a.com/sub?OwO=abc"},
	}
	for _, c := range cases {
		if got := appendQueryParam(c.raw, c.params); got != c.want {
			t.Errorf("appendQueryParam(%q, %q) = %q, want %q", c.raw, c.params, got, c.want)
		}
	}
}

func TestNodesArePlaceholder(t *testing.T) {
	if !nodesArePlaceholder([]substore.Node{{Name: "当前Clash客户端不支持本机场协议"}}) {
		t.Error("占位节点名应被识别")
	}
	if nodesArePlaceholder([]substore.Node{{Name: "日本高速01"}, {Name: "香港01"}}) {
		t.Error("正常节点列表不应被识别为占位")
	}
	if nodesArePlaceholder(nil) {
		t.Error("空列表不应被识别为占位")
	}
}
