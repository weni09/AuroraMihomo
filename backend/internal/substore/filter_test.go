package substore

import "testing"

// 回归测试：确保 OpFilter 真的被 ApplyPipeline 分发执行
func TestApplyPipelineFilterKeep(t *testing.T) {
	nodes := []Node{
		{Name: "HK-01", Type: "ss", Server: "1.1.1.1", Port: 443},
		{Name: "JP-02", Type: "ss", Server: "2.2.2.2", Port: 443},
		{Name: "US-03", Type: "ss", Server: "3.3.3.3", Port: 443},
	}
	ops := []PipelineOperator{{
		Type:    OpFilter,
		Enabled: true,
		Payload: map[string]interface{}{"action": "keep", "pattern": "HK|JP"},
	}}
	out, err := ApplyPipeline(nodes, ops)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("keep 过滤失败: 期望 2 个节点, 实际 %d", len(out))
	}
	for _, n := range out {
		if n.Name == "US-03" {
			t.Fatal("US-03 应被过滤掉")
		}
	}
}

func TestApplyPipelineFilterDrop(t *testing.T) {
	nodes := []Node{
		{Name: "HK-01", Type: "ss", Server: "1.1.1.1", Port: 443},
		{Name: "US-03", Type: "ss", Server: "3.3.3.3", Port: 443},
	}
	ops := []PipelineOperator{{
		Type:    OpFilter,
		Enabled: true,
		Payload: map[string]interface{}{"action": "drop", "pattern": "US"},
	}}
	out, err := ApplyPipeline(nodes, ops)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "HK-01" {
		t.Fatalf("drop 过滤失败: %+v", out)
	}
}
