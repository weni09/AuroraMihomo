package service

import (
	"context"
	"testing"

	"auroramihomo/backend/internal/model"
)

const unmergedBaseYAML = `
mode: rule
external-controller: 127.0.0.1:9090
proxies:
  - name: "HK01"
    type: ss
    server: a.com
    port: 443
`

// 全新安装（从未合并过）不应提示未应用：还没有「已生效配置」的概念。
func TestBaseUnmergedFalseWhenNeverMerged(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	if err := svc.UpdateBaseConfig(unmergedBaseYAML); err != nil {
		t.Fatalf("写入 base 配置失败: %v", err)
	}

	unmerged, err := svc.BaseUnmerged()
	if err != nil {
		t.Fatalf("查询未合并状态失败: %v", err)
	}
	if unmerged {
		t.Fatal("从未合并过不应提示未应用")
	}
}

// 合并成功后指纹对齐；再次保存 base 而未合并时哈希失配，提示恢复；
// 再合并一次提示消除。
func TestBaseUnmergedTracksSaveAndMerge(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	ctx := context.Background()

	if err := svc.UpdateBaseConfig(unmergedBaseYAML); err != nil {
		t.Fatalf("写入 base 配置失败: %v", err)
	}
	if _, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0)); err != nil {
		t.Fatalf("合并失败: %v", err)
	}

	if unmerged, err := svc.BaseUnmerged(); err != nil || unmerged {
		t.Fatalf("合并后应已对齐（unmerged=%v, err=%v）", unmerged, err)
	}

	// 保存 base 但不再合并：哈希与指纹失配，应提示未应用
	if err := svc.UpdateBaseConfig(unmergedBaseYAML + "log-level: debug\n"); err != nil {
		t.Fatalf("更新 base 配置失败: %v", err)
	}
	if unmerged, err := svc.BaseUnmerged(); err != nil || !unmerged {
		t.Fatalf("保存后未合并应提示（unmerged=%v, err=%v）", unmerged, err)
	}

	// 再合并一次，提示消除
	if _, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0)); err != nil {
		t.Fatalf("再次合并失败: %v", err)
	}
	if unmerged, err := svc.BaseUnmerged(); err != nil || unmerged {
		t.Fatalf("再次合并后应已对齐（unmerged=%v, err=%v）", unmerged, err)
	}
}

// 保存内容与已合并内容完全一致时不应提示（指纹相同，无需重新合并）。
func TestBaseUnmergedFalseWhenSavedContentUnchanged(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	ctx := context.Background()

	if err := svc.UpdateBaseConfig(unmergedBaseYAML); err != nil {
		t.Fatalf("写入 base 配置失败: %v", err)
	}
	if _, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0)); err != nil {
		t.Fatalf("合并失败: %v", err)
	}

	// 原样再保存一次：内容没变，不算「未合并」
	if err := svc.UpdateBaseConfig(unmergedBaseYAML); err != nil {
		t.Fatalf("重复保存 base 配置失败: %v", err)
	}
	if unmerged, err := svc.BaseUnmerged(); err != nil || unmerged {
		t.Fatalf("内容未变的保存不应提示（unmerged=%v, err=%v）", unmerged, err)
	}
}

// 升级场景：merged 记录存在但指纹从未写过（旧版本升级而来），
// 无法证明 base 与已生效配置对齐，应保守提示一次。
func TestBaseUnmergedTrueWhenFingerprintMissing(t *testing.T) {
	svc, db, _ := newTestConfigService(t)
	ctx := context.Background()

	if err := svc.UpdateBaseConfig(unmergedBaseYAML); err != nil {
		t.Fatalf("写入 base 配置失败: %v", err)
	}
	if _, err := svc.MergeAndApplyDetailed(ctx, MergeWithRefresh(0)); err != nil {
		t.Fatalf("合并失败: %v", err)
	}

	// 模拟旧版本数据：合并过但指纹记录不存在
	if err := db.DB.Where("key = ?", mergedBaseFingerprintKey).Delete(&model.Setting{}).Error; err != nil {
		t.Fatalf("删除指纹失败: %v", err)
	}

	unmerged, err := svc.BaseUnmerged()
	if err != nil {
		t.Fatalf("查询未合并状态失败: %v", err)
	}
	if !unmerged {
		t.Fatal("指纹缺失且 merged 已存在时应保守提示未应用")
	}
}
