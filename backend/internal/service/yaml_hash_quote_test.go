package service

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestYAMLMarshalQuotesHashInDNS(t *testing.T) {
	m := map[string]any{
		"nameserver": []any{"8.8.8.8#✈️ 国外", "223.5.5.5"},
	}
	b, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	t.Log(s)
	if !strings.Contains(s, "8.8.8.8#") {
		t.Fatal("hash lost")
	}
	// if unquoted, YAML comment would drop 国外 on re-parse
	var back map[string]any
	if err := yaml.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	ns := back["nameserver"].([]any)
	if ns[0].(string) != "8.8.8.8#✈️ 国外" {
		t.Fatalf("reparse got %q", ns[0])
	}
}
