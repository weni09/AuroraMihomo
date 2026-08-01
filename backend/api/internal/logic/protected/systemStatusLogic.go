package protected

import (
	"context"
	"os"
	"strings"
	"time"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/version"

	"github.com/zeromicro/go-zero/core/logx"
)

// SystemStatusLogic 处理系统状态获取逻辑
type SystemStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewSystemStatusLogic 创建系统状态逻辑实例
func NewSystemStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SystemStatusLogic {
	return &SystemStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// SystemStatus 返回当前系统及 Mihomo 服务状态
func (l *SystemStatusLogic) SystemStatus() (resp *types.Status, err error) {
	_, _ = l.svcCtx.MihomoManager.Version(l.ctx)
	st := l.svcCtx.MihomoManager.Status()
	state := "stopped"
	if st.IsRunning {
		state = "running"
	}
	now := time.Now()
	return &types.Status{
		Status:     state,
		Version:    st.Version,
		AppVersion: version.Get(),
		Pid:        st.PID,
		ServerTime: now.Format(time.RFC3339),
		Timezone:   hostTimezoneName(),
	}, nil
}

// hostTimezoneName 尽量返回 IANA 名（Asia/Shanghai），便于控制台展示。
//
// 优先读 /etc/timezone（Debian/Alpine 常见）；否则用 time.Local.String()
// （正确装了 zoneinfo 时多为 IANA，否则可能是 "Local"）。
func hostTimezoneName() string {
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	// 部分系统 localtime 是 zoneinfo 软链，从路径尾部取 IANA 名
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		const marker = "/zoneinfo/"
		if i := strings.Index(link, marker); i >= 0 {
			if s := strings.TrimSpace(link[i+len(marker):]); s != "" {
				return s
			}
		}
	}
	if name := time.Local.String(); name != "" {
		return name
	}
	return "Local"
}
