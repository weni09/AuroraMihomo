package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"auroramihomo/backend/internal/domain"
	"auroramihomo/backend/internal/engine"
)

// 设计 §18：备份目录默认保留 10 份
func TestPruneBackupsKeepsLatest10(t *testing.T) {
	dir := t.TempDir()
	s := &ConfigService{configDir: dir}

	backupDir := s.backupDir()
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 造 15 份备份，修改时间递增
	base := time.Now().Add(-24 * time.Hour)
	for i := 0; i < 15; i++ {
		name := filepath.Join(backupDir, "config-"+time.Now().Format("20060102")+"-"+string(rune('a'+i))+".yaml")
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(name, mt, mt); err != nil {
			t.Fatal(err)
		}
	}

	s.pruneBackups(defaultBackupRetain)

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != defaultBackupRetain {
		t.Fatalf("应保留 %d 份备份，实际 %d 份", defaultBackupRetain, len(entries))
	}
}

// 设计 §3 Layer 3 / §15：已解决冲突应生成 override 配置
func TestBuildOverrideYAML(t *testing.T) {
	s := &ConfigService{engine: engine.NewMergeEngine()}

	resolved := []domain.Conflict{
		{
			Type:       "proxy",
			Resolution: "remote",
			Remote:     map[string]any{"name": "HK01", "type": "ss", "server": "b.com", "port": 443},
		},
		{
			Type:       "rule",
			Resolution: "local",
			Local:      "DOMAIN-SUFFIX,google.com,DIRECT",
		},
	}

	out := s.buildOverrideYAML(resolved)
	if out == "" {
		t.Fatal("override 配置不应为空")
	}
	if !contains(out, "HK01") {
		t.Errorf("override 应包含解决后的 proxy，实际:\n%s", out)
	}
	if !contains(out, "google.com") {
		t.Errorf("override 应包含解决后的 rule，实际:\n%s", out)
	}
}

func TestBuildOverrideYAMLEmptyWhenNoResolved(t *testing.T) {
	s := &ConfigService{engine: engine.NewMergeEngine()}
	if got := s.buildOverrideYAML(nil); got != "" {
		t.Fatalf("无已解决冲突时应返回空，实际 %q", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
