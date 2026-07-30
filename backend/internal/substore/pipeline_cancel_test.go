package substore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// 管道必须响应取消。
//
// resolve_domain 会逐个节点发 DNS 请求，节点多时累计耗时可观，
// 而它原先用 context.Background() 自建超时，取消链在此断掉：
// 外层合并被取消（进程关停、请求超时）后这一步仍会跑完，
// 落在关停等待窗口之外，于是数据库已关它还在跑。

func TestApplyPipelineCtxStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 进入管道前就已取消

	nodes := []Node{{Name: "A", Type: "vmess", Server: "example.com", Port: 80}}
	ops := []PipelineOperator{
		{Type: OpResolve, Enabled: true, Payload: map[string]interface{}{}},
	}

	_, err := ApplyPipelineCtx(ctx, nodes, ops)
	if err == nil {
		t.Fatal("已取消的 ctx 应让管道返回错误")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("错误应可 errors.Is 到 context.Canceled，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "取消") {
		t.Errorf("错误信息应说明是取消，实际 %q", err.Error())
	}
}

// 取消发生在多个算子之间时应尽早停下，不把剩余算子跑完。
func TestApplyPipelineCtxStopsBetweenOperators(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	nodes := []Node{
		{Name: "保留A", Type: "vmess", Server: "a.com", Port: 80},
		{Name: "保留B", Type: "vmess", Server: "b.com", Port: 80},
	}
	// 第一个算子会把名字改掉；若取消生效，第二个算子不该执行
	ops := []PipelineOperator{
		{Type: OpRename, Enabled: true, Payload: map[string]interface{}{
			"from": "保留", "to": "改过",
		}},
		{Type: OpRegexDelete, Enabled: true, Payload: map[string]interface{}{
			"pattern": "改过",
		}},
	}

	// 立即取消，让第一个算子前的检查就命中
	cancel()
	out, err := ApplyPipelineCtx(ctx, nodes, ops)
	if err == nil {
		t.Fatal("应返回取消错误")
	}
	// 取消时返回的是当前的 nodes，名字应还没被任何算子改动
	if out[0].Name != "保留A" {
		t.Errorf("取消后不应执行任何算子，节点名实际为 %q", out[0].Name)
	}
}

// 未取消时行为不变——加了取消检查不能影响正常路径。
func TestApplyPipelineCtxNormalPathUnchanged(t *testing.T) {
	nodes := []Node{
		{Name: "香港A", Type: "vmess", Server: "a.com", Port: 80},
		{Name: "美国B", Type: "vmess", Server: "b.com", Port: 80},
	}
	ops := []PipelineOperator{
		{Type: OpFilter, Enabled: true, Payload: map[string]interface{}{
			"keywords": "香港",
		}},
	}

	withCtx, err := ApplyPipelineCtx(context.Background(), nodes, ops)
	if err != nil {
		t.Fatalf("正常路径不应出错: %v", err)
	}
	// 无 ctx 的旧签名必须产出相同结果（它内部转调 Ctx 版本）
	legacy, err := ApplyPipeline(nodes, ops)
	if err != nil {
		t.Fatalf("兼容签名不应出错: %v", err)
	}
	if len(withCtx) != len(legacy) {
		t.Fatalf("两个签名结果数量不一致: %d vs %d", len(withCtx), len(legacy))
	}
	for i := range withCtx {
		if withCtx[i].Name != legacy[i].Name {
			t.Errorf("第 %d 个节点不一致: %q vs %q", i, withCtx[i].Name, legacy[i].Name)
		}
	}
}

// 已取消时 resolve_domain 应保留原始域名，而不是把 Server 写成空值。
// 解析失败本就"保留域名、不阻断管道"，取消同理。
func TestResolveDomainKeepsHostnameOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	nodes := []Node{{Name: "A", Type: "vmess", Server: "example.com", Port: 80}}
	out, err := applyResolveDomain(ctx, nodes, map[string]interface{}{})
	if err != nil {
		t.Fatalf("取消不应作为错误从算子内部抛出: %v", err)
	}
	if out[0].Server != "example.com" {
		t.Errorf("取消后应保留原域名，实际 %q", out[0].Server)
	}
}

// 空管道与全部禁用的算子不受影响
func TestApplyPipelineCtxEmptyOps(t *testing.T) {
	nodes := []Node{{Name: "A", Type: "vmess", Server: "a.com", Port: 80}}
	out, err := ApplyPipelineCtx(context.Background(), nodes, nil)
	if err != nil || len(out) != 1 {
		t.Errorf("空管道应原样返回，err=%v len=%d", err, len(out))
	}

	out, err = ApplyPipelineCtx(context.Background(), nodes, []PipelineOperator{
		{Type: OpResolve, Enabled: false, Payload: map[string]interface{}{}},
	})
	if err != nil || len(out) != 1 {
		t.Errorf("全部禁用应原样返回，err=%v len=%d", err, len(out))
	}
}
