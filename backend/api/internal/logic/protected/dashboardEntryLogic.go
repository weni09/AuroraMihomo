package protected

import (
	"context"
	"net/url"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

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
// zashboard 支持通过 ?hostname=&port=&secret= 自动配置后端（见其
// URLSearchParams 解析逻辑），因此这里用当前生效配置里的
// external-controller 拼出直连本地内核的链接，用户点开即用，
// 不必自己去填后端地址和密钥。
//
// requestHost 是用户访问管理端时使用的主机名：当 controller 配成
// ":9090" 或 "0.0.0.0:9090" 这类监听地址时，它对浏览器没有意义，
// 需要回落到用户实际访问的主机，否则面板会连不上。
func (l *DashboardEntryLogic) DashboardEntry(requestHost string) (*types.DashboardEntryResp, error) {
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
		if h := hostWithoutPort(requestHost); h != "" {
			host = h
		}
	}
	if host == "" {
		host = "127.0.0.1"
	}

	q := url.Values{}
	q.Set("hostname", host)
	q.Set("port", target.Port)
	if target.Secret != "" {
		q.Set("secret", target.Secret)
	}
	q.Set("label", "AuroraMihomo 本地内核")

	return &types.DashboardEntryResp{
		Available: true,
		Url:       "/ui/?" + q.Encode(),
		Host:      host,
		Port:      target.Port,
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
