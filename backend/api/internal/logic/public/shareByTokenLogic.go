package public

import (
	"context"
	"fmt"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type ShareByTokenLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewShareByTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ShareByTokenLogic {
	return &ShareByTokenLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// ShareOutput 是分享端点写给客户端的完整响应
type ShareOutput struct {
	Body        string
	ContentType string
	// UserInfo 为 subscription-userinfo 响应头内容，空串表示不下发
	UserInfo string
}

// ShareRaw 产出可被代理客户端直接消费的纯文本订阅。
// 渲染逻辑统一收敛在 RenderService，与「测试构建」走同一条路径。
func (l *ShareByTokenLogic) ShareRaw(req *types.ShareTokenReq) (*ShareOutput, error) {
	res, err := l.svcCtx.RenderService.RenderByToken(l.ctx, req.Token, req.Target, req.Filter)
	if err != nil {
		return nil, err
	}

	out := &ShareOutput{
		Body:        res.Body,
		ContentType: service.ContentTypeFor(res.Target),
	}
	// 把上游机场的流量信息透传给客户端，Clash Verge / Stash 等据此展示剩余流量
	if t := res.Traffic; t != nil && (t.Total > 0 || t.Expire > 0) {
		out.UserInfo = fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d",
			t.Upload, t.Download, t.Total, t.Expire)
	}
	return out, nil
}
