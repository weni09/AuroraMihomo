package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"auroramihomo/backend/internal/netcheck"

	"github.com/zeromicro/go-zero/core/logx"
)

// rawOfficialHost 是 raw 内容的官方主机，识别「需要加速」的链接。
const rawOfficialHost = "raw.githubusercontent.com"

// Client downloads remote subscription content.
type Client struct {
	httpClient *http.Client
	userAgent  string

	// mu 保护运行期可变的字段：rawProviderFunc / rawSuccess / proxyURLFn。
	// 设置保存或启动装载时会写入，而拉取是并发的，无锁读写是 data race。
	mu sync.RWMutex
	// rawProviderFunc 返回当前应使用的 GitHub 下载源列表（已优先化，官方源兜底）。
	// 与 Release 下载共用同一份 CDN 配置：由 service 层注入 updater 的
	// PrioritizedCDNProviders，raw 拉取每次现查，last 变化即时生效。
	rawProviderFunc func() []string
	// rawSuccess 记录一次成功的下载源（落库 last 优先序）；由 service 层注入
	// updater.RememberCDNSuccess。默认空操作。
	rawSuccess func(string)
	// proxyURLFn 返回本地 mihomo 的 HTTP 代理地址（如 http://127.0.0.1:7890）。
	// 由 service 层注入；raw 官方链接拉取时优先经它直取官方，失败再回落镜像。
	proxyURLFn func() string
}

func New(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		httpClient: &http.Client{
			Timeout: timeout,
			// 重定向逐跳校验 + 限跳数，防 302 绕过 validateFetchURL
			// （详见 ssrf.go 的 checkRedirect）。
			CheckRedirect: checkRedirect,
			// 给订阅拉取的连接打上面板专用 fwmark，使其在透明代理 TProxy
			// 模式下不被 mihomo 自己接管（理由见 netcheck.MarkedDialer）。
			// 非 Linux 平台上打标是空操作，行为与改动前一致。
			Transport: &http.Transport{
				DialContext: netcheck.MarkedDialContext(dialTimeout, logx.Errorf),
			},
		},
		// 默认 UA 用 ClashMeta/2.0 而非自定义 UA：V2Board 类机场按 UA 决定
		// 是否下发 subscription-userinfo 头与返回格式——UA 含 "clash" 才给
		// 流量信息（实测 ClashMeta 系返回完整 YAML+userinfo；AuroraMihomo/
		// v2rayN 等未知 UA 只给 base64 且无 userinfo）。选 ClashMeta 而非
		// ClashforWindows：部分机场禁用了纯 clash 输出，CFW UA 会拿到
		// 「当前Clash客户端不支持本机场协议」占位节点，meta 系 UA 无此问题。
		// 用户显式配置的 UA（订阅的 UserAgent 字段）始终优先于此处默认值。
		userAgent: "ClashMeta/2.0",
	}
}

// dialTimeout 单次 TCP 建连的超时。
// 比整体 Timeout 短很多：机场地址不可达时应尽快失败，而不是把整个
// 刷新流程卡在一个连不上的订阅上。
const dialTimeout = 10 * time.Second

// SetRawCDNProviderFunc 注入 raw 拉取用的下载源查询函数。
// 返回的列表应已优先化（上次成功源在前）且官方源兜底——由调用方
// （updater.PrioritizedCDNProviders）保证；nil 表示不加速、直连官方。
func (c *Client) SetRawCDNProviderFunc(fn func() []string) {
	c.mu.Lock()
	c.rawProviderFunc = fn
	c.mu.Unlock()
}

// rawProviders 现查当前应使用的下载源列表；未注入时返回 nil（不加速）。
func (c *Client) rawProviders() []string {
	c.mu.RLock()
	fn := c.rawProviderFunc
	c.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn()
}

// SetRawSuccessCallback 注入下载源成功回调（记入全局 last 优先序）。
func (c *Client) SetRawSuccessCallback(fn func(string)) {
	if fn == nil {
		fn = func(string) {}
	}
	c.mu.Lock()
	c.rawSuccess = fn
	c.mu.Unlock()
}

// SetProxyURLFunc 注入本地 mihomo 代理地址的查询回调。
func (c *Client) SetProxyURLFunc(fn func() string) {
	c.mu.Lock()
	c.proxyURLFn = fn
	c.mu.Unlock()
}

// proxyURL 返回当前可用的 mihomo 代理地址，不可用时为空串。
func (c *Client) proxyURL() string {
	c.mu.RLock()
	fn := c.proxyURLFn
	c.mu.RUnlock()
	if fn == nil {
		return ""
	}
	return strings.TrimSpace(fn())
}

// proxyClient 返回走 mihomo 代理的客户端；代理不可用时返回直连客户端与空串。
// 每次调用都重新判断：内核可能在运行期被启停。保留 CheckRedirect，
// 否则经代理路径会丢掉 SSRF 重定向校验。
func (c *Client) proxyClient() (*http.Client, string) {
	proxy := c.proxyURL()
	if proxy == "" {
		return c.httpClient, ""
	}
	u, err := url.Parse(proxy)
	if err != nil || u.Host == "" {
		logx.Errorf("mihomo 代理地址无法解析，改为直连: %q", proxy)
		return c.httpClient, ""
	}
	return &http.Client{
		Timeout:       c.httpClient.Timeout,
		CheckRedirect: c.httpClient.CheckRedirect,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(u),
			// 与直连路径一样打 fwmark：到 mihomo 混合端口是本地回环连接，
			// 本不会被 TPROXY 抓，但两条路径同构可避免「改了一处漏了一处」。
			DialContext: netcheck.MarkedDialContext(dialTimeout, logx.Errorf),
		},
	}, proxy
}

func (c *Client) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	return c.FetchWithUA(ctx, rawURL, "")
}

func (c *Client) FetchWithUA(ctx context.Context, rawURL, userAgent string) ([]byte, error) {
	data, _, err := c.FetchWithMeta(ctx, rawURL, userAgent)
	return data, err
}

// FetchWithMeta 在下载内容的同时解析 subscription-userinfo 响应头，
// 机场普遍用它下发已用流量与到期时间。
func (c *Client) FetchWithMeta(ctx context.Context, rawURL, userAgent string) ([]byte, UserInfo, error) {
	// GitHub 网页链接 → raw 直链：用户常从浏览器地址栏粘贴 /blob/ 页面，
	// 那会下到整页 HTML，模板/订阅解析必失败；在真正请求前改写。
	rawURL = normalizeFetchURL(rawURL)

	// 协议限制 + 云 metadata / link-local 黑名单。
	// RFC1918 内网故意放行：自建订阅与局域网机场是正当用法；
	// 添加订阅需登录，风险边界主要在鉴权，但仍挡掉可读实例身份令牌的地址。
	if err := validateFetchURL(rawURL); err != nil {
		return nil, UserInfo{}, err
	}

	// raw 官方链接且配置了加速源时，走「代理优先 + 镜像轮换」；否则单源直拉。
	// 普通订阅 URL 不加速也不代理：机场地址本就走 fwmark 直连，
	// 强行套本地代理会改变既有出网路径。
	if isRawOfficial(rawURL) && len(c.rawProviders()) > 0 {
		return c.fetchWithRawCDN(ctx, rawURL, userAgent)
	}
	return c.fetchSingle(ctx, rawURL, userAgent)
}

// fetchSingle 用默认直连客户端拉取。
func (c *Client) fetchSingle(ctx context.Context, rawURL, userAgent string) ([]byte, UserInfo, error) {
	return c.fetchSingleWithClient(ctx, rawURL, userAgent, c.httpClient)
}

// fetchSingleWithClient 单次完整拉取：请求、校验状态码与 HTML、返回正文与 userinfo。
// 是 fetchWithRawCDN 的每个候选源（代理路径或镜像路径）的拉取单元。
func (c *Client) fetchSingleWithClient(ctx context.Context, rawURL, userAgent string, client *http.Client) ([]byte, UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, UserInfo{}, fmt.Errorf("invalid subscription url: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		req.Header.Set("User-Agent", c.userAgent)
	}
	req.Header.Set("Accept", "text/plain, text/yaml, application/yaml, application/octet-stream, */*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, UserInfo{}, fmt.Errorf("fetch subscription failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		// 上游响应体可能含账号信息，且该错误会被存入 error_message 回显给前端，
		// 因此只暴露状态码，响应体仅在 debug 级日志中保留
		logx.Debugf("订阅拉取失败 status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
		return nil, UserInfo{}, fmt.Errorf("订阅拉取失败，上游返回状态码 %d", resp.StatusCode)
	}

	info := ParseUserInfo(resp.Header.Get("subscription-userinfo"))

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8MB
	if err != nil {
		return nil, info, fmt.Errorf("read subscription body failed: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, info, fmt.Errorf("subscription body is empty")
	}
	// 仍拿到 HTML 时（私有仓库登录页、未改写的网页链）直接拒掉：
	// 否则 YAML/模板解析会给出难懂的语法错误，掩盖真正原因。
	if looksLikeHTMLPage(resp.Header.Get("Content-Type"), data) {
		return nil, info, fmt.Errorf("远程地址返回的是网页而非配置正文，请改用 raw 直链（GitHub 可用 raw.githubusercontent.com）")
	}
	return data, info, nil
}

// isRawOfficial 判断 URL 主机是否为 raw.githubusercontent.com。
// 仅对该主机的链接应用加速；其它主机按原样单源直拉。
func isRawOfficial(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.ToLower(u.Hostname()) == rawOfficialHost
}

// shouldRememberRaw 判断某个 raw 源成功时是否值得记录为「上次成功源」。
// 官方源（github/official/raw 官方主机）成功不记录：记录的语义是
// 「镜像优先」，直连官方不需要下次优先。
func shouldRememberRaw(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "github", "official", rawOfficialHost:
		return false
	}
	return true
}

// fetchWithRawCDN 拉取 raw 官方链接：先经 mihomo 代理直取官方地址，
// 失败再按下载源列表轮换——与 updater 的 Release 下载规则同构。
// 代理与官方源成功都不记 last（直连官方无需下次优先）；
// 镜像成功记入全局 last 优先序（与 Release 下载共用）。
func (c *Client) fetchWithRawCDN(ctx context.Context, rawURL, userAgent string) ([]byte, UserInfo, error) {
	var errs []string

	// 1) 代理优先：内核在跑时走它通常比第三方镜像更快，且拿到官方原始文件。
	if client, proxy := c.proxyClient(); proxy != "" {
		data, info, err := c.fetchSingleWithClient(ctx, rawURL, userAgent, client)
		if err == nil {
			return data, info, nil
		}
		errs = append(errs, fmt.Sprintf("mihomo 代理(%s) => %v", proxy, err))
	}

	// 2) 镜像轮换：按列表顺序尝试，官方源（github）作最后兜底。
	for _, p := range c.rawProviders() {
		u := rawCDNURLFor(rawURL, p)
		if u == "" {
			continue
		}
		data, info, err := c.fetchSingle(ctx, u, userAgent)
		if err != nil {
			// 只拼源标识不拼完整 URL：订阅 URL 可能带 token 等凭据参数，
			// 该错误会随 error_message 回显给前端
			errs = append(errs, fmt.Sprintf("%s => %v", p, err))
			continue
		}
		if shouldRememberRaw(p) {
			c.rememberRawSuccess(p)
		}
		return data, info, nil
	}
	return nil, UserInfo{}, fmt.Errorf("all raw sources failed: %s", strings.Join(errs, " | "))
}

// rawCDNURLFor 把单个 raw 加速源展开成可请求的 URL；无法识别时返回空串。
// 与 updater 的 Release CDN 展开同构：官方源原样、含 %s 模板替换、
// 完整前缀拼接、裸域名忽略、jsdelivr 跳过。
func rawCDNURLFor(official, provider string) string {
	switch strings.ToLower(provider) {
	case "github", "official", "raw.githubusercontent.com":
		return official
	case "ghproxy.com":
		return "https://ghproxy.com/" + official
	case "mirror.ghproxy.com":
		return "https://mirror.ghproxy.com/" + official
	case "gh.llkk.cc":
		return "https://gh.llkk.cc/" + official
	case "ghproxy.net":
		return "https://ghproxy.net/" + official
	case "gh.ddlc.top":
		return "https://gh.ddlc.top/" + official
	case "gitdl.cn":
		return "https://gitdl.cn/" + official
	case "ghp.ci":
		return "https://ghp.ci/" + official
	default:
		if strings.Contains(provider, "%s") {
			return fmt.Sprintf(provider, official)
		}
		// jsdelivr 只镜像仓库内文件、代理不了 raw 路径，跳过
		if isJsdelivrHost(provider) {
			return ""
		}
		if strings.HasPrefix(provider, "http://") || strings.HasPrefix(provider, "https://") {
			if strings.HasSuffix(provider, "/") {
				return provider + official
			}
			return provider + "/" + official
		}
		return ""
	}
}

// jsdelivrHosts 列出已知的 jsdelivr 镜像域名，raw 加速填进来会被跳过。
var jsdelivrHosts = []string{"jsdelivr.net", "jsdelivr.com"}

// isJsdelivrHost 判断一个源是否为 jsdelivr 镜像（只看域名部分）。
func isJsdelivrHost(provider string) bool {
	p := strings.ToLower(strings.TrimSpace(provider))
	p = strings.TrimPrefix(strings.TrimPrefix(p, "https://"), "http://")
	if i := strings.IndexAny(p, "/?#"); i >= 0 {
		p = p[:i]
	}
	for _, h := range jsdelivrHosts {
		if p == h || strings.HasSuffix(p, "."+h) {
			return true
		}
	}
	return false
}

// rememberRawSuccess 记下本次成功的源（进程内优先化用）并触发注入的回调（落库）。
// 官方源成功不会走到这里（shouldRememberRaw 已过滤）。
// rememberRawSuccess 触发注入的回调，把本次成功的镜像记入全局 last 优先序
// （updater.RememberCDNSuccess 落库）。官方源成功不会走到这里。
func (c *Client) rememberRawSuccess(provider string) {
	c.mu.RLock()
	fn := c.rawSuccess
	c.mu.RUnlock()
	if fn != nil {
		fn(strings.TrimSpace(provider))
	}
}
