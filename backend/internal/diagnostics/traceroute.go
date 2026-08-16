package diagnostics

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Hop 是 traceroute 输出中的一跳，直接序列化给前端展示。
type Hop struct {
	Hop  int    `json:"hop"`
	Addr string `json:"addr,omitempty"`
	RTT  string `json:"rtt,omitempty"`
}

// TracerouteProbe 调用系统 traceroute/tracert 命令并解析逐跳结果。
//
// RunCmd 可注入（测试注入固定输出、错误与阻塞行为）；nil 时使用系统命令
// （Unix 系 traceroute -n，Windows tracert -d）。
// 无全局可变状态：Timeout/RunCmd 在 Run 内按值读取并解析默认值，
// 不写回结构体字段，同一实例可安全并发执行。
type TracerouteProbe struct {
	Timeout time.Duration
	RunCmd  func(ctx context.Context, host string) ([]byte, error)
}

// ProbeTimeout 返回该探测期望的独立超时：显式设置时用显式值，否则 30s。
// 实现 TimeoutProbe——traceroute 常需逐跳等待多个超时，服务级 5s 会过早
// 掐断整条链路探测。
func (p *TracerouteProbe) ProbeTimeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return 30 * time.Second
}

// Run 执行一次 traceroute 探测。
//
// 生命周期与状态语义：
//   - RunCmd 为 nil 时回落到系统命令；命令缺失（*exec.Error，未安装）回填
//     fail 并给出明确错误「系统未安装 traceroute/tracert」；
//   - 命令执行失败（退出码/输出错误）回填 fail；上下文到期未完成回填 timeout；
//   - 成功回填 success，Detail 携带 {hops, raw}：hops 为解析后的逐跳数组，
//     raw 为命令原始输出。
func (p *TracerouteProbe) Run(ctx context.Context, target DiagnosticTarget, path string, cb ProgressFunc) ProbeResult {
	runCmd := p.RunCmd
	if runCmd == nil {
		runCmd = systemTraceroute
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	out, err := runCmd(pctx, target.Target)
	latency := time.Since(start)
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return ProbeResult{Target: target.Target, Type: TypeTraceroute, Path: path, Status: StatusFail, Error: "系统未安装 traceroute/tracert"}
		}
		status := StatusFail
		if pctx.Err() != nil {
			status = StatusTimeout
		}
		return ProbeResult{Target: target.Target, Type: TypeTraceroute, Path: path, Status: status, LatencyMs: latency.Milliseconds(), Error: err.Error()}
	}
	detail := map[string]interface{}{
		"hops": parseTraceroute(string(out)),
		"raw":  string(out),
	}
	if path == PathProxy {
		// 系统 traceroute/tracert 不经 HTTP 代理：结果与 direct 路径一致，
		// 如实标注，避免把直出路径误读为代理路径数据。
		detail["note"] = "traceroute 不经 HTTP 代理，结果与 direct 路径一致"
	}
	return ProbeResult{
		Target:    target.Target,
		Type:      TypeTraceroute,
		Path:      path,
		Status:    StatusSuccess,
		LatencyMs: latency.Milliseconds(),
		Detail:    detail,
	}
}

// systemTraceroute 用系统 traceroute/tracert 命令探测目标主机。
// CombinedOutput 同时捕获 stderr（如权限提示），便于失败时给出可读错误。
func systemTraceroute(ctx context.Context, host string) ([]byte, error) {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "tracert", "-d", host).CombinedOutput()
	}
	return exec.CommandContext(ctx, "traceroute", "-n", host).CombinedOutput()
}

// parseTraceroute 解析 traceroute/tracert 命令输出，提取逐跳信息。
//
// 支持的两种行格式（首列均为纯数字跳数，行首空格不影响解析）：
//   - Unix traceroute -n：1  192.168.1.1  1.2 ms  1.1 ms  1.3 ms
//   - Windows tracert -d：1     1 ms     1 ms     1 ms  192.168.1.1
//
// 头部（traceroute to .../Tracing route to ...）、尾部（Trace complete.）等
// 非跳行直接跳过；跳无响应（*）时地址与 RTT 留空。地址取行内首个 IP 形字段
// （Unix 在第二列，Windows 在末列），RTT 取首个「N ms」样式组合。
func parseTraceroute(out string) []Hop {
	var hops []Hop
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil || n <= 0 {
			continue
		}
		hops = append(hops, Hop{Hop: n, Addr: traceAddr(fields[1:]), RTT: traceRTT(fields[1:])})
	}
	return hops
}

// traceAddr 从跳行字段中找地址：Unix traceroute 的地址在第二列，
// Windows tracert 的地址在末列，统一取首个 IP 形字段。
func traceAddr(fields []string) string {
	for _, f := range fields {
		if isIPField(f) {
			return f
		}
	}
	return ""
}

// traceRTT 取跳行中首个「N ms」样式的 RTT 展示串（Unix 为 "1.2 ms"，
// Windows 为 "1 ms"），无有效 RTT 时返回空串。
func traceRTT(fields []string) string {
	for i := 0; i+1 < len(fields); i++ {
		if isRTTField(fields[i]) && strings.EqualFold(fields[i+1], "ms") {
			return fields[i] + " " + fields[i+1]
		}
	}
	return ""
}

// isIPField 判断字段是否为 IP 地址：含 . 或 : 且本身不是 RTT 数值。
// 借此把 "192.168.1.1" 判为地址，把 "1.2" 判为 RTT 数值。
func isIPField(f string) bool {
	if isRTTField(f) {
		return false
	}
	return strings.Contains(f, ".") || strings.Contains(f, ":")
}

// isRTTField 判断字段是否为 RTT 数值；tracert 的 "<1" 也视为合法。
func isRTTField(f string) bool {
	trimmed := strings.TrimPrefix(f, "<")
	_, err := strconv.ParseFloat(trimmed, 64)
	return err == nil
}
