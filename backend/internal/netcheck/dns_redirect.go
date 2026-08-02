package netcheck

import (
	"context"
	"fmt"
	"strings"
)

// AGH DNS 专用 nft 表名：与 aurora_tproxy 分离，无 TProxy 时也能单独劫持 53。
const aghDNSTable = "aurora_agh_dns"

// DNSRedirectParams 仅 DNS 重定向（不启全量 TProxy）。
type DNSRedirectParams struct {
	// DNSPort AdGuard（或其它本机 DNS）监听端口，必须 >0 且通常 ≠53。
	DNSPort int
	// EnableIPv6 为 true 时 output 链同时处理 IPv6（排除 ::1）。
	EnableIPv6 bool
}

// Validate 检查参数。
func (p DNSRedirectParams) Validate() error {
	if p.DNSPort <= 0 || p.DNSPort > 65535 {
		return fmt.Errorf("DNS 重定向目标端口非法: %d", p.DNSPort)
	}
	if p.DNSPort == 53 {
		return fmt.Errorf("重定向目标不能是 53：请让 AdGuard 监听高位端口（如 1053），或改用「使用 53 端口」模式")
	}
	return nil
}

// BuildAGHDNSRedirectNFT 生成仅劫持 53→DNSPort 的 nft 脚本。
//
// 使用 nat redirect（不是 tproxy）：不依赖策略路由，适合「未开 TProxy、
// 但希望本机/经本机转发的 DNS 进 AdGuard」的场景。
//
// 环路注意：若 AdGuard 上游仍是「某 IP 的 53 端口」，output 链重定向会
// 把上游查询再送回 AdGuard。模式 2 默认把上游指到 mihomo 高位端口或 DoH，
// 以降低环路概率。
func BuildAGHDNSRedirectNFT(p DNSRedirectParams) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("flush table inet " + aghDNSTable + "\n")
	b.WriteString("table inet " + aghDNSTable + " {\n")
	b.WriteString("  chain prerouting {\n")
	b.WriteString("    type nat hook prerouting priority dstnat - 5; policy accept;\n")
	fmt.Fprintf(&b, "    meta l4proto { tcp, udp } th dport 53 redirect to :%d\n", p.DNSPort)
	b.WriteString("  }\n")
	b.WriteString("  chain output {\n")
	b.WriteString("    type nat hook output priority -95; policy accept;\n")
	// 排除回环目的，避免干扰本机 127.0.0.1 上的解析与自检
	fmt.Fprintf(&b, "    meta l4proto { tcp, udp } th dport 53 ip daddr != 127.0.0.0/8 redirect to :%d\n", p.DNSPort)
	if p.EnableIPv6 {
		fmt.Fprintf(&b, "    meta l4proto { tcp, udp } th dport 53 ip6 daddr != ::1/128 redirect to :%d\n", p.DNSPort)
	}
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String(), nil
}

// AGHDNSRedirectTeardownCommand 删除专用表（幂等）。
func AGHDNSRedirectTeardownCommand() []string {
	return []string{"nft", "delete", "table", "inet", aghDNSTable}
}

// ApplyDNSRedirect 下发仅 DNS 重定向规则。
//
// 下发前探测 DNSPort 是否有 UDP 监听：无人听则拒绝，避免把全网 DNS 导入黑洞。
func (a *Applier) ApplyDNSRedirect(ctx context.Context, p DNSRedirectParams) error {
	if err := p.Validate(); err != nil {
		return err
	}
	script, err := BuildAGHDNSRedirectNFT(p)
	if err != nil {
		return err
	}

	if !a.udpPortInUse(p.DNSPort) {
		return fmt.Errorf("本机 UDP %d 无进程监听，拒绝下发 53→%d 重定向（否则域名解析会失败）。请先启动 AdGuard 并确认 DNS 端口", p.DNSPort, p.DNSPort)
	}

	if a.Runner == nil {
		return fmt.Errorf("未配置命令执行器，无法下发 DNS 重定向")
	}
	// 先删旧表再 apply，保证幂等替换
	_, _ = a.Runner.Run(ctx, AGHDNSRedirectTeardownCommand()[0], AGHDNSRedirectTeardownCommand()[1:]...)
	if out, err := a.Runner.RunWithStdin(ctx, script, "nft", "-f", "-"); err != nil {
		return fmt.Errorf("下发 AdGuard DNS 重定向失败: %w (%s)", err, strings.TrimSpace(out))
	}
	if a.Logf != nil {
		a.Logf("applied aurora_agh_dns redirect 53 -> :%d", p.DNSPort)
	}
	return nil
}

// TeardownDNSRedirect 拆除仅 DNS 重定向表（幂等）。
func (a *Applier) TeardownDNSRedirect(ctx context.Context) error {
	if a.Runner == nil {
		return nil
	}
	cmd := AGHDNSRedirectTeardownCommand()
	if out, err := a.Runner.Run(ctx, cmd[0], cmd[1:]...); err != nil {
		// 表不存在时 nft 非 0，视为已干净
		msg := strings.ToLower(out + err.Error())
		if strings.Contains(msg, "no such file") ||
			strings.Contains(msg, "does not exist") ||
			strings.Contains(msg, "not found") {
			return nil
		}
		return fmt.Errorf("拆除 aurora_agh_dns 失败: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}
