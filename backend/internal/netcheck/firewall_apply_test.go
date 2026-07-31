package netcheck

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// fakeRunner 记录被执行的命令，并可让指定命令失败。
type fakeRunner struct {
	calls []string
	// failOn 命令前缀 -> 返回的输出，命中则报错
	failOn map[string]string
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{failOn: map[string]string{}}
}

func (f *fakeRunner) record(name string, args []string) (string, error) {
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	f.calls = append(f.calls, line)
	for prefix, out := range f.failOn {
		if strings.HasPrefix(line, prefix) {
			return out, fmt.Errorf("模拟失败: %s", prefix)
		}
	}
	return "", nil
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	return f.record(name, args)
}

func (f *fakeRunner) RunWithStdin(_ context.Context, _ string, name string, args ...string) (string, error) {
	return f.record(name, args)
}

// RunEnv 把环境变量前置到记录里，使断言可以检查"有没有设
// DEBIAN_FRONTEND"这类要求
func (f *fakeRunner) RunEnv(_ context.Context, env []string, name string, args ...string) (string, error) {
	if len(env) > 0 {
		return f.record(strings.Join(env, " ")+" "+name, args)
	}
	return f.record(name, args)
}

func (f *fakeRunner) joined() string { return strings.Join(f.calls, "\n") }

func (f *fakeRunner) indexOf(prefix string) int {
	for i, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

func newApplier(r CommandRunner, dir string) *Applier {
	return &Applier{SnapshotDir: dir, Runner: r}
}

// 干跑校验必须发生在任何实际改动之前，否则语法错误会留下半应用状态。
func TestApplyValidatesBeforeAnyChange(t *testing.T) {
	r := newFakeRunner()
	r.failOn["nft --check"] = "syntax error"

	err := newApplier(r, t.TempDir()).Apply(context.Background(), sampleParams())
	if err == nil {
		t.Fatal("校验失败时 Apply 应报错")
	}
	if !strings.Contains(err.Error(), "未做任何改动") {
		t.Errorf("报错应说明未做改动，实际: %v", err)
	}
	// 除了 --check，不应执行任何写操作
	for _, c := range r.calls {
		if strings.HasPrefix(c, "nft --check") {
			continue
		}
		t.Errorf("校验未通过却执行了 %q", c)
	}
}

// 策略路由必须先于规则下发：反过来会出现"规则已生效、路由未就绪"的黑洞，
// 表现就是应用瞬间断网。
func TestApplyConfiguresRouteBeforeRules(t *testing.T) {
	r := newFakeRunner()
	if err := newApplier(r, t.TempDir()).Apply(context.Background(), sampleParams()); err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}

	route := r.indexOf("ip route add local")
	// 真正下发规则是 `nft -f -`，要与 `nft --check -f -` 区分开
	var rules = -1
	for i, c := range r.calls {
		if strings.HasPrefix(c, "nft -f") {
			rules = i
			break
		}
	}
	if route < 0 || rules < 0 {
		t.Fatalf("缺少策略路由或规则下发步骤:\n%s", r.joined())
	}
	if route > rules {
		t.Errorf("策略路由(%d)应在规则下发(%d)之前，否则打标包会被丢弃:\n%s",
			route, rules, r.joined())
	}
}

// 规则下发失败要自动拆除已建的策略路由，不能留半应用状态
func TestApplyRollsBackOnRuleFailure(t *testing.T) {
	r := newFakeRunner()
	r.failOn["nft -f"] = "permission denied"

	err := newApplier(r, t.TempDir()).Apply(context.Background(), sampleParams())
	if err == nil {
		t.Fatal("规则下发失败时应报错")
	}
	if r.indexOf("ip rule del") < 0 {
		t.Errorf("失败后应拆除已建的策略路由:\n%s", r.joined())
	}
	if r.indexOf("nft delete table") < 0 {
		t.Errorf("失败后应尝试删除专用表:\n%s", r.joined())
	}
}

// 重复应用要幂等：策略路由已存在时报 "File exists"，不应视为失败
func TestApplyTreatsExistingRouteAsSuccess(t *testing.T) {
	r := newFakeRunner()
	r.failOn["ip rule add"] = "RTNETLINK answers: File exists"

	if err := newApplier(r, t.TempDir()).Apply(context.Background(), sampleParams()); err != nil {
		t.Errorf("策略路由已存在应视为成功，实际报错: %v", err)
	}
	// 且不应因此触发回滚
	if r.indexOf("ip rule del") >= 0 {
		t.Errorf("已存在不该触发回滚:\n%s", r.joined())
	}
}

// 拆除要幂等：目标本来就不存在时不算错误
func TestTeardownIsIdempotent(t *testing.T) {
	r := newFakeRunner()
	r.failOn["nft delete table"] = "Error: No such file or directory"
	r.failOn["ip rule del"] = "RTNETLINK answers: No such process"

	if err := newApplier(r, t.TempDir()).Teardown(context.Background()); err != nil {
		t.Errorf("目标不存在时拆除应视为成功，实际: %v", err)
	}
}

// 拆除必须无条件清理 v6，不看启用时是不是开了 v6。
//
// 真机测试发现的残留：旧实现按参数决定清不清 v6，而调用方传的是硬编码的
// false，于是在有 IPv6 出网能力的宿主上关闭透明代理后，v6 的 ip rule 与
// local ::/0 路由仍然留在系统里，日志却报告"规则已拆除"。
// 这类残留最隐蔽的地方在于它不影响当次上网，却会让下一次启用/排障时
// 看到一个不该存在的策略路由。
func TestTeardownAlwaysCleansIPv6(t *testing.T) {
	r := newFakeRunner()
	if err := newApplier(r, t.TempDir()).Teardown(context.Background()); err != nil {
		t.Fatalf("Teardown 失败: %v", err)
	}
	all := r.joined()
	for _, want := range []string{
		"ip -6 rule del fwmark 1 table 100",
		"ip -6 route flush table 100",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("拆除未执行 %q，v6 策略路由会残留:\n%s", want, all)
		}
	}
}

// 拆除顺序：先删规则再撤路由。反序会短暂出现"规则在、路由没了"的黑洞。
func TestTeardownRemovesRulesBeforeRoutes(t *testing.T) {
	r := newFakeRunner()
	if err := newApplier(r, t.TempDir()).Teardown(context.Background()); err != nil {
		t.Fatalf("Teardown 失败: %v", err)
	}
	nft := r.indexOf("nft delete table")
	rule := r.indexOf("ip rule del")
	if nft < 0 || rule < 0 {
		t.Fatalf("拆除步骤不全:\n%s", r.joined())
	}
	if nft > rule {
		t.Errorf("应先删 nft 表(%d)再撤策略路由(%d):\n%s", nft, rule, r.joined())
	}
}

// 拆除只能动自己的表，不得整体 flush
func TestTeardownNeverFlushesEverything(t *testing.T) {
	r := newFakeRunner()
	_ = newApplier(r, t.TempDir()).Teardown(context.Background())

	all := r.joined()
	for _, forbidden := range []string{"flush ruleset", "iptables -F", "-t nat -F", "nft flush"} {
		if strings.Contains(all, forbidden) {
			t.Errorf("拆除执行了危险操作 %q，会抹掉宿主其它规则:\n%s", forbidden, all)
		}
	}
	// ip route flush 只针对自己的表号，这是允许的
	if strings.Contains(all, "ip route flush table") &&
		!strings.Contains(all, "ip route flush table 100") {
		t.Errorf("route flush 必须限定到专用表号:\n%s", all)
	}
}

// 快照失败不应阻断流程，但要真的落盘可用内容
func TestSnapshotWritesCollectedOutput(t *testing.T) {
	dir := t.TempDir()
	r := newFakeRunner()
	// 让 nft 不可用，模拟未装 nftables 的机器
	r.failOn["nft list ruleset"] = "command not found"

	out, err := newApplier(r, dir).Snapshot(context.Background())
	if err != nil {
		t.Fatalf("快照应容忍部分命令失败: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("读快照目录失败: %v", err)
	}
	if len(entries) == 0 {
		t.Error("快照目录为空，出问题时无从恢复")
	}
	// 失败的那项不该留下空文件误导用户
	for _, e := range entries {
		if e.Name() == "nft.ruleset" {
			t.Error("采集失败的项不应写出文件")
		}
	}
}

func TestSnapshotRequiresDir(t *testing.T) {
	if _, err := (&Applier{Runner: newFakeRunner()}).Snapshot(context.Background()); err == nil {
		t.Error("未配置快照目录时应报错")
	}
}

// 规则确实存在时，探测应返回 true 且不判定为错误
func TestRulesActiveReturnsTrueWhenTableExists(t *testing.T) {
	r := newFakeRunner()
	active, err := newApplier(r, t.TempDir()).RulesActive(context.Background())
	if err != nil {
		t.Fatalf("探测应成功，实际: %v", err)
	}
	if !active {
		t.Error("nft 命令成功返回时应判定规则存在")
	}
}

// 表不存在是"规则已失效"的正常信号（例如宿主重启后），不应当作探测出错
func TestRulesActiveReturnsFalseWhenTableMissing(t *testing.T) {
	r := newFakeRunner()
	r.failOn["nft list table inet aurora_tproxy"] = "Error: No such file or directory"

	active, err := newApplier(r, t.TempDir()).RulesActive(context.Background())
	if err != nil {
		t.Fatalf("表不存在不应视为探测出错，实际: %v", err)
	}
	if active {
		t.Error("表不存在时应判定规则不存在")
	}
}

// 命令本身执行不了（如 nft 未安装）时，不能贸然断言"规则不存在"，
// 否则会错误地把"探测失败"和"确实没有规则"混为一谈
func TestRulesActiveReturnsErrorOnUnexpectedFailure(t *testing.T) {
	r := newFakeRunner()
	r.failOn["nft list table inet aurora_tproxy"] = "nft: command not found"

	_, err := newApplier(r, t.TempDir()).RulesActive(context.Background())
	if err == nil {
		t.Error("非'表不存在'的失败应报错，交由调用方判断，不能默认为规则已失效")
	}
}
