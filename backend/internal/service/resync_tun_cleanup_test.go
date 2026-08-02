package service

import (
	"context"
	"testing"

	"auroramihomo/backend/internal/netcheck"
)

// 配置中心把 tun.enable 打开后，Resync 必须拆掉仍托管的 aurora_tproxy。
func TestResyncCleansManagedTProxyWhenConfigIsTUN(t *testing.T) {
	s, store, app, _ := newSvcWithBase(t, reportWith("linux", netcheck.ModeTUN, netcheck.ModeTProxy),
		"tun:\n  enable: true\n  stack: mixed\n")
	seedManagedTProxy(t, store)
	app.rulesActive = true
	if err := s.Resync(context.Background()); err != nil {
		t.Fatalf("Resync 失败: %v", err)
	}
	if _, torn, _ := app.counts(); torn < 1 {
		t.Fatal("配置已是 TUN 时 Resync 应 Teardown 残留 aurora_tproxy")
	}
	if store.kv[settingTProxyManaged] == "1" {
		t.Fatal("应清除 TProxy 托管标记")
	}
}
