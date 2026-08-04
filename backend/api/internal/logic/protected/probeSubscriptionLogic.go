package protected

import (
	"context"
	"errors"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/internal/service"

	"github.com/zeromicro/go-zero/core/logx"
)

// ProbeSubscriptionReq 探测订阅流量参数的请求。
// 类型定义在本文件而非 goctl 的 types.go：本接口是手挂路由（见
// system.go 的 registerSubscriptionProbeRoute），不参与 goctl 生成，
// 避免下次重新生成时被覆盖或污染生成物。
type ProbeSubscriptionReq struct {
	URL       string `json:"url"`
	UserAgent string `json:"userAgent,optional"`
}

// ProbeSubscriptionResp 探测结果：全部候选 + 最佳组合 URL。
type ProbeSubscriptionResp struct {
	Candidates []service.ProbeCandidate `json:"candidates"`
	BestURL    string                   `json:"bestUrl"`
}

type ProbeSubscriptionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProbeSubscriptionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProbeSubscriptionLogic {
	return &ProbeSubscriptionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ProbeSubscription 对订阅 URL 逐一尝试常见 flag 参数，找出
// 「有流量信息且节点完整」的组合。用于 V2Board 类只在特定参数下
// 下发 subscription-userinfo 响应头的机场。
func (l *ProbeSubscriptionLogic) ProbeSubscription(req *ProbeSubscriptionReq) (*ProbeSubscriptionResp, error) {
	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		return nil, errors.New("订阅地址不能为空")
	}
	candidates, bestURL := l.svcCtx.ConfigService.ProbeSubscriptionParams(l.ctx, rawURL, req.UserAgent)
	return &ProbeSubscriptionResp{Candidates: candidates, BestURL: bestURL}, nil
}
