package substore

import (
	"strings"
	"testing"
)

func mkNodes() []Node {
	return []Node{
		{Name: "🇭🇰 香港 01", Type: "ss", Server: "hk1.example.com", Port: 443,
			Extra: map[string]interface{}{"cipher": "aes-256-gcm", "password": "p"}},
		{Name: "US Los Angeles 02", Type: "vmess", Server: "1.2.3.4", Port: 80,
			Extra: map[string]interface{}{"uuid": "u"}},
		{Name: "过期勿使用", Type: "ss", Server: "expire.example.com", Port: 8388,
			Extra: map[string]interface{}{"cipher": "aes-128-gcm", "password": "q"}},
		{Name: "🇯🇵 日本 03", Type: "trojan", Server: "jp.example.com", Port: 443,
			Extra: map[string]interface{}{"password": "r"}},
	}
}

func op(typ OperatorType, payload map[string]interface{}) PipelineOperator {
	return PipelineOperator{Type: typ, Enabled: true, Payload: payload}
}

// 逐个执行每种管道算子，确认它们真的产生了效果，
// 而不是空壳实现（原样返回输入节点）。
func TestAllOperatorsHaveRealEffect(t *testing.T) {
	cases := []struct {
		name   string
		ops    []PipelineOperator
		verify func(t *testing.T, in, out []Node)
	}{
		{
			name: "rename 重命名",
			ops:  []PipelineOperator{op(OpRename, map[string]interface{}{"pattern": "香港", "replace": "HK"})},
			verify: func(t *testing.T, in, out []Node) {
				for _, n := range out {
					if strings.Contains(n.Name, "香港") {
						t.Errorf("rename 未生效，仍含「香港」: %q", n.Name)
					}
				}
				if !strings.Contains(out[0].Name, "HK") {
					t.Errorf("rename 未替换为 HK，实际 %q", out[0].Name)
				}
			},
		},
		{
			name: "filter 关键字保留",
			ops:  []PipelineOperator{op(OpFilter, map[string]interface{}{"action": "keep", "pattern": "香港"})},
			verify: func(t *testing.T, in, out []Node) {
				if len(out) >= len(in) {
					t.Errorf("filter 未过滤，输入 %d 输出 %d", len(in), len(out))
				}
				for _, n := range out {
					if !strings.Contains(n.Name, "香港") {
						t.Errorf("keep 过滤后不该留下 %q", n.Name)
					}
				}
			},
		},
		{
			name: "flag 补国旗",
			ops:  []PipelineOperator{op(OpFlag, map[string]interface{}{})},
			verify: func(t *testing.T, in, out []Node) {
				var got string
				for _, n := range out {
					if strings.Contains(n.Name, "Los Angeles") {
						got = n.Name
					}
				}
				if got == "" {
					t.Skip("未找到目标节点，跳过")
				}
				if got == in[1].Name {
					t.Errorf("flag 未生效，名称未变化: %q", got)
				}
			},
		},
		{
			name: "set_property 设置字段",
			ops:  []PipelineOperator{op(OpSetProperty, map[string]interface{}{"udp": true})},
			verify: func(t *testing.T, in, out []Node) {
				// udp 是 Node 的结构体字段，不落在 Extra 里
				for _, n := range out {
					if !n.UDP {
						t.Errorf("set_property 未把 %q 的 udp 设为 true", n.Name)
					}
				}
			},
		},
		{
			name: "set_property 自定义字段落 Extra",
			ops:  []PipelineOperator{op(OpSetProperty, map[string]interface{}{"skip-cert-verify": true})},
			verify: func(t *testing.T, in, out []Node) {
				for _, n := range out {
					if n.Extra["skip-cert-verify"] != true {
						t.Errorf("未建模的属性应落入 Extra，节点 %q 实际 %v", n.Name, n.Extra["skip-cert-verify"])
					}
				}
			},
		},
		{
			name: "script 脚本改名",
			ops: []PipelineOperator{op(OpScript, map[string]interface{}{
				"script": "function operator(nodes){ return nodes.map(n => ({...n, name: 'S-' + n.name})) }"})},
			verify: func(t *testing.T, in, out []Node) {
				if len(out) == 0 {
					t.Fatal("script 执行后节点为空")
				}
				for _, n := range out {
					if !strings.HasPrefix(n.Name, "S-") {
						t.Errorf("script 未生效，节点名 %q 缺前缀", n.Name)
					}
				}
			},
		},
		{
			name: "sort 名称排序",
			ops:  []PipelineOperator{op(OpSort, map[string]interface{}{"order": "asc"})},
			verify: func(t *testing.T, in, out []Node) {
				if len(out) != len(in) {
					t.Fatalf("sort 不应改变节点数，输入 %d 输出 %d", len(in), len(out))
				}
				for i := 1; i < len(out); i++ {
					if out[i-1].Name > out[i].Name {
						t.Errorf("升序未生效: %q 排在 %q 之前", out[i-1].Name, out[i].Name)
					}
				}
			},
		},
		{
			name: "regex_sort 正则优先级排序",
			ops:  []PipelineOperator{op(OpRegexSort, map[string]interface{}{"patterns": []interface{}{"日本", "香港"}})},
			verify: func(t *testing.T, in, out []Node) {
				if len(out) == 0 {
					t.Fatal("排序后节点为空")
				}
				if !strings.Contains(out[0].Name, "日本") {
					t.Errorf("regex_sort 未把「日本」排到最前，实际首位 %q", out[0].Name)
				}
			},
		},
		{
			name: "regex_delete 删除匹配片段",
			ops:  []PipelineOperator{op(OpRegexDelete, map[string]interface{}{"pattern": `\d+`})},
			verify: func(t *testing.T, in, out []Node) {
				for _, n := range out {
					if strings.ContainsAny(n.Name, "0123456789") {
						t.Errorf("regex_delete 未删除数字，实际 %q", n.Name)
					}
				}
			},
		},
		{
			name: "useless 移除无效节点",
			ops:  []PipelineOperator{op(OpUseless, map[string]interface{}{})},
			verify: func(t *testing.T, in, out []Node) {
				for _, n := range out {
					if strings.Contains(n.Name, "过期") {
						t.Errorf("useless 未移除过期节点 %q", n.Name)
					}
				}
				if len(out) >= len(in) {
					t.Errorf("useless 应过滤掉无效节点，输入 %d 输出 %d", len(in), len(out))
				}
			},
		},
		{
			name: "region 按地区筛选",
			ops:  []PipelineOperator{op(OpRegion, map[string]interface{}{"action": "keep", "regions": []interface{}{"HK"}})},
			verify: func(t *testing.T, in, out []Node) {
				if len(out) == 0 {
					t.Fatal("region 筛选 HK 后不该为空")
				}
				if len(out) >= len(in) {
					t.Errorf("region 应只保留 HK 节点，输入 %d 输出 %d", len(in), len(out))
				}
			},
		},
		{
			name: "resolve_domain 域名解析",
			ops:  []PipelineOperator{op(OpResolve, map[string]interface{}{})},
			verify: func(t *testing.T, in, out []Node) {
				// 测试环境无法保证 DNS 可用，这里只验证算子不会吞掉节点
				if len(out) != len(in) {
					t.Errorf("resolve_domain 不应改变节点数量，输入 %d 输出 %d", len(in), len(out))
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := mkNodes()
			out, err := ApplyPipeline(mkNodes(), c.ops)
			if err != nil {
				t.Fatalf("执行失败: %v", err)
			}
			c.verify(t, in, out)
		})
	}
}

// 节点被过滤光时不能生成成员为空的策略组：
// mihomo 对 proxies 与 use 同时为空的组会报 "'use' or 'proxies' missing" 并拒绝加载。
func TestEmptyNodesDoesNotEmitEmptyGroup(t *testing.T) {
	out, err := NodesToMihomoYAML(nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "proxy-groups") {
		t.Errorf("无节点时不应生成策略组，实际产物:\n%s", out)
	}
	if strings.Contains(out, "MATCH,Proxy") {
		t.Errorf("无节点时不应生成指向空组的规则，实际产物:\n%s", out)
	}

	// 有节点时仍应正常生成
	out2, err := NodesToMihomoYAML([]Node{
		{Name: "N1", Type: "ss", Server: "a.com", Port: 1,
			Extra: map[string]interface{}{"cipher": "aes-256-gcm", "password": "p"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "proxy-groups") || !strings.Contains(out2, "MATCH,Proxy") {
		t.Errorf("有节点时应正常生成策略组与规则，实际:\n%s", out2)
	}
}
