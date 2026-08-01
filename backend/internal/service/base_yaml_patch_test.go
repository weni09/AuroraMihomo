package service

import (
	"strings"
	"testing"

	"auroramihomo/backend/internal/domain"

	"gopkg.in/yaml.v3"
)

// 这份用例是整个改动的核心防线：开关切换写的是用户手写的 base.yaml，
// 丢注释、重排键、凭空写出零值字段都会直接损坏用户资产。
const userBaseYAML = `# 我的机场配置，请勿删除注释
mixed-port: 7890
mode: rule

# DNS 段：公司内网需要走本地解析
dns:
  enable: true
  nameserver:
    - 223.5.5.5

proxies:
  - name: 香港节点
    type: ss
    server: hk.example.com
    cipher: aes-256-gcm

hosts:
  'router.local': 192.168.1.1
`

func TestPatchPreservesCommentsAndUnrelatedKeys(t *testing.T) {
	out, err := patchBaseYAML(userBaseYAML, "tun.enable", true)
	if err != nil {
		t.Fatalf("改写失败: %v", err)
	}

	// 注释是用户资产，往返后必须一字不少
	for _, want := range []string{
		"# 我的机场配置，请勿删除注释",
		"# DNS 段：公司内网需要走本地解析",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("注释丢失: %q\n实际输出:\n%s", want, out)
		}
	}

	// 未涉及的键必须原样保留
	for _, want := range []string{"mixed-port: 7890", "mode: rule", "香港节点", "router.local"} {
		if !strings.Contains(out, want) {
			t.Errorf("无关配置丢失: %q\n实际输出:\n%s", want, out)
		}
	}

	if !strings.Contains(out, "enable: true") {
		t.Errorf("目标键未写入\n实际输出:\n%s", out)
	}
}

// 结构体往返会把非 omitempty 的零值实体化成显式配置，
// 这些用户从未写过的字段会改变 mihomo 行为，必须确认不再出现。
func TestPatchDoesNotMaterializeZeroValues(t *testing.T) {
	out, err := patchBaseYAML(userBaseYAML, "tproxy-port", 7893)
	if err != nil {
		t.Fatalf("改写失败: %v", err)
	}

	for _, unwanted := range []string{"ipv6: false", "port: 0", "allow-lan", "log-level"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("凭空写出了用户从未设置的字段 %q\n实际输出:\n%s", unwanted, out)
		}
	}
	// 用户没写 tun 段时，改 tproxy-port 不该顺手造出一个 tun 段
	if strings.Contains(out, "tun:") {
		t.Errorf("不该凭空创建 tun 段\n实际输出:\n%s", out)
	}
}

func TestPatchCreatesNestedPath(t *testing.T) {
	out, err := patchBaseYAML("mode: rule\n", "tun.enable", true)
	if err != nil {
		t.Fatalf("改写失败: %v", err)
	}
	if !strings.Contains(out, "tun:") || !strings.Contains(out, "enable: true") {
		t.Errorf("嵌套路径未创建\n实际输出:\n%s", out)
	}
	if !strings.Contains(out, "mode: rule") {
		t.Errorf("原有键丢失\n实际输出:\n%s", out)
	}
}

func TestPatchUpdatesExistingValueInPlace(t *testing.T) {
	src := "tun:\n  enable: false\n  stack: system\n"
	out, err := patchBaseYAML(src, "tun.enable", true)
	if err != nil {
		t.Fatalf("改写失败: %v", err)
	}
	if !strings.Contains(out, "enable: true") {
		t.Errorf("值未更新\n实际输出:\n%s", out)
	}
	// 同段内其它键不能被牵连
	if !strings.Contains(out, "stack: system") {
		t.Errorf("同段内其它键被改动\n实际输出:\n%s", out)
	}
}

// 用户在配置项上写的说明不该因为改值而消失
func TestPatchKeepsCommentOnPatchedKey(t *testing.T) {
	src := "tun:\n  # 这台机器必须用 system 栈\n  enable: false\n"
	out, err := patchBaseYAML(src, "tun.enable", true)
	if err != nil {
		t.Fatalf("改写失败: %v", err)
	}
	if !strings.Contains(out, "# 这台机器必须用 system 栈") {
		t.Errorf("被改键上的注释丢失\n实际输出:\n%s", out)
	}
}

func TestPatchDeleteRemovesKeyAndEmptyParent(t *testing.T) {
	src := "mode: rule\ntun:\n  enable: true\n"
	out, err := patchBaseYAML(src, "tun.enable", nil)
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if strings.Contains(out, "enable") {
		t.Errorf("键未被删除\n实际输出:\n%s", out)
	}
	// 删空后不该留下一个空的 tun 段
	if strings.Contains(out, "tun:") {
		t.Errorf("空父级未清理\n实际输出:\n%s", out)
	}
	if !strings.Contains(out, "mode: rule") {
		t.Errorf("无关键被删\n实际输出:\n%s", out)
	}
}

// 删除时若父级还有别的键，不能把父级一起清掉
func TestPatchDeleteKeepsNonEmptyParent(t *testing.T) {
	src := "tun:\n  enable: true\n  mtu: 9000\n"
	out, err := patchBaseYAML(src, "tun.enable", nil)
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if !strings.Contains(out, "mtu: 9000") {
		t.Errorf("父级其它键被误删\n实际输出:\n%s", out)
	}
	if !strings.Contains(out, "tun:") {
		t.Errorf("父级非空却被清理\n实际输出:\n%s", out)
	}
}

func TestPatchEmptySourceProducesMinimalDoc(t *testing.T) {
	out, err := patchBaseYAML("", "tun.enable", true)
	if err != nil {
		t.Fatalf("改写失败: %v", err)
	}
	if !strings.Contains(out, "tun:") || !strings.Contains(out, "enable: true") {
		t.Errorf("空配置未生成目标键\n实际输出:\n%s", out)
	}
}

// 用户把 tun 写成了标量时不能继续往里塞子键，否则会破坏他的数据
func TestPatchRejectsWritingIntoNonMapping(t *testing.T) {
	if _, err := patchBaseYAML("tun: yes\n", "tun.enable", true); err == nil {
		t.Error("应拒绝往非映射节点写子键")
	}
}

func TestPatchMultiAppliesAllAtomically(t *testing.T) {
	out, err := patchBaseYAMLMulti(userBaseYAML, map[string]interface{}{
		"tun.enable":  true,
		"tun.stack":   "mixed",
		"tproxy-port": nil,
	})
	if err != nil {
		t.Fatalf("批量改写失败: %v", err)
	}
	if !strings.Contains(out, "enable: true") || !strings.Contains(out, "stack: mixed") {
		t.Errorf("批量写入未全部生效\n实际输出:\n%s", out)
	}
	if !strings.Contains(out, "# 我的机场配置，请勿删除注释") {
		t.Errorf("批量改写丢了注释\n实际输出:\n%s", out)
	}
}

func TestReadBaseSwitchState(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		wantTUN    bool
		wantStack  string
		wantTProxy int
	}{
		{"空配置", "", false, "", 0},
		{"未配置 tun", "mode: rule\n", false, "", 0},
		{"tun 开启带 stack", "tun:\n  enable: true\n  stack: gvisor\n", true, "gvisor", 0},
		{"tun 显式关闭", "tun:\n  enable: false\n", false, "", 0},
		{"tproxy 端口", "tproxy-port: 7893\n", false, "", 7893},
		{"两者并存以 tun 为先", "tun:\n  enable: true\ntproxy-port: 7893\n", true, "", 7893},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tun, stack, port, err := readBaseSwitchState(c.src)
			if err != nil {
				t.Fatalf("读取失败: %v", err)
			}
			if tun != c.wantTUN {
				t.Errorf("tunEnabled = %v, want %v", tun, c.wantTUN)
			}
			if stack != c.wantStack {
				t.Errorf("tunStack = %q, want %q", stack, c.wantStack)
			}
			if port != c.wantTProxy {
				t.Errorf("tproxyPort = %d, want %d", port, c.wantTProxy)
			}
		})
	}
}

// 反复切换开关不该让文件持续膨胀或漂移——用户会切很多次
func TestPatchIsIdempotent(t *testing.T) {
	once, err := patchBaseYAML(userBaseYAML, "tun.enable", true)
	if err != nil {
		t.Fatalf("首次改写失败: %v", err)
	}
	twice, err := patchBaseYAML(once, "tun.enable", true)
	if err != nil {
		t.Fatalf("二次改写失败: %v", err)
	}
	if once != twice {
		t.Errorf("重复写入同一值产生了不同结果\n第一次:\n%s\n第二次:\n%s", once, twice)
	}
}

// readBaseSwitchState 对同一份文本的理解必须与合并流程（yaml.Unmarshal 到
// domain.Config）完全一致。
//
// 这条不变量是"面板状态"与"实际下发的配置"之间唯一的连接。早先这里用
// node.Value == "true" 判布尔、用 fmt.Sscanf 解端口，两处都比 yaml 库更宽或更窄：
//   - True / TRUE / yes / on 都是 YAML 的真值，字符串比较会漏掉，面板显示
//     "已关闭"而 mihomo 实际跑着 TUN；
//   - Sscanf 只要前缀匹配就算成功，"7893abc" 会被解成 7893，而合并流程会
//     拒绝整份配置——面板据此下发的防火墙规则指向一个内核没在监听的端口。
//
// 用表驱动直接比对两条解析路径，而不是各自断言期望值：期望值要靠人算，
// 而这里真正要保证的就是"两边一致"，让它们互为参照更难写错。
func TestReadBaseSwitchStateMatchesMergeDecoding(t *testing.T) {
	cases := []string{
		"tun:\n  enable: true\n",
		"tun:\n  enable: True\n",
		"tun:\n  enable: TRUE\n",
		"tun:\n  enable: yes\n",
		"tun:\n  enable: on\n",
		"tun:\n  enable: false\n",
		"tun:\n  enable: off\n",
		"tun:\n  enable: no\n",
		"tproxy-port: 7893\n",
		"tproxy-port: 0x1ed9\n",
		"mode: rule\n",
	}
	for _, src := range cases {
		t.Run(strings.ReplaceAll(strings.TrimSpace(src), "\n", " "), func(t *testing.T) {
			tun, _, port, err := readBaseSwitchState(src)
			if err != nil {
				t.Fatalf("readBaseSwitchState 失败: %v", err)
			}

			var cfg domain.Config
			if uerr := yaml.Unmarshal([]byte(src), &cfg); uerr != nil {
				t.Fatalf("合并流程的解码失败（本用例只覆盖两边都该成功的输入）: %v", uerr)
			}

			if tun != cfg.TUN.Enable {
				t.Errorf("tun.enable 判定与合并流程不一致: 面板 %v, 合并 %v", tun, cfg.TUN.Enable)
			}
			if port != cfg.TProxyPort {
				t.Errorf("tproxy-port 判定与合并流程不一致: 面板 %d, 合并 %d", port, cfg.TProxyPort)
			}
		})
	}
}

// 合并流程会拒绝的值，这里也必须报错而不是"尽力猜一个"。
//
// 猜出来的值比报错更危险：面板会拿它去下发防火墙规则，而内核因为配置无效
// 根本没有监听那个端口，结果是流量被引进黑洞且界面显示一切正常。
func TestReadBaseSwitchStateRejectsWhatMergeRejects(t *testing.T) {
	cases := []struct{ name, src string }{
		{"布尔值是任意字符串", "tun:\n  enable: nope\n"},
		{"端口带非数字后缀", "tproxy-port: 7893abc\n"},
		{"端口被引号包成字符串", "tproxy-port: \"7893\"\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 前提：合并流程确实拒绝这份输入
			var cfg domain.Config
			if uerr := yaml.Unmarshal([]byte(c.src), &cfg); uerr == nil {
				t.Fatalf("前提不成立：合并流程接受了 %q", c.src)
			}
			if _, _, _, err := readBaseSwitchState(c.src); err == nil {
				t.Errorf("合并流程拒绝的输入这里也应报错，而不是猜一个值: %q", c.src)
			}
		})
	}
}
