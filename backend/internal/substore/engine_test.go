package substore

import (
	"context"
	"strings"
	"testing"
)

func TestConvertClashYAML(t *testing.T) {
	raw := `
proxies:
  - name: "N1"
    type: ss
    server: 1.1.1.1
    port: 443
    cipher: aes-128-gcm
    password: pwd
`
	e := NewEngine()
	res, err := e.Convert(context.Background(), ConvertRequest{Content: raw}, nil, nil, "mihomo-yaml", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.CountNodes() != 1 && len(res.Nodes) != 1 {
		t.Fatalf("nodes=%d", len(res.Nodes))
	}
	if !strings.Contains(res.YAML, "N1") {
		t.Fatalf("yaml missing node: %s", res.YAML)
	}
}

func TestConvertShareLinkAndRewrite(t *testing.T) {
	// ss://base64(method:pass)@host:port#name
	// aes-128-gcm:pwd => YWVzLTEyOC1nY206cHdk
	content := "ss://YWVzLTEyOC1nY206cHdk@2.2.2.2:8443#OldName"
	e := NewEngine()
	rules := []RewriteRule{{Scope: "name", Pattern: "Old", Replace: "New", FilterMode: "rewrite", Enabled: true}}
	res, err := e.Convert(context.Background(), ConvertRequest{Content: content}, rules, nil, "mihomo-yaml", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes) != 1 {
		t.Fatalf("nodes=%d", len(res.Nodes))
	}
	if res.Nodes[0].Name != "NewName" {
		t.Fatalf("rewrite failed: %s", res.Nodes[0].Name)
	}
}

// helper to keep test resilient if Count field naming differs
func (r *ConvertResult) CountNodes() int { return len(r.Nodes) }
