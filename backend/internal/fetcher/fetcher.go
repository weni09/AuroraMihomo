package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"auroramihomo/backend/internal/netcheck"

	"github.com/zeromicro/go-zero/core/logx"
)

// Client downloads remote subscription content.
type Client struct {
	httpClient *http.Client
	userAgent  string
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

	resp, err := c.httpClient.Do(req)
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
