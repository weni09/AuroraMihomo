package protected

import (
	"context"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateCollectionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateCollectionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateCollectionLogic {
	return &UpdateCollectionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateCollectionLogic) UpdateCollection(req *types.CollectionUpdateReq) (resp *types.Collection, err error) {
	c, err := l.svcCtx.Database.GetCollection(req.Id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) != "" {
		c.Name = strings.TrimSpace(req.Name)
	}
	if req.Enabled != nil {
		if *req.Enabled {
			c.Enabled = 1
		} else {
			c.Enabled = 0
		}
	}
	if req.Operators != nil {
		b, _ := jsonMarshal(req.Operators)
		c.Operators = string(b)
	}
	c.UpdatedAt = nowTime()
	if err := l.svcCtx.Database.UpdateCollection(c); err != nil {
		return nil, err
	}
	if req.SubIds != nil {
		if err := l.svcCtx.Database.ReplaceCollectionItems(c.ID, req.SubIds); err != nil {
			return nil, err
		}
	}
	out := toCollectionType(l.svcCtx, *c)
	return &out, nil
}
