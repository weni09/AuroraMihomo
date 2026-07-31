package protected

import (
	"context"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateCollectionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateCollectionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateCollectionLogic {
	return &CreateCollectionLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateCollectionLogic) CreateCollection(req *types.CollectionReq) (resp *types.Collection, err error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errInvalid("组合名称不能为空")
	}
	enabled := 0
	if req.Enabled {
		enabled = 1
	}
	var opsJSON []byte
	if len(req.Operators) > 0 {
		opsJSON, _ = jsonMarshal(req.Operators)
	}
	token, err := randomToken(8)
	if err != nil {
		return nil, err
	}
	c := &model.SubCollection{
		Name: name, Enabled: enabled,
		Operators:  string(opsJSON),
		ShareToken: token,
		CreatedAt:  nowTime(), UpdatedAt: nowTime(),
	}
	if err := l.svcCtx.Database.CreateCollection(c); err != nil {
		return nil, err
	}
	if err := l.svcCtx.Database.ReplaceCollectionItems(c.ID, req.SubIds); err != nil {
		return nil, err
	}
	out := toCollectionType(l.svcCtx, *c)
	return &out, nil
}
