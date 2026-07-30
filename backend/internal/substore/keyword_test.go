package substore

import (
	"strings"
	"testing"
	"time"
)

// 两字母短码必须按词边界匹配，否则 Russia→US、Singapore→IN 这类误伤会大面积发生
func TestMatchKeywordWordBoundary(t *testing.T) {
	cases := []struct {
		name    string
		keyword string
		want    bool
	}{
		{"russia-01", "us", false},
		{"bonus节点", "us", false},
		{"house-plus", "us", false},
		{"us-la-01", "us", true},
		{"us_la", "us", true},
		{"[us] los angeles", "us", true},
		{"singapore-01", "in", false},
		{"beijing-hk", "in", false},
		{"in-mumbai", "in", true},
		{"美国 洛杉矶", "美国", true},
		{"🇺🇸 LA", "🇺🇸", true},
	}
	for _, c := range cases {
		got := matchKeyword(c.name, c.keyword)
		if got != c.want {
			t.Errorf("matchKeyword(%q, %q) = %v, 期望 %v", c.name, c.keyword, got, c.want)
		}
	}
}

// region 过滤器不得把 Russia / Singapore 误判
func TestRegionNoFalsePositive(t *testing.T) {
	nodes := []Node{
		{Name: "Russia-Moscow-01", Type: "ss"},
		{Name: "Singapore-01", Type: "ss"},
		{Name: "US-LA-01", Type: "ss"},
	}
	kept, err := applyRegionFilter(nodes, map[string]interface{}{
		"action": "keep", "regions": []interface{}{"US"},
	})
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if len(kept) != 1 || kept[0].Name != "US-LA-01" {
		t.Fatalf("US 过滤误伤，实际保留: %+v", kept)
	}
}

// applyFlag 必须是确定性的：同样输入多次执行结果一致
func TestApplyFlagDeterministic(t *testing.T) {
	build := func() []Node {
		return []Node{
			{Name: "Russia-Moscow", Type: "ss"},
			{Name: "香港-01", Type: "ss"},
			{Name: "JP-Tokyo", Type: "ss"},
		}
	}
	first, err := applyFlag(build())
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	for i := 0; i < 30; i++ {
		got, _ := applyFlag(build())
		for j := range got {
			if got[j].Name != first[j].Name {
				t.Fatalf("applyFlag 结果不稳定: 第 %d 次 %q != %q", i, got[j].Name, first[j].Name)
			}
		}
	}
	if first[0].Name != "🇷🇺 Russia-Moscow" {
		t.Fatalf("Russia 应识别为俄罗斯而非美国，实际 %q", first[0].Name)
	}
	if first[1].Name != "🇭🇰 香港-01" || first[2].Name != "🇯🇵 JP-Tokyo" {
		t.Fatalf("国旗标注错误: %q / %q", first[1].Name, first[2].Name)
	}
}

// 去重应按 server:port 而非名称，避免多机场同名节点被静默删除
func TestDedupeByEndpointNotName(t *testing.T) {
	nodes := []Node{
		{Name: "香港 01", Type: "ss", Server: "1.1.1.1", Port: 443},
		{Name: "香港 01", Type: "ss", Server: "2.2.2.2", Port: 443}, // 另一机场的同名节点
		{Name: "香港 01", Type: "ss", Server: "1.1.1.1", Port: 443}, // 真重复
	}
	out := dedupeNodes(nodes)
	if len(out) != 2 {
		t.Fatalf("期望保留 2 个节点，实际 %d: %+v", len(out), out)
	}
	if out[0].Name == out[1].Name {
		t.Fatalf("重名节点未加序号区分: %q / %q", out[0].Name, out[1].Name)
	}
	if out[0].Server != "1.1.1.1" || out[1].Server != "2.2.2.2" {
		t.Fatalf("去重保留了错误的节点: %+v", out)
	}
}

// matchRegion 应对大小写不敏感
func TestMatchRegionCaseInsensitive(t *testing.T) {
	for _, name := range []string{"US-LA-01", "us-la-01", "Us-La-01"} {
		if !matchRegion(name, "US") {
			t.Fatalf("%q 未能识别为 US", name)
		}
	}
	if matchRegion("Russia-01", "US") {
		t.Fatal("Russia 被误判为 US")
	}
}

// 死循环脚本必须被中断，否则公开分享端点可被用来打满 CPU
func TestScriptTimeoutInterrupts(t *testing.T) {
	nodes := []Node{{Name: "n1", Type: "ss", Server: "1.1.1.1", Port: 80}}
	done := make(chan error, 1)
	go func() {
		_, err := ApplyPipeline(nodes, []PipelineOperator{{
			Type:    OpScript,
			Enabled: true,
			Payload: map[string]interface{}{
				"script": "function operator(proxies){ while(true){} return proxies; }",
			},
		}})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("死循环脚本应返回超时错误")
		}
		if !strings.Contains(err.Error(), "中断") && !strings.Contains(err.Error(), "超时") {
			t.Fatalf("错误信息未体现超时中断: %v", err)
		}
	case <-time.After(scriptTimeout + 10*time.Second):
		t.Fatal("死循环脚本未被中断，协程仍在运行")
	}
}

// 正常脚本不受超时影响
func TestScriptNormalStillWorks(t *testing.T) {
	nodes := []Node{{Name: "a", Type: "ss", Server: "1.1.1.1", Port: 80}}
	out, err := ApplyPipeline(nodes, []PipelineOperator{{
		Type:    OpScript,
		Enabled: true,
		Payload: map[string]interface{}{
			"script": "function operator(proxies){ proxies[0].name = 'renamed'; return proxies; }",
		},
	}})
	if err != nil {
		t.Fatalf("正常脚本执行失败: %v", err)
	}
	if len(out) != 1 || out[0].Name != "renamed" {
		t.Fatalf("脚本未生效: %+v", out)
	}
}
