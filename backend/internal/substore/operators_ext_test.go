package substore

import "testing"

// Sub-Store: Sort —— 按名称排序
func TestApplySort(t *testing.T) {
	nodes := []Node{
		{Name: "US-03", Server: "3.3.3.3", Port: 443},
		{Name: "HK-01", Server: "1.1.1.1", Port: 443},
		{Name: "JP-02", Server: "2.2.2.2", Port: 443},
	}

	asc, err := ApplyPipeline(nodes, []PipelineOperator{
		{Type: OpSort, Enabled: true, Payload: map[string]interface{}{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if asc[0].Name != "HK-01" || asc[2].Name != "US-03" {
		t.Fatalf("升序排序失败: %v", names(asc))
	}

	desc, err := ApplyPipeline(nodes, []PipelineOperator{
		{Type: OpSort, Enabled: true, Payload: map[string]interface{}{"order": "desc"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if desc[0].Name != "US-03" {
		t.Fatalf("降序排序失败: %v", names(desc))
	}
}

// Sub-Store: Regex Sort —— 按关键词优先级排序
func TestApplyRegexSort(t *testing.T) {
	nodes := []Node{
		{Name: "US-01", Server: "3.3.3.3", Port: 443},
		{Name: "JP-01", Server: "2.2.2.2", Port: 443},
		{Name: "HK-01", Server: "1.1.1.1", Port: 443},
	}
	out, err := ApplyPipeline(nodes, []PipelineOperator{{
		Type:    OpRegexSort,
		Enabled: true,
		Payload: map[string]interface{}{
			"patterns": []interface{}{"HK", "JP"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Name != "HK-01" || out[1].Name != "JP-01" || out[2].Name != "US-01" {
		t.Fatalf("关键词排序失败: %v", names(out))
	}
}

// Sub-Store: Regex Delete —— 删除名称中的匹配片段
func TestApplyRegexDelete(t *testing.T) {
	nodes := []Node{
		{Name: "【测试】HK-01", Server: "1.1.1.1", Port: 443},
	}
	out, err := ApplyPipeline(nodes, []PipelineOperator{{
		Type:    OpRegexDelete,
		Enabled: true,
		Payload: map[string]interface{}{"pattern": `【[^】]*】`},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Name != "HK-01" {
		t.Fatalf("Regex Delete 失败: %q", out[0].Name)
	}
	if len(out) != 1 {
		t.Fatal("Regex Delete 不应删除节点本身")
	}
}

// Sub-Store: Useless Proxies —— 剔除机场信息类假节点
func TestApplyUselessFilter(t *testing.T) {
	nodes := []Node{
		{Name: "HK-01", Server: "1.1.1.1", Port: 443},
		{Name: "剩余流量：100GB", Server: "0.0.0.0", Port: 1},
		{Name: "官网 example.com", Server: "0.0.0.0", Port: 1},
		{Name: "套餐到期：2026-01-01", Server: "0.0.0.0", Port: 1},
		{Name: "NoServer", Server: "", Port: 0},
	}
	out, err := ApplyPipeline(nodes, []PipelineOperator{
		{Type: OpUseless, Enabled: true, Payload: map[string]interface{}{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "HK-01" {
		t.Fatalf("无效节点过滤失败，剩余: %v", names(out))
	}
}

// Resolve Domain：已是 IP 的节点应保持不变（不依赖外部 DNS）
func TestApplyResolveDomainSkipsIP(t *testing.T) {
	nodes := []Node{
		{Name: "HK-01", Server: "1.1.1.1", Port: 443},
	}
	out, err := ApplyPipeline(nodes, []PipelineOperator{
		{Type: OpResolve, Enabled: true, Payload: map[string]interface{}{"timeout": float64(1)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Server != "1.1.1.1" {
		t.Fatalf("IP 不应被改写: %q", out[0].Server)
	}
	if _, ok := out[0].Extra["_origin_server"]; ok {
		t.Fatal("IP 节点不应记录 _origin_server")
	}
}

func names(ns []Node) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.Name)
	}
	return out
}
