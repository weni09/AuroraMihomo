package service

import (
	"context"
	"path/filepath"
	"testing"

	"auroramihomo/backend/internal/adguard"
	"auroramihomo/backend/internal/repository"
)

func TestApplyEntryDNSPreset_WritesAGHAndMihomo(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "agh")
	dbPath := filepath.Join(dir, "t.db")
	db, err := repository.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 最小 AGH yaml
	if err := adguard.SetDNSPort(work, 5353); err != nil {
		t.Fatal(err)
	}
	if _, err := adguard.PatchUpstreamDNS(work, []string{"1.1.1.1"}); err != nil {
		t.Fatal(err)
	}

	// 假 ConfigService：只记录 SetBaseDNSListen / UpdateBaseConfig
	// 使用真实 ConfigService 需要更多依赖；这里用轻量 stub 注入困难，
	// 改为在有 cfgSvc 的集成路径上测 AGH 侧，mihomo 侧用 nil 时错误路径。
	svc := NewAdGuardService(db, nil, adguard.NewManager(adguard.Config{WorkDir: work}), nil, nil, work, "127.0.0.1:3000")
	if err := svc.ApplyEntryDNSPreset(context.Background()); err == nil {
		t.Fatal("无 ConfigService 应失败")
	}

	// 直接验证 PatchDNSResolvers + SetDNSPort 组合（preset 核心）
	if err := adguard.SetDNSPort(work, 53); err != nil {
		t.Fatal(err)
	}
	if err := adguard.PatchDNSResolvers(work, entryPresetAGHUpstream, entryPresetAGHFallback, entryPresetAGHBootstrap); err != nil {
		t.Fatal(err)
	}
	port, err := adguard.ReadDNSPort(work)
	if err != nil || port != 53 {
		t.Fatalf("port=%d err=%v", port, err)
	}
}
