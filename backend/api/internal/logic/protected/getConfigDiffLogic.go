package protected

import (
	"context"
	"encoding/json"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetConfigDiffLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetConfigDiffLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetConfigDiffLogic {
	return &GetConfigDiffLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetConfigDiffLogic) GetConfigDiff() (resp *types.DiffReport, err error) {
	d := l.svcCtx.ConfigService.GetLastDiff()
	conv := func(kind, name string, from, to any) types.DiffItem {
		fb, _ := json.Marshal(from)
		tb, _ := json.Marshal(to)
		return types.DiffItem{Kind: kind, Name: name, From: string(fb), To: string(tb)}
	}
	resp = &types.DiffReport{
		Added:   []types.DiffItem{},
		Removed: []types.DiffItem{},
		Changed: []types.DiffItem{},
	}
	for _, x := range d.Added {
		resp.Added = append(resp.Added, conv(x.Kind, x.Name, x.From, x.To))
	}
	for _, x := range d.Removed {
		resp.Removed = append(resp.Removed, conv(x.Kind, x.Name, x.From, x.To))
	}
	for _, x := range d.Changed {
		resp.Changed = append(resp.Changed, conv(x.Kind, x.Name, x.From, x.To))
	}
	return resp, nil
}
