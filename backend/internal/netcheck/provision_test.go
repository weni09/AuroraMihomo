package netcheck

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// linuxReport 造一份"Linux + root + 缺依赖"的基线报告，各用例按需改字段。
func linuxReport(pm string) *Report {
	return &Report{
		OS:             "linux",
		Root:           true,
		PackageManager: pm,
		Modes: []ModeStatus{
			{Mode: ModeTUN, Available: true},
			{Mode: ModeTProxy, Available: false, Missing: []string{"nft 或 iptables"}},
		},
	}
}

func newProvisioner(r CommandRunner, root string) *Provisioner {
	return &Provisioner{Runner: r, Root: root}
}

// 非 Linux 直接拒绝：macOS 没有这套包管理器，Windows 压根不支持透明代理
func TestProvisionRejectsNonLinux(t *testing.T) {
	rep := linuxReport("apt-get")
	rep.OS = "darwin"

	_, err := newProvisioner(newFakeRunner(), t.TempDir()).
		Provision(context.Background(), rep, ProvisionOptions{InstallPackages: true})
	if err == nil {
		t.Fatal("非 Linux 应拒绝")
	}
	if !strings.Contains(err.Error(), "darwin") {
		t.Errorf("报错应点明当前系统，实际: %v", err)
	}
}

// 非 root 提前拒绝，而不是跑一半失败留下半装状态
func TestProvisionRejectsNonRoot(t *testing.T) {
	rep := linuxReport("apt-get")
	rep.Root = false
	r := newFakeRunner()

	_, err := newProvisioner(r, t.TempDir()).
		Provision(context.Background(), rep, ProvisionOptions{InstallPackages: true})
	if err == nil {
		t.Fatal("非 root 应拒绝")
	}
	// 报错要有可操作性：告诉用户可以手动执行或以 root 运行
	if !strings.Contains(err.Error(), "root") {
		t.Errorf("报错应说明需要 root，实际: %v", err)
	}
	if len(r.calls) != 0 {
		t.Errorf("拒绝后不应执行任何命令，实际执行了 %v", r.calls)
	}
}

// 两个开关都不开时不该悄悄什么都不做地返回成功
func TestProvisionRejectsEmptyOptions(t *testing.T) {
	_, err := newProvisioner(newFakeRunner(), t.TempDir()).
		Provision(context.Background(), linuxReport("apt-get"), ProvisionOptions{})
	if err == nil {
		t.Error("未指定操作时应报错")
	}
}

// Debian 系必须先 apt-get update：全新系统没有索引，直接 install 会失败
func TestProvisionAptRefreshesBeforeInstall(t *testing.T) {
	r := newFakeRunner()
	res, err := newProvisioner(r, t.TempDir()).
		Provision(context.Background(), linuxReport("apt-get"),
			ProvisionOptions{InstallPackages: true})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !res.Success {
		t.Errorf("应成功，实际 %+v", res.Steps)
	}

	update := r.indexOf("apt-get update")
	install := r.indexOf("DEBIAN_FRONTEND=noninteractive apt-get install")
	if update < 0 || install < 0 {
		t.Fatalf("缺少 update 或 install 步骤:\n%s", r.joined())
	}
	if update > install {
		t.Errorf("apt-get update(%d) 必须在 install(%d) 之前:\n%s",
			update, install, r.joined())
	}
}

// 源不可达时不该继续装：继续必然失败，且会把真正的原因埋在第二条报错里
func TestProvisionStopsWhenAptUpdateFails(t *testing.T) {
	r := newFakeRunner()
	r.failOn["apt-get update"] = "Could not resolve 'archive.ubuntu.com'"

	res, err := newProvisioner(r, t.TempDir()).
		Provision(context.Background(), linuxReport("apt-get"),
			ProvisionOptions{InstallPackages: true})
	if err != nil {
		t.Fatalf("单步失败不该返回 error: %v", err)
	}
	if res.Success {
		t.Error("update 失败时整体应为失败")
	}
	if r.indexOf("apt-get install") >= 0 {
		t.Errorf("update 失败后不该继续 install:\n%s", r.joined())
	}
	// 原始输出要透传，否则用户不知道是 DNS 问题还是源配置问题
	if !strings.Contains(res.Steps[0].Detail, "Could not resolve") {
		t.Errorf("应透传命令原始输出，实际 %q", res.Steps[0].Detail)
	}
}

// Alpine 走 apk add，且不需要单独的 update 步骤
func TestProvisionApkInstalls(t *testing.T) {
	r := newFakeRunner()
	res, err := newProvisioner(r, t.TempDir()).
		Provision(context.Background(), linuxReport("apk"),
			ProvisionOptions{InstallPackages: true})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !res.Success {
		t.Errorf("应成功，实际 %+v", res.Steps)
	}
	if r.indexOf("apk add --no-cache") < 0 {
		t.Errorf("应执行 apk add:\n%s", r.joined())
	}
	// Alpine 的 ip6tables 是独立包，漏了会导致 IPv6 规则下发失败
	if !strings.Contains(r.joined(), "ip6tables") {
		t.Errorf("Alpine 需显式安装 ip6tables:\n%s", r.joined())
	}
}

// 认不出包管理器时如实说明并只给手动命令，不去瞎猜
func TestProvisionWithoutPackageManager(t *testing.T) {
	r := newFakeRunner()
	res, err := newProvisioner(r, t.TempDir()).
		Provision(context.Background(), linuxReport(""),
			ProvisionOptions{InstallPackages: true})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if res.Success {
		t.Error("无法安装时不应报成功")
	}
	if len(r.calls) != 0 {
		t.Errorf("认不出包管理器时不该执行命令，实际 %v", r.calls)
	}
	if len(res.ManualCommands) == 0 {
		t.Error("应给出手动命令兜底")
	}
}

// 幂等：依赖已齐时跳过，不去白跑一次联网的 apt-get update
func TestProvisionSkipsWhenPackagesAlreadySatisfied(t *testing.T) {
	rep := linuxReport("apt-get")
	rep.Modes = []ModeStatus{
		{Mode: ModeTUN, Available: true},
		{Mode: ModeTProxy, Available: true}, // Missing 为空
	}
	r := newFakeRunner()

	res, err := newProvisioner(r, t.TempDir()).
		Provision(context.Background(), rep, ProvisionOptions{InstallPackages: true})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !res.Success {
		t.Error("已满足条件应视为成功")
	}
	if !res.Steps[0].Skipped {
		t.Errorf("应标记为跳过，实际 %+v", res.Steps[0])
	}
	if len(r.calls) != 0 {
		t.Errorf("已满足时不该执行包管理器，实际 %v", r.calls)
	}
}

// 容器内装包能成功但重启即丢，必须标出来，否则用户会以为一次搞定
func TestProvisionMarksNotPersistentInContainer(t *testing.T) {
	rep := linuxReport("apk")
	rep.InContainer = true

	res, err := newProvisioner(newFakeRunner(), t.TempDir()).
		Provision(context.Background(), rep, ProvisionOptions{InstallPackages: true})
	if err != nil {
		t.Fatalf("容器内不该拒绝装包: %v", err)
	}
	if !res.NotPersistent {
		t.Error("容器内应标记 NotPersistent，提示用户改镜像才能持久")
	}
}

// ---- sysctl ----

func sysctlReport(ipForward, rpFilter string, ifaces ...string) *Report {
	r := linuxReport("apt-get")
	r.SysctlIPForward = ipForward
	r.SysctlRPFilter = rpFilter
	r.RPFilterStrictIfaces = ifaces
	return r
}

func TestProvisionWritesSysctlDropIn(t *testing.T) {
	root := t.TempDir()
	r := newFakeRunner()

	res, err := newProvisioner(r, root).Provision(context.Background(),
		sysctlReport("0", "1", "ens18"), ProvisionOptions{ApplySysctl: true})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !res.Success {
		t.Fatalf("应成功，实际 %+v", res.Steps)
	}

	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(sysctlDropInRelPath)))
	if err != nil {
		t.Fatalf("应写出 drop-in 文件: %v", err)
	}
	s := string(body)
	for _, want := range []string{
		"net.ipv4.ip_forward = 1",
		"net.ipv4.conf.all.rp_filter = 2",
		// default 管新出现的网卡，漏了它之后新建网卡仍是严格模式
		"net.ipv4.conf.default.rp_filter = 2",
		// 关键：内核取 max(all, 网卡)，不写这条则 all 的设置在 ens18 上不生效
		"net.ipv4.conf.ens18.rp_filter = 2",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("配置里应包含 %q，实际内容:\n%s", want, s)
		}
	}
	// 写文件 ≠ 已生效，必须再让它加载一次
	if r.indexOf("sysctl --system") < 0 {
		t.Errorf("写完应执行 sysctl --system 使其生效:\n%s", r.joined())
	}
}

// BusyBox 的 sysctl 不认 --system（那是 procps-ng 的 GNU 扩展），
// 此时必须回退到逐文件 sysctl -p，否则配置文件写了却不生效、
// rp_filter 仍是严格模式，TProxy 收不到包而界面显示"部分成功"。
// 这是在 Alpine 真机上发现的缺陷，与既有的 ip --version 误判同源。
func TestProvisionSysctlFallsBackWhenSystemFlagUnsupported(t *testing.T) {
	root := t.TempDir()
	r := newFakeRunner()
	r.failOn["sysctl --system"] = "sysctl: unrecognized option: system\n" +
		"BusyBox v1.37.0 multi-call binary."

	res, err := newProvisioner(r, root).Provision(context.Background(),
		sysctlReport("0", "1", "eth0"), ProvisionOptions{ApplySysctl: true})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}

	dropIn := filepath.Join(root, filepath.FromSlash(sysctlDropInRelPath))
	if r.indexOf("sysctl -p "+dropIn) < 0 {
		t.Errorf("--system 失败后应回退为 sysctl -p <drop-in>:\n%s", r.joined())
	}
	// 回退成功即整体成功：目标是"内核值生效"，用哪个选项达成不重要
	if !res.Success {
		t.Errorf("回退成功后整体应报成功，实际 %+v", res.Steps)
	}
}

// 回退路径也失败时必须如实报错，不能因为"试过两种"就当成成功。
func TestProvisionSysctlReportsFailureWhenFallbackAlsoFails(t *testing.T) {
	root := t.TempDir()
	r := newFakeRunner()
	r.failOn["sysctl"] = "sysctl: permission denied"

	res, err := newProvisioner(r, root).Provision(context.Background(),
		sysctlReport("0", "1", "eth0"), ProvisionOptions{ApplySysctl: true})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if res.Success {
		t.Error("两种方式都失败时不应报成功")
	}
}

// manualCommands 给用户复制到终端执行，不能带上在 BusyBox 上必然失败的
// --system —— 用户照抄会得到和面板一样的错误，却没有回退可依赖。
func TestProvisionManualSysctlCommandWorksOnBusybox(t *testing.T) {
	res, err := newProvisioner(newFakeRunner(), t.TempDir()).
		Provision(context.Background(), sysctlReport("0", "1", "eth0"),
			ProvisionOptions{ApplySysctl: true})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	joined := strings.Join(res.ManualCommands, "\n")
	if !strings.Contains(joined, "sysctl -p /"+sysctlDropInRelPath) {
		t.Errorf("手动命令应给出 BusyBox 也支持的 sysctl -p，实际:\n%s", joined)
	}
}

// rp_filter 本来就宽松、转发也开着时不该白写文件
func TestProvisionSkipsSysctlWhenCompliant(t *testing.T) {
	root := t.TempDir()
	r := newFakeRunner()

	res, err := newProvisioner(r, root).Provision(context.Background(),
		sysctlReport("1", "2"), ProvisionOptions{ApplySysctl: true})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !res.Steps[0].Skipped {
		t.Errorf("已合规应跳过，实际 %+v", res.Steps[0])
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(sysctlDropInRelPath))); err == nil {
		t.Error("无需改动时不该创建配置文件")
	}
	if len(r.calls) != 0 {
		t.Errorf("无需改动时不该执行命令，实际 %v", r.calls)
	}
}

// 只有 ip_forward 需要改时，不该顺手把 rp_filter 也写进去——改得越少越好
func TestProvisionOnlyWritesNeededKeys(t *testing.T) {
	root := t.TempDir()

	_, err := newProvisioner(newFakeRunner(), root).Provision(context.Background(),
		sysctlReport("0", "2"), ProvisionOptions{ApplySysctl: true})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(sysctlDropInRelPath)))
	if err != nil {
		t.Fatalf("应写出文件: %v", err)
	}
	if !strings.Contains(string(body), "net.ipv4.ip_forward") {
		t.Error("应写入 ip_forward")
	}
	if strings.Contains(string(body), "rp_filter") {
		t.Errorf("rp_filter 已合规，不该写入:\n%s", body)
	}
}

// 重复执行不能让同名键在文件里堆积：整体重写而非追加
func TestProvisionSysctlIsIdempotent(t *testing.T) {
	root := t.TempDir()
	rep := sysctlReport("0", "1", "ens18")
	p := newProvisioner(newFakeRunner(), root)

	for i := 0; i < 3; i++ {
		if _, err := p.Provision(context.Background(), rep,
			ProvisionOptions{ApplySysctl: true}); err != nil {
			t.Fatalf("第 %d 次失败: %v", i+1, err)
		}
	}

	body, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(sysctlDropInRelPath)))
	if n := strings.Count(string(body), "net.ipv4.ip_forward = 1"); n != 1 {
		t.Errorf("重复执行后 ip_forward 应只出现 1 次，实际 %d 次:\n%s", n, body)
	}
}

// 容器内不动 sysctl：非特权容器会被拒绝，host 网络下会直接改到宿主
func TestProvisionRefusesSysctlInContainer(t *testing.T) {
	root := t.TempDir()
	rep := sysctlReport("0", "1")
	rep.InContainer = true
	r := newFakeRunner()

	res, err := newProvisioner(r, root).Provision(context.Background(), rep,
		ProvisionOptions{ApplySysctl: true})
	if err != nil {
		t.Fatalf("不该整体报错: %v", err)
	}
	if res.Success {
		t.Error("容器内 sysctl 未执行，不应报成功")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(sysctlDropInRelPath))); err == nil {
		t.Error("容器内不该写 sysctl 文件")
	}
	if len(r.calls) != 0 {
		t.Errorf("容器内不该执行 sysctl，实际 %v", r.calls)
	}
	if len(res.ManualCommands) == 0 {
		t.Error("应给出宿主上执行的手动命令")
	}
}

// 装包成功但 sysctl 失败是常见组合，不能合成一个布尔值糊过去
func TestProvisionReportsPartialFailure(t *testing.T) {
	root := t.TempDir()
	r := newFakeRunner()
	r.failOn["sysctl --system"] = "sysctl: cannot stat /proc/sys/net/ipv4/xxx"

	res, err := newProvisioner(r, root).Provision(context.Background(),
		sysctlReport("0", "1"),
		ProvisionOptions{InstallPackages: true, ApplySysctl: true})
	if err != nil {
		t.Fatalf("不该整体报错: %v", err)
	}
	if res.Success {
		t.Error("有步骤失败时整体应为失败")
	}

	var installOK, sysctlFailed bool
	for _, s := range res.Steps {
		if strings.Contains(s.Command, "apt-get install") && s.Success {
			installOK = true
		}
		if strings.Contains(s.Command, "sysctl --system") && !s.Success {
			sysctlFailed = true
		}
	}
	if !installOK {
		t.Errorf("装包应成功并如实标记:\n%+v", res.Steps)
	}
	if !sysctlFailed {
		t.Errorf("sysctl 失败应如实标记:\n%+v", res.Steps)
	}
	// 总结里要能看出"部分成功"，否则用户不知道该修哪一半
	if !strings.Contains(res.Message, "失败") {
		t.Errorf("总结应提到失败项，实际 %q", res.Message)
	}
}

// 手动命令必须始终提供：失败时是兜底，成功时用户也常要记进部署脚本
func TestProvisionAlwaysProvidesManualCommands(t *testing.T) {
	res, err := newProvisioner(newFakeRunner(), t.TempDir()).
		Provision(context.Background(), sysctlReport("0", "1"),
			ProvisionOptions{InstallPackages: true, ApplySysctl: true})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	joined := strings.Join(res.ManualCommands, "\n")
	if !strings.Contains(joined, "apt-get install") {
		t.Errorf("应含装包命令:\n%s", joined)
	}
	// 断言"有让 sysctl 生效的命令"而不锁定具体选项：这里刻意用 -p 而非
	// --system，因为手动命令要在 BusyBox 环境下也能原样粘贴执行
	if !strings.Contains(joined, "sysctl -p") {
		t.Errorf("应含使 sysctl 生效的命令:\n%s", joined)
	}
	if !strings.Contains(joined, sysctlDropInRelPath) {
		t.Errorf("应含写 drop-in 的路径:\n%s", joined)
	}
}

// 手动命令是给用户复制到终端执行的，必须能原样粘贴
func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	got := shellQuote("a'b")
	if got != `'a'\''b'` {
		t.Errorf("单引号转义有误，实际 %s", got)
	}
}

// 安装提示与实际执行的包列表必须同源，否则会出现"提示装 A、按钮装 B"
func TestInstallHintMatchesRequiredPackages(t *testing.T) {
	for pm, pkgs := range requiredPackages {
		hint := installHintFor(pm)
		for _, pkg := range pkgs {
			if !strings.Contains(hint, pkg) {
				t.Errorf("%s 的提示缺少包 %q: %s", pm, pkg, hint)
			}
		}
	}
	// 未知包管理器要给出可读的兜底，而不是空串
	if installHintFor("pacman") == "" {
		t.Error("未知包管理器也应给出提示")
	}
}
