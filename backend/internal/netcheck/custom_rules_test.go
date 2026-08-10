package netcheck

import (
	"context"
	"strings"
	"testing"
)

// ---------- NormalizeCustomRules ----------

func TestNormalizeCustomRulesForms(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "完整命令与裸参数混用，注释与空行忽略",
			in: "# 放行内网回程\n" +
				"iptables -t nat -A PREROUTING -d 10.0.0.0/8 -j RETURN\n" +
				"\n" +
				"-t mangle -A PREROUTING -p udp --dport 53 -j MARK --set-mark 0x2\n" +
				"ip6tables -t nat -A PREROUTING -d 2001:db8::/32 -j RETURN",
			want: []string{
				"iptables -t nat -A PREROUTING -d 10.0.0.0/8 -j RETURN",
				"iptables -t mangle -A PREROUTING -p udp --dport 53 -j MARK --set-mark 0x2",
				"ip6tables -t nat -A PREROUTING -d 2001:db8::/32 -j RETURN",
			},
		},
		{
			name: "空文本",
			in:   "  \n# 只有注释\n",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeCustomRules(tc.in)
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("规则数不符: got %d want %d\n%v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("第 %d 条: got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestNormalizeCustomRulesRejects(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"sudo 前缀", "sudo iptables -A INPUT -j ACCEPT"},
		{"sh 前缀", "sh -c iptables -A INPUT -j ACCEPT"},
		{"既非命令也非参数", "random text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NormalizeCustomRules(tc.in); err == nil {
				t.Fatalf("应拒绝: %q", tc.in)
			}
		})
	}
}

// ---------- toDeleteCommand ----------

func TestToDeleteCommand(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "-A 直接转 -D",
			in:   "iptables -t nat -A PREROUTING -d 10.0.0.0/8 -j RETURN",
			want: "iptables -t nat -D PREROUTING -d 10.0.0.0/8 -j RETURN",
		},
		{
			name: "-I 带位置参数时去掉位置",
			in:   "iptables -t mangle -I PREROUTING 3 -p udp --dport 53 -j MARK --set-mark 0x2",
			want: "iptables -t mangle -D PREROUTING -p udp --dport 53 -j MARK --set-mark 0x2",
		},
		{
			name: "-I 不带位置",
			in:   "iptables -I INPUT -j ACCEPT",
			want: "iptables -D INPUT -j ACCEPT",
		},
		{
			name: "ip6tables 同样处理",
			in:   "ip6tables -A INPUT -d 2001:db8::/32 -j RETURN",
			want: "ip6tables -D INPUT -d 2001:db8::/32 -j RETURN",
		},
		{
			name: "链管理命令无法逆反",
			in:   "iptables -N MY_CHAIN",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toDeleteCommand(tc.in)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// ---------- Apply / Teardown 与自定义规则的交互 ----------

func customParams() TProxyParams {
	p := sampleParams()
	p.CustomRules = []string{
		"iptables -t nat -A PREROUTING -d 10.0.0.0/8 -j RETURN",
		"iptables -t mangle -I PREROUTING 3 -p udp --dport 53 -j MARK --set-mark 0x2",
	}
	return p
}

// 自定义规则必须在内置 nft 规则之后执行（sh -c），且顺序保持书写顺序。
func TestApplyRunsCustomRulesAfterBuiltin(t *testing.T) {
	r := newFakeRunner()
	applier := newApplier(r, t.TempDir())

	if err := applier.Apply(context.Background(), customParams()); err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}

		nftIdx := r.indexOf("nft -f -")
		if nftIdx < 0 {
			t.Fatalf("没有下发 nft 规则:\n%s", r.joined())
		}
		// Apply 前会有一次 sh -c purge 旧 ip rule；自定义规则是之后的 sh -c iptables
		var customIdx []int
		for i, c := range r.calls {
			if strings.HasPrefix(c, "sh -c iptables") {
				customIdx = append(customIdx, i)
			}
		}
		if len(customIdx) != 2 {
			t.Fatalf("应执行 2 条自定义规则，实际 %d:\n%s", len(customIdx), r.joined())
		}
		if customIdx[0] < nftIdx {
			t.Fatalf("自定义规则必须在内置 nft 之后:\n%s", r.joined())
		}
		if !strings.HasPrefix(r.calls[customIdx[0]], "sh -c iptables -t nat -A") {
			t.Errorf("第 1 条自定义规则不对: %s", r.calls[customIdx[0]])
		}
		if !strings.HasPrefix(r.calls[customIdx[1]], "sh -c iptables -t mangle -I") {
			t.Errorf("第 2 条自定义规则不对: %s", r.calls[customIdx[1]])
		}
	}

// 自定义规则任一条失败 → 整体回滚：内置规则拆除 + 已执行的自定义规则
// 逆序 -D 拆除，且错误信息带行号。
func TestApplyRollsBackWhenCustomRuleFails(t *testing.T) {
	r := newFakeRunner()
	// 第 2 条（mangle -I）失败
	r.failOn["sh -c iptables -t mangle"] = "iptables: Permission denied"
	applier := newApplier(r, t.TempDir())

	err := applier.Apply(context.Background(), customParams())
	if err == nil {
		t.Fatal("应报错")
	}
	if !strings.Contains(err.Error(), "第 2 行") {
		t.Errorf("错误应带行号: %v", err)
	}

	joined := r.joined()
	// 已执行的第 1 条被逆序 -D 拆除
	if !strings.Contains(joined, "sh -c iptables -t nat -D PREROUTING -d 10.0.0.0/8 -j RETURN") {
		t.Errorf("应拆除已执行的自定义规则:\n%s", joined)
	}
	// 内置 nft 表被删除
	if !strings.Contains(joined, NFTTeardownCommand()[0]+" delete table inet aurora_tproxy") {
		t.Errorf("应拆除内置规则:\n%s", joined)
	}
}

// Teardown 逆序 -D 拆除自定义规则；"Bad rule"（规则已不在，如宿主重启）
// 视为成功；无法逆反的链管理命令跳过并记日志。
func TestTeardownRemovesCustomRules(t *testing.T) {
	r := newFakeRunner()
	r.failOn["sh -c ip6tables"] = "iptables: Bad rule (does a matching rule exist in that chain?)"
	applier := newApplier(r, t.TempDir())

	rules := []string{
		"iptables -t nat -A PREROUTING -d 10.0.0.0/8 -j RETURN",
		"ip6tables -A INPUT -d 2001:db8::/32 -j RETURN",
		"iptables -N MY_CHAIN",
	}
	if err := applier.Teardown(context.Background(), rules); err != nil {
		t.Fatalf("Teardown 不应失败（Bad rule 应容忍）: %v", err)
	}

	joined := r.joined()
	// 逆序：第 2 条（ip6tables）先拆，第 1 条后拆
	i1 := r.indexOf("sh -c ip6tables -D")
	i2 := r.indexOf("sh -c iptables -t nat -D")
	if i1 < 0 || i2 < 0 {
		t.Fatalf("应逆序 -D 拆除自定义规则:\n%s", joined)
	}
	if i1 > i2 {
		t.Errorf("拆除顺序应逆序（先拆后加的），ip6tables 应先于 iptables:\n%s", joined)
	}
	// -N 没有 -D 调用
	if strings.Contains(joined, "MY_CHAIN") {
		t.Errorf("-N 链管理命令不应出现在拆除调用里:\n%s", joined)
	}
}

// DumpRules：表不存在（TProxy 未开启）返回空串而非报错。
func TestDumpRulesEmptyWhenTableMissing(t *testing.T) {
	r := newFakeRunner()
	r.failOn["nft list table"] = "Error: No such file or directory"
	applier := newApplier(r, t.TempDir())

	out, err := applier.DumpRules(context.Background())
	if err != nil {
		t.Fatalf("表不存在不应报错: %v", err)
	}
	if out != "" {
		t.Fatalf("应返回空串，got %q", out)
	}
}

// 带引号的 comment 拆除时必须保留引号语义，否则 -D 对不上已应用规则。
func TestToDeleteCommandPreservesQuotedComment(t *testing.T) {
	in := `iptables -A INPUT -m comment --comment "hello world" -j ACCEPT`
	got := toDeleteCommand(in)
	want := `iptables -D INPUT -m comment --comment "hello world" -j ACCEPT`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// 二次 Apply 同一批自定义规则：必须先 -D 旧批再 -A，不能叠两条。
func TestApplyRemovesPreviousCustomRulesBeforeReapply(t *testing.T) {
	r := newFakeRunner()
	applier := newApplier(r, t.TempDir())
	p := customParams()

	if err := applier.Apply(context.Background(), p); err != nil {
		t.Fatalf("首次 Apply 失败: %v", err)
	}
	// 模拟服务层落库：第二次 Apply 带 PreviousCustomRules
	p.PreviousCustomRules = append([]string{}, p.CustomRules...)
	r.calls = nil
	if err := applier.Apply(context.Background(), p); err != nil {
		t.Fatalf("二次 Apply 失败: %v", err)
	}

	// 应先出现 -D，再出现 -A
	firstDel, firstAdd := -1, -1
	for i, c := range r.calls {
		if firstDel < 0 && strings.Contains(c, "sh -c") && strings.Contains(c, " -D ") {
			firstDel = i
		}
		if firstAdd < 0 && strings.Contains(c, "sh -c") && strings.Contains(c, " -A ") {
			firstAdd = i
		}
	}
	if firstDel < 0 || firstAdd < 0 {
		t.Fatalf("二次 Apply 应同时包含 -D 与 -A:\n%s", r.joined())
	}
	if firstDel > firstAdd {
		t.Fatalf("应先拆旧批再追加，del@%d add@%d:\n%s", firstDel, firstAdd, r.joined())
	}
}

// 改 A→B 时必须拆掉 A，不能只追加 B。
func TestApplyRemovesOrphanWhenRulesChange(t *testing.T) {
	r := newFakeRunner()
	applier := newApplier(r, t.TempDir())

	oldRule := "iptables -t nat -A PREROUTING -d 10.0.0.0/8 -j RETURN"
	newRule := "iptables -t nat -A PREROUTING -d 192.168.0.0/16 -j RETURN"
	p := sampleParams()
	p.PreviousCustomRules = []string{oldRule}
	p.CustomRules = []string{newRule}

	if err := applier.Apply(context.Background(), p); err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	joined := r.joined()
	if !strings.Contains(joined, "sh -c iptables -t nat -D PREROUTING -d 10.0.0.0/8 -j RETURN") {
		t.Errorf("应拆除旧规则 A:\n%s", joined)
	}
	if !strings.Contains(joined, "sh -c iptables -t nat -A PREROUTING -d 192.168.0.0/16 -j RETURN") {
		t.Errorf("应追加新规则 B:\n%s", joined)
	}
}
