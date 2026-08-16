package fetcher

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"auroramihomo/backend/internal/netcheck"

	"github.com/zeromicro/go-zero/core/logx"
)

// 云环境链路本地 / 元数据地址：订阅 URL 由登录用户提供，但服务端代发请求。
// 若允许指向这些地址，等于给已登录（或 SSRF 链式）调用方一条读实例身份
// 令牌的捷径。RFC1918 内网故意保留——自建订阅与局域网机场是正当用法。
//
// 校验只看 URL 主机字面量与字面 IP，不做 DNS 解析：解析后的 A 记录仍可能
// 指向 metadata，完整防护需要自定义 Dial 再拦一次；本层先挡住最常见的
// 直接字面量与已知主机名。

var blockedMetadataHosts = map[string]struct{}{
	"metadata.google.internal": {},
	"metadata.google":          {},
	// 部分云厂商 / 工具用的别名
	"instance-data": {},
}

// normalizeFetchURL 把常见「网页查看」链接改写成可直接下载正文的地址。
//
// 模板文件 / 订阅远程源的用户常从浏览器地址栏复制 GitHub 的 /blob/ 页面：
// 该 URL 返回的是带 UI 的 HTML（200），不是 YAML。下游按配置解析就会
// 报一堆难懂的语法错误。改写为 raw.githubusercontent.com 后与「Raw」按钮一致。
//
// 仅处理公开 github.com 路径形态；企业 GitHub / 其它代码托管不猜测。
// 非匹配 URL 原样返回，由后续 validate + 拉取处理。
func normalizeFetchURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	host := strings.ToLower(u.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return rawURL
	}
	// /owner/repo/blob/ref/path...  或  /owner/repo/raw/ref/path...
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 5 {
		return rawURL
	}
	kind := parts[2]
	if kind != "blob" && kind != "raw" {
		return rawURL
	}
	owner, repo, ref := parts[0], parts[1], parts[3]
	filePath := strings.Join(parts[4:], "/")
	if owner == "" || repo == "" || ref == "" || filePath == "" {
		return rawURL
	}
	out := &url.URL{
		Scheme: "https",
		Host:   "raw.githubusercontent.com",
		// Path 用 JoinPath 语义：保留文件名中的特殊字符编码由 String() 处理
		Path: "/" + owner + "/" + repo + "/" + ref + "/" + filePath,
	}
	return out.String()
}

// looksLikeHTMLPage 判断响应是否为网页文档而非配置正文。
//
// 只按正文判断、不采信 Content-Type：V2Board 类机场用 text/html 声明
// 返回 base64 编码的订阅正文（原 Sub-Store 项目照常解析），仅凭声明
// 就把它们拒掉会让整条订阅不可用；而 GitHub 网页/私有仓库登录页的
// 正文以 <!DOCTYPE/<html 开头，无论声明什么类型都是网页，仍会拒掉。
// contentType 参数保留仅为调用处语义完整，实际判定不使用它。
func looksLikeHTMLPage(contentType string, body []byte) bool {
	trim := strings.TrimSpace(string(body))
	if len(trim) < 15 {
		return false
	}
	// 只看开头，避免大文件全量 ToLower
	head := strings.ToLower(trim[:min(64, len(trim))])
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// validateFetchURL 在真正发起 HTTP 请求前做协议与危险主机检查。
func validateFetchURL(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("subscription url is empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("订阅地址无法解析: %w", err)
	}
	sc := strings.ToLower(u.Scheme)
	if sc != "http" && sc != "https" {
		return fmt.Errorf("订阅地址仅支持 http/https，当前为 %q", u.Scheme)
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return fmt.Errorf("订阅地址缺少主机名")
	}
	if _, bad := blockedMetadataHosts[host]; bad {
		return fmt.Errorf("订阅地址指向云元数据主机，已拒绝: %s", host)
	}
	// 字面 IP：拦 link-local（含 169.254.169.254）、loopback 以外的
	// 特殊用途地址中「云 metadata 常用」那一段。loopback / RFC1918 仍放行。
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedMetadataIP(ip) {
			return fmt.Errorf("订阅地址指向链路本地/元数据地址，已拒绝: %s", host)
		}
	}
	return nil
}

// isBlockedMetadataIP 判断是否为云 metadata / 链路本地一类危险地址。
// 不拦 RFC1918、不拦 loopback（本地 mock 与自建服务）。
func isBlockedMetadataIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// IPv4 link-local 169.254.0.0/16（含 169.254.169.254）
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		// AWS/阿里等偶发使用的「伪」metadata：部分文档写 100.100.100.200
		// （阿里云）—— 属于 CGNAT 100.64/10 的子集。只拦已知精确地址，
		// 不整段封 100.64/10，以免误伤运营商 CGNAT 上的合法订阅源。
		if ip4[0] == 100 && ip4[1] == 100 && ip4[2] == 100 && ip4[3] == 200 {
			return true
		}
		return false
	}
	// IPv6 link-local fe80::/10
	if ip.IsLinkLocalUnicast() {
		return true
	}
	// AWS EC2 IMDSv6 的固定地址 fd00:ec2::254 落在 ULA 范围内，
	// 而 ULA 默认放行——这里显式拦截，避免云实例凭据被读出。
	if strings.EqualFold(ip.String(), "fd00:ec2::254") {
		return true
	}
	// IPv6 唯一本地地址不拦（fc00::/7 类似 RFC1918），上面的精确地址除外
	return false
}

// maxRedirects 订阅源 302 到 CDN 属正常用法，一般 1-2 跳；
// 5 跳留足余量，同时防无限跳转链。Go 默认 10 跳但无逐跳校验。
const maxRedirects = 5

// checkRedirect 作为 http.Client.CheckRedirect：对每一跳目标复用
// validateFetchURL 做协议与云 metadata 黑名单校验，并限制跳数。
// 不设该回调时 net/http 默认无校验地跟随 10 跳——恶意上游只需
// 302 指向 169.254.169.254 即可绕过初始 URL 检查（见文件头注释）。
// 返回非 nil 错误时 net/http 会中止本次请求，不会发出跳转请求。
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("订阅地址重定向次数超过上限 %d", maxRedirects)
	}
	if err := validateFetchURL(req.URL.String()); err != nil {
		return fmt.Errorf("订阅地址重定向目标被拒绝: %w", err)
	}
	return nil
}

// guardedDialContext 在 MarkedDialContext 外再包一层 DNS 复验。
//
// validateFetchURL / checkRedirect 只检查 URL 的主机名字面量与字面 IP，
// 而域名可在请求时经 DNS 重绑定解析到云 metadata（169.254.169.254）等
// 被拦地址。本函数在真正拨号前先解析域名，对每个解析出的 IP 执行
// isBlockedMetadataIP，命中则拒绝建连——从而封死 DNS 重绑定绕过 SSRF
// 防护的通道。
//
// 对已是 IP 字面量的地址直接检查（与 validateFetchURL 一致）；
// 对域名先 LookupIP 再逐个检查，最后用已检查过的 IP 直接拨号，
// 避免底层 Dialer 二次解析时被重绑定到不同地址。
func guardedDialContext(timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	dialer := netcheck.MarkedDialer(timeout, logx.Errorf)
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			// 无端口（如 unix 域或纯 IP 场景），按原样回退到标记拨号器
			return dialer.DialContext(ctx, network, addr)
		}

		// 已是 IP 字面量：直接检查后拨号
		if ip := net.ParseIP(host); ip != nil {
			if isBlockedMetadataIP(ip) {
				return nil, fmt.Errorf("订阅地址解析到被拦截的 metadata 地址 %s", ip)
			}
			return dialer.DialContext(ctx, network, addr)
		}

		// 域名：先解析并逐个检查，再用第一个通过检查的 IP 拨号，
		// 避免 Dialer 二次解析时的 DNS 重绑定窗口。
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if isBlockedMetadataIP(ip) {
				return nil, fmt.Errorf("订阅地址 %s 解析到被拦截的 metadata 地址 %s", host, ip)
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("订阅地址 %s 未解析到任何 IP", host)
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
}

// GuardedDialContext 导出 guardedDialContext 供 diagnostics 等外部包复用：
// 返回在 MarkedDialContext 外再包一层 DNS 复验的 DialContext（建连前对解析
// 出的每个 IP 执行 isBlockedMetadataIP），封死 DNS 重绑定绕过 SSRF 防护的通道。
// 网络诊断的直连/代理 client 与订阅拉取共用这一份实现，避免两套防线漂移。
func GuardedDialContext(timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	return guardedDialContext(timeout)
}

// CheckRedirect 导出 checkRedirect 供 diagnostics 等外部包复用：
// 对每一跳目标复用 validateFetchURL 做协议与云 metadata 黑名单校验并限制跳数，
// 防止恶意上游 302 指向 169.254.169.254 等地址绕过初始 URL 校验。
func CheckRedirect(req *http.Request, via []*http.Request) error {
	return checkRedirect(req, via)
}

// ValidateFetchURLExternal 导出校验函数供 diagnostics 等外部包复用。
// 返回 error 表示该 URL 不允许被服务端代发请求（SSRF 黑名单）。
func ValidateFetchURLExternal(rawURL string) error {
	return validateFetchURL(rawURL)
}
