package fetcher

import (
	"context"
	"fmt"
	"github.com/zeromicro/go-zero/core/logx"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
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
		httpClient: &http.Client{Timeout: timeout},
		userAgent:  "AuroraMihomo/0.1",
	}
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
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, UserInfo{}, fmt.Errorf("subscription url is empty")
	}

	// 只允许 http/https。订阅地址由用户提供、由服务端发起请求，
	// 限制协议可挡掉 file:// 读本地文件、gopher:// 等协议走私手法。
	// 注意：这里不做内网地址封禁 —— 自建订阅/局域网机场是常见正当用法，
	// 强行封禁会破坏功能；添加订阅本身需要登录，风险边界在鉴权上。
	if u, perr := url.Parse(rawURL); perr != nil {
		return nil, UserInfo{}, fmt.Errorf("订阅地址无法解析: %w", perr)
	} else if sc := strings.ToLower(u.Scheme); sc != "http" && sc != "https" {
		return nil, UserInfo{}, fmt.Errorf("订阅地址仅支持 http/https，当前为 %q", u.Scheme)
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
	return data, info, nil
}
