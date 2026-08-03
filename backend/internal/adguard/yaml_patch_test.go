package adguard

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func writeAGHYaml(t *testing.T, dir, content string) {
	t.Helper()
	path := filepath.Join(dir, "AdGuardHome.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
}

func readAGHMap(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "AdGuardHome.yaml"))
	if err != nil {
		t.Fatalf("read yaml: %v", err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, raw)
	}
	return m
}

func TestReadDNSPort(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, "dns:\n  port: 1053\nbind_host: 127.0.0.1\n")
	port, err := ReadDNSPort(dir)
	if err != nil || port != 1053 {
		t.Fatalf("port=%d err=%v", port, err)
	}
}

func TestReadDNSPort_Custom(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, "dns:\n  port: 5353\n")
	port, err := ReadDNSPort(dir)
	if err != nil || port != 5353 {
		t.Fatalf("port=%d err=%v", port, err)
	}
}

func TestReadDNSPort_MissingYaml(t *testing.T) {
	dir := t.TempDir()
	port, err := ReadDNSPort(dir)
	if err != nil {
		t.Fatalf("缺失 yaml 应返回默认值而非错误: %v", err)
	}
	if port != 5353 {
		t.Fatalf("default port want 5353 got %d", port)
	}
}

func TestReadDNSPort_MissingPortKey(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, "dns:\n  bootstrap_dns:\n    - 1.1.1.1\n")
	port, err := ReadDNSPort(dir)
	if err != nil || port != 5353 {
		t.Fatalf("无 port 键应默认 5353, port=%d err=%v", port, err)
	}
}

func TestSetDNSPort(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, "bind_host: 127.0.0.1\ndns:\n  port: 1053\n  upstream_dns:\n    - 1.1.1.1\n")
	if err := SetDNSPort(dir, 53); err != nil {
		t.Fatalf("SetDNSPort: %v", err)
	}
	port, err := ReadDNSPort(dir)
	if err != nil || port != 53 {
		t.Fatalf("port=%d err=%v", port, err)
	}
	m := readAGHMap(t, dir)
	dns := asMap(m["dns"])
	if dns == nil {
		t.Fatal("dns 段丢失")
	}
	ups := asStringList(dns["upstream_dns"])
	if len(ups) != 1 || ups[0] != "1.1.1.1" {
		t.Fatalf("SetDNSPort 不应破坏 upstream: %#v", ups)
	}
}

func TestSetDNSPort_CreateYaml(t *testing.T) {
	dir := t.TempDir()
	if err := SetDNSPort(dir, 5353); err != nil {
		t.Fatalf("SetDNSPort create: %v", err)
	}
	port, err := ReadDNSPort(dir)
	if err != nil || port != 5353 {
		t.Fatalf("port=%d err=%v", port, err)
	}
}

func TestSetDNSPort_Invalid(t *testing.T) {
	dir := t.TempDir()
	if err := SetDNSPort(dir, 0); err == nil {
		t.Fatal("port=0 应失败")
	}
}

func TestReadWebPort(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, "http:\n  port: 3000\n")
	port, err := ReadWebPort(dir)
	if err != nil || port != 3000 {
		t.Fatalf("port=%d err=%v", port, err)
	}
}

func TestReadWebPort_MissingYaml(t *testing.T) {
	dir := t.TempDir()
	port, err := ReadWebPort(dir)
	if err != nil {
		t.Fatalf("缺失 yaml 应返回默认值: %v", err)
	}
	if port != 3000 {
		t.Fatalf("default web port want 3000 got %d", port)
	}
}

func TestReadWebPort_FromAddress(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, "http:\n  address: 0.0.0.0:8080\n")
	port, err := ReadWebPort(dir)
	if err != nil || port != 8080 {
		t.Fatalf("port=%d err=%v", port, err)
	}
}

func TestEnsureBindLocalhost(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, "bind_host: 0.0.0.0\nhttp:\n  address: 0.0.0.0:3000\ndns:\n  port: 53\n  bootstrap_dns:\n    - 8.8.8.8\n")
	if err := EnsureBindLocalhost(dir); err != nil {
		t.Fatalf("EnsureBindLocalhost: %v", err)
	}
	m := readAGHMap(t, dir)
	if m["bind_host"] != "127.0.0.1" {
		t.Fatalf("bind_host=%v, want 127.0.0.1", m["bind_host"])
	}
	httpSec, ok := m["http"].(map[string]any)
	if !ok {
		t.Fatalf("http 段丢失: %#v", m["http"])
	}
	if httpSec["address"] != "127.0.0.1:3000" {
		t.Fatalf("http.address=%v, want 127.0.0.1:3000", httpSec["address"])
	}
	// 其它键必须保留
	dns, ok := m["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns 段丢失: %#v", m["dns"])
	}
	if _, ok := dns["bootstrap_dns"]; !ok {
		t.Fatal("dns.bootstrap_dns 被误删")
	}
}

func TestEnsureBindLocalhost_MissingYaml(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureBindLocalhost(dir); err != nil {
		t.Fatalf("缺失 yaml 时应创建: %v", err)
	}
	m := readAGHMap(t, dir)
	if m["bind_host"] != "127.0.0.1" {
		t.Fatalf("bind_host=%v", m["bind_host"])
	}
	httpSec, ok := m["http"].(map[string]any)
	if !ok {
		t.Fatalf("http 段应被创建: %#v", m)
	}
	if httpSec["address"] != "127.0.0.1:3000" {
		t.Fatalf("http.address=%v", httpSec["address"])
	}
}

func TestEnsureBindLocalhost_PreserveCustomPort(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, "http:\n  address: 192.168.1.1:8088\n")
	if err := EnsureBindLocalhost(dir); err != nil {
		t.Fatal(err)
	}
	m := readAGHMap(t, dir)
	httpSec := m["http"].(map[string]any)
	if httpSec["address"] != "127.0.0.1:8088" {
		t.Fatalf("http.address=%v, want preserve port 8088", httpSec["address"])
	}
}

func TestPatchUpstream_Insert(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, `dns:
  port: 1053
  bootstrap_dns:
    - 1.1.1.1
  upstream_dns:
    - 8.8.8.8
    - 8.8.4.4
users:
  - name: admin
`)
	prev, err := PatchUpstreamDNS(dir, []string{"127.0.0.1:1054"})
	if err != nil {
		t.Fatalf("PatchUpstreamDNS: %v", err)
	}
	if !reflect.DeepEqual(prev, []string{"8.8.8.8", "8.8.4.4"}) {
		t.Fatalf("previous=%v", prev)
	}

	m := readAGHMap(t, dir)
	// 无关顶层键保留
	if _, ok := m["users"]; !ok {
		t.Fatal("users 键被丢弃")
	}
	dns, ok := m["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns=%#v", m["dns"])
	}
	if _, ok := dns["bootstrap_dns"]; !ok {
		t.Fatal("dns.bootstrap_dns 被误改/删除")
	}
	up := asStringSlice(t, dns["upstream_dns"])
	if !reflect.DeepEqual(up, []string{"127.0.0.1:1054"}) {
		t.Fatalf("upstream_dns=%v", up)
	}
	// port 仍在
	if p, ok := toInt(dns["port"]); !ok || p != 1053 {
		t.Fatalf("dns.port 被破坏: %#v", dns["port"])
	}
}

func TestPatchUpstream_NoPriorUpstream(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, "dns:\n  port: 1053\n")
	prev, err := PatchUpstreamDNS(dir, []string{"127.0.0.1:1054"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(prev) != 0 {
		t.Fatalf("previous 应为 empty, got %v", prev)
	}
	m := readAGHMap(t, dir)
	dns := m["dns"].(map[string]any)
	up := asStringSlice(t, dns["upstream_dns"])
	if !reflect.DeepEqual(up, []string{"127.0.0.1:1054"}) {
		t.Fatalf("upstream=%v", up)
	}
}

func TestRestoreUpstreamDNS(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, `dns:
  port: 1053
  upstream_dns:
    - 8.8.8.8
`)
	prev, err := PatchUpstreamDNS(dir, []string{"127.0.0.1:1054"})
	if err != nil {
		t.Fatal(err)
	}
	if err := RestoreUpstreamDNS(dir, prev); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	m := readAGHMap(t, dir)
	dns := m["dns"].(map[string]any)
	up := asStringSlice(t, dns["upstream_dns"])
	if !reflect.DeepEqual(up, []string{"8.8.8.8"}) {
		t.Fatalf("restored upstream=%v", up)
	}
}

func TestPatchUpstream_MissingYaml(t *testing.T) {
	dir := t.TempDir()
	prev, err := PatchUpstreamDNS(dir, []string{"127.0.0.1:1054"})
	if err != nil {
		t.Fatalf("缺失 yaml 时应创建最小配置: %v", err)
	}
	if len(prev) != 0 {
		t.Fatalf("previous=%v", prev)
	}
	m := readAGHMap(t, dir)
	dns, ok := m["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns missing: %#v", m)
	}
	up := asStringSlice(t, dns["upstream_dns"])
	if !reflect.DeepEqual(up, []string{"127.0.0.1:1054"}) {
		t.Fatalf("upstream=%v", up)
	}
}

func asStringSlice(t *testing.T, v any) []string {
	t.Helper()
	if v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			out = append(out, item.(string))
		}
		return out
	default:
		t.Fatalf("not a string slice: %T %#v", v, v)
		return nil
	}
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func TestSetWebPort(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, "bind_host: 0.0.0.0\nhttp:\n  address: 0.0.0.0:3000\n")
	if err := SetWebPort(dir, 3456); err != nil {
		t.Fatalf("SetWebPort: %v", err)
	}
	m := readAGHMap(t, dir)
	if m["bind_host"] != "127.0.0.1" {
		t.Fatalf("bind_host=%v", m["bind_host"])
	}
	httpSec, ok := m["http"].(map[string]any)
	if !ok {
		t.Fatal("http section missing")
	}
	if httpSec["address"] != "127.0.0.1:3456" {
		t.Fatalf("http.address=%v, want 127.0.0.1:3456", httpSec["address"])
	}
	port, err := ReadWebPort(dir)
	if err != nil || port != 3456 {
		t.Fatalf("ReadWebPort=%d err=%v", port, err)
	}
}

func TestSetWebPort_Invalid(t *testing.T) {
	dir := t.TempDir()
	if err := SetWebPort(dir, 0); err == nil {
		t.Fatal("port=0 应失败")
	}
	if err := SetWebPort(dir, 70000); err == nil {
		t.Fatal("port=70000 应失败")
	}
}

func TestPatchDNSResolvers(t *testing.T) {
	dir := t.TempDir()
	writeAGHYaml(t, dir, "dns:\n  port: 5353\n  upstream_dns:\n    - 1.1.1.1\n")
	if err := PatchDNSResolvers(dir,
		[]string{"127.0.0.1:1053"},
		[]string{"127.0.0.1:1053"},
		[]string{"223.5.5.5", "119.29.29.29"},
	); err != nil {
		t.Fatalf("PatchDNSResolvers: %v", err)
	}
	m, _, err := loadConfigMap(dir)
	if err != nil {
		t.Fatal(err)
	}
	dns := asMap(m["dns"])
	if got := asStringList(dns["upstream_dns"]); len(got) != 1 || got[0] != "127.0.0.1:1053" {
		t.Fatalf("upstream=%v", got)
	}
	if got := asStringList(dns["fallback_dns"]); len(got) != 1 || got[0] != "127.0.0.1:1053" {
		t.Fatalf("fallback=%v", got)
	}
	if got := asStringList(dns["bootstrap_dns"]); len(got) != 2 {
		t.Fatalf("bootstrap=%v", got)
	}
	// nil 字段不改
	if err := PatchDNSResolvers(dir, []string{"8.8.8.8"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	m, _, _ = loadConfigMap(dir)
	dns = asMap(m["dns"])
	if got := asStringList(dns["upstream_dns"]); len(got) != 1 || got[0] != "8.8.8.8" {
		t.Fatalf("upstream after partial=%v", got)
	}
	if got := asStringList(dns["fallback_dns"]); len(got) != 1 || got[0] != "127.0.0.1:1053" {
		t.Fatalf("fallback should keep: %v", got)
	}
}
