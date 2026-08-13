package mihomo

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"auroramihomo/backend/internal/auth"

	"github.com/zeromicro/go-zero/core/logx"
)

// 同源反代前缀。zashboard 通过 secondaryPath=/mihomo-api 把 API 与 WebSocket
// 都指到这里，再由本反代转发到内核 external-controller（见 dashboardEntryLogic）。
const MihomoAPIPrefix = "/mihomo-api"

// kernelAPICacheTTL 内核只读接口的内存缓存时长。
// 代理页首次打开会拉全量 /proxies，用户在页面间快速往返（切分组、返回、
// 进详情再回来）会重复拉同一份大 JSON。2s 短缓存让这些往返命中内存，
// 不重复请求内核；2s 足够短，节点延迟/状态的更新频率远高于此，
// 不会让用户看到明显过期的数据。敏感数据只留内存、不落盘。
const kernelAPICacheTTL = 2 * time.Second

// kernelAPICacheMaxBody 缓存 body 上限：/proxies 全量 JSON 可能上百 KB，
// 防恶意超大响应撑爆内存。
const kernelAPICacheMaxBody = 8 << 20 // 8MB

// kernelCacheEntry 一条内核只读接口的缓存。
type kernelCacheEntry struct {
	body []byte
	ct   string
	at   time.Time
}

// kernelAPICache 以 method+path 为键的短 TTL 缓存。
// 并发安全：sync.Map + 写入时带时间戳，读取时校验过期。
var kernelAPICache sync.Map

// kernelAPICacheable 判断内核接口是否可短缓存。
// 只缓存只读 GET、非 WebSocket 的数据接口：/version /proxies(/名字) /
// providers /configs 都是 zashboard 代理页会重复拉的全量只读数据。
// /traffic /connections /logs 是 WS 隧道（Upgrade），且是持续流，绝不缓存。
func kernelAPICacheable(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	// WebSocket 隧道（Upgrade 头）是双向流，缓存无意义且会破坏握手
	if strings.Contains(strings.ToLower(r.Header.Get("Upgrade")), "websocket") {
		return false
	}
	path := r.URL.Path
	// 去掉 /mihomo-api 前缀按内核路径判断
	p := strings.TrimPrefix(path, MihomoAPIPrefix)
	if p == "" {
		p = "/"
	}
	switch {
	case p == "/version", p == "/configs", p == "/proxies", p == "/providers":
		return true
	case strings.HasPrefix(p, "/proxies/"), strings.HasPrefix(p, "/providers/"):
		return true
	}
	return false
}

// LocalDialTarget 把 external-controller 的监听主机归一为反代可 dial 的上游主机
// （不含端口、不含 IPv6 方括号）。空 / 0.0.0.0 / [::] / ::（监听所有网卡）
// → 127.0.0.1。调用方再用 net.JoinHostPort 拼地址，避免 IPv6 双重括号。
// 与 adguard.LocalProxyUpstream 的归一语义一致：绑定单网卡 IP 时保持原值。
func LocalDialTarget(host string) string {
	host = strings.TrimSpace(host)
	bare := strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "" || bare == "0.0.0.0" || bare == "::" {
		return "127.0.0.1"
	}
	return bare
}

// NewKernelAPIProxyHandler 返回挂在 /mihomo-api 下的内核 external-controller 同源反代。
//
// 背景：zashboard 面板默认直连「external-controller 地址」，公网部署时该地址
// 要么不可达（内核只绑 127.0.0.1）、要么裸奔在 http 端口（被浏览器按混合内容
// 拦截），nginx 也帮不上忙——面板的请求根本不经过它。这里仿 /adguard-ui 的
// 做法把内核 API 收进面板同源路径：浏览器访问 https://面板/mihomo-api/...，
// nginx 与面板的 443 一并处理，无需再开放任何新端口。
//
// 鉴权：iframe 内嵌页面无法像 fetch 那样带 Authorization 头，但同源请求会
// 自动附带 aurora_session cookie，因此与 /adguard-ui 共用 auth.AuthorizeRequest。
// 面板的 WebSocket 隧道（/connections、/traffic、/logs）同样要过这道鉴权。
//
// secret 注入：mihomo external-controller 与本仓库 manager.go、zashboard 一致，
// 使用 Authorization: Bearer <secret>；WebSocket 另认 query token=。
// 这里统一注入两者：覆盖客户端可能带来的凭据，避免面板把错误形态
// （Basic / 空）传到内核导致半残（HTTP 401 但 WS 偶发仍通）。
//
// targetResolver 返回内核对外监听地址（"127.0.0.1:9090" 等），调用方负责归一
// 通配监听；返回空串表示内核未启用外部控制，一律 503。
func NewKernelAPIProxyHandler(jwtSecret string, ver *auth.PasswordVer, targetResolver func() (addr, secret string), trustedProxies []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.AuthorizeRequest(r, jwtSecret, ver) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		addr, kernelSecret := "", ""
		if targetResolver != nil {
			addr, kernelSecret = targetResolver()
		}
		addr = strings.TrimSpace(addr)
		if addr == "" {
			http.Error(w, "mihomo external-controller unavailable", http.StatusServiceUnavailable)
			return
		}

		// 只读接口短缓存：代理页快速往返重复拉 /proxies 全量 JSON 时，
		// 命中内存直接回，不请求内核。缓存 key 用原始路径（含 /mihomo-api
		// 前缀）——Director 会改写 path，ModifyResponse 拿不到原始值。
		if kernelAPICacheable(r) {
			cacheKey := r.Method + " " + r.URL.Path
			if v, ok := kernelAPICache.Load(cacheKey); ok {
				if e, ok := v.(*kernelCacheEntry); ok && time.Since(e.at) < kernelAPICacheTTL {
					w.Header().Set("Content-Type", e.ct)
					w.Header().Set("Cache-Control", "private, max-age=2")
					_, _ = w.Write(e.body)
					return
				}
			}
		}
		// 上游白名单：回环 / 本机接口 IP / 私网地址（分机部署的内核常配置成
		// 其它机器的私网 IP）。域名与公网 IP 拒绝，防配置被污染成 SSRF 跳板。
		if host, _, err := net.SplitHostPort(addr); err == nil && !auth.IsAllowedKernelUpstream(host) {
			logx.Errorf("mihomo-api proxy rejected upstream %q (path=%s): not local/private", addr, r.URL.Path)
			http.Error(w, "upstream must be a local or private IP address", http.StatusBadGateway)
			return
		}

		target, err := url.Parse("http://" + addr)
		if err != nil || target.Host == "" {
			logx.Errorf("mihomo-api proxy invalid upstream %q (path=%s): %v", addr, r.URL.Path, err)
			http.Error(w, "invalid upstream", http.StatusBadGateway)
			return
		}

		// 内核没跑时端口无人监听，快速失败而不是等默认 30s 连接超时。
		// 该秒数远小于面板 API 轮询间隔，不会在正常重启期间刷出报错页。
		transport := &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   3 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		}

		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.Transport = transport
		proxy.FlushInterval = -1
		proxy.Director = func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			req.URL.Path = strings.TrimPrefix(req.URL.Path, MihomoAPIPrefix)
			if req.URL.Path == "" {
				req.URL.Path = "/"
			}
			req.URL.RawPath = ""
			req.Header.Del("Origin")

			// 清除客户端带来的 Authorization，再注入与 manager.go / zashboard
			// 一致的 Bearer。WS 路径（/traffic、/connections、/logs）主要靠
			// query token= 鉴权，这里同步覆盖，避免面板侧旧记录缺 password
			// 时隧道握手 401。
			req.Header.Del("Authorization")
			if kernelSecret != "" {
				req.Header.Set("Authorization", "Bearer "+kernelSecret)
				q := req.URL.Query()
				q.Set("token", kernelSecret)
				req.URL.RawQuery = q.Encode()
			}

			if clientIP, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				req.Header.Set("X-Real-IP", clientIP)
				if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
					req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
				} else {
					req.Header.Set("X-Forwarded-For", clientIP)
				}
			}
			proto := "http"
			if r.TLS != nil {
				proto = "https"
			} else if auth.RequestIsHTTPS(r, trustedProxies) {
				proto = "https"
			}
			req.Header.Set("X-Forwarded-Proto", proto)
			if host := r.Host; host != "" {
				req.Header.Set("X-Forwarded-Host", host)
			}
			req.Header.Set("X-Forwarded-Prefix", MihomoAPIPrefix)
		}
		proxy.ModifyResponse = func(resp *http.Response) error {
			// 面板同源内嵌，不允许上游写入可能覆盖面板自己的 cookie
			resp.Header.Del("Set-Cookie")
			// 可缓存接口：写内存缓存 + 给浏览器 private 短缓存，代理页快速
			// 往返（切分组/返回）不重拉全量。其余接口仍 no-store——
			// 内核接口含代理列表/连接等敏感数据，禁止磁盘缓存。
			if kernelAPICacheable(r) {
				cacheKey := r.Method + " " + r.URL.Path
				body, err := io.ReadAll(io.LimitReader(resp.Body, kernelAPICacheMaxBody))
				if err == nil {
					resp.Body = io.NopCloser(bytes.NewReader(body))
					kernelAPICache.Store(cacheKey, &kernelCacheEntry{
						body: body,
						ct:   resp.Header.Get("Content-Type"),
						at:   time.Now(),
					})
				}
				resp.Header.Set("Cache-Control", "private, max-age=2")
				return nil
			}
			resp.Header.Set("Cache-Control", "no-store")
			return nil
		}
		proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
			// 上游 dial / 请求失败：打日志便于排查「内核没跑 / 端口不对 / 私网不通」。
			logx.Errorf("mihomo-api proxy upstream error (path=%s): %v", req.URL.Path, err)
			http.Error(rw, "bad gateway", http.StatusBadGateway)
		}

		// 面板的 WebSocket 隧道（/traffic、/connections、/logs）与普通 API
		// 共用同一路径前缀，nginx 的 Upgrade 头已经原样穿过到本进程。
		// ReverseProxy 对 Upgrade 请求会自动退化为「双向字节流复制」的隧道
		// 模式，但上游连接的 http.Server 级超时（Read/WriteTimeout）会残留为
		// 绝对 deadline，几十秒就把这条长活隧道掐断（/ws 的处理方式同此，
		// 见 aurora.go registerWebSocket）。用连接感知的 ResponseWriter 在
		// hijack 后清掉 deadline，让长连接按对端关闭自然结束。
		var flusher http.Flusher
		if f, ok := w.(http.Flusher); ok {
			flusher = f
		}
		connAware := &connAwareWriter{w: w, flusher: flusher}
		proxy.ServeHTTP(connAware, r)
	})
}

// connAwareWriter 包装 ResponseWriter：拿到底层连接后立刻清除该连接上的
// 绝对 deadline，避免 http.Server 的连接级超时掐断被 hijack 的长连接
// （WebSocket 隧道）。仅在其 Hijack 能力暴露后生效；普通响应照常转发。
type connAwareWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	once    sync.Once
}

func (c *connAwareWriter) Header() http.Header         { return c.w.Header() }
func (c *connAwareWriter) WriteHeader(code int)        { c.w.WriteHeader(code) }
func (c *connAwareWriter) Write(b []byte) (int, error) { return c.w.Write(b) }
func (c *connAwareWriter) Flush() {
	if c.flusher != nil {
		c.flusher.Flush()
	}
}
func (c *connAwareWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := c.w.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, nil, err
	}
	c.once.Do(func() {
		// 清除绝对 deadline：反代把这条连接交给上游对等方后，生命周期由
		// 双方决定，不能再被 http.Server 的读写超时按秒掐断。
		_ = conn.SetDeadline(time.Time{})
	})
	return conn, rw, nil
}

var _ http.Hijacker = (*connAwareWriter)(nil)
var _ http.Flusher = (*connAwareWriter)(nil)
