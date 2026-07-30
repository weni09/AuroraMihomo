package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// 合并流程的取消语义有一条明确分界线：writeConfigAtomically。
//
//   - 落盘之前取消：什么都还没改，直接返回，无需回滚
//   - 落盘之后取消：磁盘已是新配置，中止会留下"磁盘新、数据库旧"的不一致
//     （界面读 merged 记录，会一直显示旧内容），因此落盘后不再检查取消
//
// 换言之：取消能避免"白做一场"，但不会制造"做一半"。
// 这组测试固化这条语义——它直接决定关停时会不会损坏用户配置。

func TestMergeCancelledBeforeWriteLeavesFileUntouched(t *testing.T) {
	svc, _, _ := newTestConfigService(t)

	// 先正常合并一次，得到一份基线配置文件
	if _, err := svc.MergeAndApplyDetailed(context.Background(), MergeOptions{}); err != nil {
		t.Fatalf("基线合并失败: %v", err)
	}
	before, err := os.ReadFile(svc.configPath())
	if err != nil {
		t.Fatalf("读取基线配置失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.MergeAndApplyDetailed(ctx, MergeOptions{})
	if err == nil {
		t.Fatal("已取消的 ctx 应让合并返回错误")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("错误应可 errors.Is 到 context.Canceled，实际 %v", err)
	}

	after, err := os.ReadFile(svc.configPath())
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if string(before) != string(after) {
		t.Error("落盘前取消不应改动 config.yaml")
	}
}

// 取消时的错误信息必须说清"配置尚未落盘、无需回滚"，
// 否则运维看到合并失败会去翻备份、担心配置被写坏。
func TestMergeCancelErrorExplainsNoRollbackNeeded(t *testing.T) {
	svc, _, _ := newTestConfigService(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.MergeAndApplyDetailed(ctx, MergeOptions{})
	if err == nil {
		t.Fatal("应返回取消错误")
	}
	if !strings.Contains(err.Error(), "取消") {
		t.Errorf("错误应说明是取消，实际 %q", err.Error())
	}
}

// RestoreVersion 与合并共用同一条分界线
func TestRestoreVersionCancelledBeforeWrite(t *testing.T) {
	svc, db, _ := newTestConfigService(t)

	if _, err := svc.MergeAndApplyDetailed(context.Background(), MergeOptions{}); err != nil {
		t.Fatalf("基线合并失败: %v", err)
	}
	versions, err := db.ListConfigVersions(10)
	if err != nil || len(versions) == 0 {
		t.Fatalf("应至少有一个版本记录，err=%v len=%d", err, len(versions))
	}
	before, err := os.ReadFile(svc.configPath())
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := svc.RestoreVersion(ctx, versions[0].ID); err == nil {
		t.Fatal("已取消的 ctx 应让恢复返回错误")
	}

	after, err := os.ReadFile(svc.configPath())
	if err != nil {
		t.Fatalf("读取配置失败: %v", err)
	}
	if string(before) != string(after) {
		t.Error("落盘前取消不应改动 config.yaml")
	}
}

// 未取消时行为不变：加了取消检查不能影响正常合并
func TestMergeNormalPathUnaffectedByCancelChecks(t *testing.T) {
	svc, _, _ := newTestConfigService(t)

	res, err := svc.MergeAndApplyDetailed(context.Background(), MergeOptions{})
	if err != nil {
		t.Fatalf("正常合并不应出错: %v", err)
	}
	if res == nil {
		t.Fatal("正常合并应返回结果")
	}
	if _, err := os.Stat(svc.configPath()); err != nil {
		t.Errorf("正常合并应产出配置文件: %v", err)
	}
}
