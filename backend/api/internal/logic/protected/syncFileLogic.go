package protected

import (
	"context"
	"fmt"
	"strings"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/model"
	"auroramihomo/backend/internal/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type SyncFileLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSyncFileLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SyncFileLogic {
	return &SyncFileLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

// SyncFile 把远程地址的内容拉下来固化进本地正文。
//
// 只固化远程侧：本动作的语义是「把远程内容变成本地内容」，
// 若按 mergeSources 把本地正文也一起写回，本地内容会在每次同步后
// 自我叠加一份，越同步越长。
func (l *SyncFileLogic) SyncFile(req *types.IdPathReq) (*types.SubFile, error) {
	f, err := l.svcCtx.Database.GetFile(req.Id)
	if err != nil {
		return nil, err
	}
	if len(service.SplitFileURLs(f.SyncURL)) == 0 {
		return nil, fmt.Errorf("文件 %s 未配置远程地址", f.Name)
	}

	// 用副本强制成「仅远程、不合并」，不改动库里的实际配置
	remoteOnly := *f
	remoteOnly.SourceMode = model.FileSourceRemote
	remoteOnly.MergeSources = model.FileMergeNone

	res, err := l.svcCtx.RenderService.FileContent().Resolve(l.ctx, &remoteOnly)
	if err != nil {
		return nil, fmt.Errorf("拉取远程内容失败: %w", err)
	}
	if strings.TrimSpace(res.Content) == "" {
		// 空内容多半是上游异常。直接写回会静默清空用户的文件，
		// 而这一步没有撤销入口。
		return nil, fmt.Errorf("远程内容为空，已放弃覆盖本地正文")
	}

	f.Content = res.Content
	if err := l.svcCtx.Database.SaveFile(f); err != nil {
		return nil, err
	}
	// 正文变了必须让渲染缓存失效，否则同步后访问直链仍是旧内容
	l.svcCtx.RenderService.InvalidateRenderCache()

	for _, w := range res.Warnings {
		l.Errorf("同步文件 %s：%s", f.Name, w)
	}
	out := toFileType(*f)
	return &out, nil
}
