package substore

import "testing"

func TestStripPublicUnsafeOpsRemovesScriptOnly(t *testing.T) {
	ops := []PipelineOperator{
		{Type: OpFilter, Enabled: true},
		{Type: OpScript, Enabled: true, Payload: map[string]interface{}{"script": "return proxies"}},
		{Type: OpResolve, Enabled: true},
		{Type: OpRename, Enabled: true},
	}
	got := StripPublicUnsafeOps(ops)
	if len(got) != 3 {
		t.Fatalf("应保留 3 个非 script 算子，实际 %d", len(got))
	}
	for _, op := range got {
		if op.Type == OpScript {
			t.Fatal("公开路径不应保留 script 算子")
		}
	}
	// 原切片未被原地修改，避免调用方仍持有引用时行为诡异
	if ops[1].Type != OpScript {
		t.Fatal("Strip 不应修改入参切片元素顺序外的原内容语义：原 script 项应仍在")
	}
}

func TestStripPublicUnsafeOpsEmptyNil(t *testing.T) {
	if StripPublicUnsafeOps(nil) != nil {
		t.Fatal("nil 入参应返回 nil")
	}
	if got := StripPublicUnsafeOps(nil); len(got) != 0 {
		t.Fatalf("nil 长度应为 0，实际 %d", len(got))
	}
	empty := []PipelineOperator{}
	if got := StripPublicUnsafeOps(empty); len(got) != 0 {
		t.Fatalf("空切片应仍为空，实际 %d", len(got))
	}
}

func TestStripPublicUnsafeOpsKeepsOrder(t *testing.T) {
	ops := []PipelineOperator{
		{Type: OpFlag, Enabled: true},
		{Type: OpScript, Enabled: true},
		{Type: OpSort, Enabled: true},
	}
	got := StripPublicUnsafeOps(ops)
	if len(got) != 2 || got[0].Type != OpFlag || got[1].Type != OpSort {
		t.Fatalf("应保持相对顺序 flag→sort，实际 %+v", got)
	}
}
