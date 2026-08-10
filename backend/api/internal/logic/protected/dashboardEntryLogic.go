package protected

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/auth"
	"auroramihomo/backend/internal/mihomo"

	"github.com/zeromicro/go-zero/core/logx"
)

type DashboardEntryLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDashboardEntryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DashboardEntryLogic {
	return &DashboardEntryLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// DashboardEntry 返回内置 zashboard 面板的免手填入口地址。
//
// 面板不再直连内核 external-controller（公网场景该地址不可达或裸 http，
// 详见 internal/mihomo/proxy.go 的背景说明），而是经同源反代 /mihomo-api
// 访问内核：这里拼 hostname=<访问面板的主机>&port=<访问面板的端口>&https&
// secondaryPath=/mihomo-api&secret=<内核 secret>。zashboard 拿到后会连
// https://面板主机:端口/mihomo-api/...，API 与 WebSocket 隧道都走这条路径，
// 与面板同源，浏览器不会再有混合内容拦截，也无需开放任何新端口。
//
// requestHost 是用户访问管理端时的请求；当 controller 配成 ":9090" 或
// "0.0.0.0:9090" 这类监听地址时，它对浏览器没有意义，需要回落到用户实际
// 访问的主机，否则面板会连不上。HTTPS 判定复用登录/限流同一套可信代理模型：
// 直连 TLS 或受信代理声明的 X-Forwarded-Proto=https（nginx 部署场景）。
func (l *DashboardEntryLogic) DashboardEntry(r *http.Request) (*types.DashboardEntryResp, error) {
	target, err := l.svcCtx.ConfigService.KernelAPITarget()
	if err != nil {
		return &types.DashboardEntryResp{
			Available: false,
			Message:   err.Error(),
		}, nil
	}

	host := target.Host
	if host == "" || host == "127.0.0.1" || host == "localhost" {
		// 内核只监听本机时，面板必须经由用户当前访问的主机名访问，
		// 否则从其它设备打开面板会连到那台设备自己的 127.0.0.1
		if h := hostWithoutPort(r.Host); h != "" {
			host = h
		}
	}
	if host == "" {
		host = "127.0.0.1"
	}

	// 通过面板自身的端口访问反代：内核 API 与面板同源同端口。
	//
	// HTTPS 判定：
	// 1) 直连 TLS 或受信代理声明的 X-Forwarded-Proto=https（RequestIsHTTPS）
	// 2) Host 无显式端口时，nginx 终结 TLS 且 TrustedProxies 未配齐的常见场景
	//    下，RequestIsHTTPS 会误判为 http→port=80；此时若 X-Forwarded-Proto
	//    明确写了 https，仍采信（即便代理不在白名单）——伪造该头最多让入口
	//    拼成 https://host:443，浏览器仍在用户真实页面协议下访问，风险低于
	//    拼成 :80 导致公网面板全挂。
	// 前端还会用 location.host/protocol 再兜底一次（见 ZashboardView）。
	isHTTPS := auth.RequestIsHTTPS(r, l.svcCtx.Config.TrustedProxies)
	if !isHTTPS && hostHasNoExplicitPort(r.Host) {
		if xf := firstForwardedProto(r.Header.Get("X-Forwarded-Proto")); xf == "https" {
			isHTTPS = true
		}
	}
	publicHost, publicPort := publicAuthority(r.Host, isHTTPS)
	if publicHost != "" {
		host = publicHost
	}

	q := url.Values{}
	q.Set("hostname", host)
	q.Set("port", publicPort)
	if isHTTPS {
		q.Set("https", "")
	}
	q.Set("secondaryPath", mihomo.MihomoAPIPrefix)
	if target.Secret != "" {
		q.Set("secret", target.Secret)
	}
	q.Set("label", "AuroraMihomo 本地内核")

	return &types.DashboardEntryResp{
		Available:  true,
		Url:        "/ui/?" + q.Encode(),
		Host:       host,
		Port:       publicPort,
		PublicPort: publicPort,
	}, nil
}

// hostWithoutPort 去掉 Host 头里的端口，兼容 IPv6 的 [::1]:8899 写法
func hostWithoutPort(hostHeader string) string {
	h := strings.TrimSpace(hostHeader)
	if h == "" {
		return ""
	}
	if strings.HasPrefix(h, "[") {
		if i := strings.Index(h, "]"); i > 0 {
			return h[:i+1]
		}
		return h
	}
	if i := strings.LastIndex(h, ":"); i > 0 {
		return h[:i]
	}
	return h
}

// publicAuthority 从 Host 头拆出浏览器访问面板用的主机与端口。
// 无显式端口时按当前协议取隐含端口：http→80、https→443。
// 返回的端口总是可用的字符串，供 zashboard 直接拼 URL。
func publicAuthority(hostHeader string, isHTTPS bool) (host, port string) {
	h := strings.TrimSpace(hostHeader)
	defaultPort := "80"
	if isHTTPS {
		defaultPort = "443"
	}
	if h == "" {
		return "", defaultPort
	}
	if strings.HasPrefix(h, "[") {
		if i := strings.Index(h, "]"); i > 0 {
			host = h[:i+1]
			if rest := h[i+1:]; strings.HasPrefix(rest, ":") {
				if p, err := strconv.Atoi(strings.TrimPrefix(rest, ":")); err == nil && p > 0 && p <= 65535 {
					return host, strconv.Itoa(p)
				}
			}
			return host, defaultPort
		}
		return "", defaultPort
	}
	if i := strings.LastIndex(h, ":"); i > 0 {
		portStr := h[i+1:]
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 && p <= 65535 {
			return h[:i], portStr
		}
	}
	return h, defaultPort
}

// hostHasNoExplicitPort 判断 Host 头是否省略了端口（浏览器访问 80/443 时常见）。
func hostHasNoExplicitPort(hostHeader string) bool {
	h := strings.TrimSpace(hostHeader)
	if h == "" {
		return true
	}
	if strings.HasPrefix(h, "[") {
		i := strings.Index(h, "]")
		if i < 0 {
			return true
		}
		return !strings.HasPrefix(h[i+1:], ":")
	}
	// 含冒号且最后一段是纯数字 → 显式端口；否则视为无端口
	if i := strings.LastIndex(h, ":"); i > 0 {
		_, err := strconv.Atoi(h[i+1:])
		return err != nil
	}
	return true
}

// firstForwardedProto 取 X-Forwarded-Proto 最左侧值并小写化。
func firstForwardedProto(raw string) string {
	proto := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.Index(proto, ","); i >= 0 {
		proto = strings.TrimSpace(proto[:i])
	}
	return proto
}
