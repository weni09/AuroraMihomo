package protected

import (
	"context"
	"fmt"

	"auroramihomo/backend/api/internal/svc"
	"auroramihomo/backend/api/internal/types"
	"auroramihomo/backend/internal/updater"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateCheckLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateCheckLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateCheckLogic {
	return &UpdateCheckLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// UpdateCheck 对比本地已安装版本与 GitHub 上的最新 release，
// 而不只是判断本地文件是否存在——"检查更新"应告知用户是否真的有新版本可用。
func (l *UpdateCheckLogic) UpdateCheck() (resp *types.Result, err error) {
	localVersion, _ := l.svcCtx.MihomoManager.Version(l.ctx)
	// AdGuard 版本探测尚未接入，先传空串：只能判断是否已安装
	mihomoCheck, zashCheck, adguardCheck := l.svcCtx.Updater.CheckLatest(l.ctx, localVersion, "")

	describe := func(name string, c updater.ComponentCheck) string {
		if !c.Present {
			return fmt.Sprintf("%s 未安装", name)
		}
		if c.Error != "" {
			return fmt.Sprintf("%s 已安装，检查最新版本失败: %s", name, c.Error)
		}
		if c.UpdateNeeded {
			return fmt.Sprintf("%s 有新版本可用 (%s)", name, c.LatestVersion)
		}
		return fmt.Sprintf("%s 已是最新", name)
	}

	msg := fmt.Sprintf("%s；%s；%s；自动更新=%v",
		describe("mihomo", mihomoCheck),
		describe("zashboard", zashCheck),
		describe("AdGuardHome", adguardCheck),
		l.svcCtx.Updater.AutoUpdateEnabled())

	// "检查"这个动作本身成功就返回 true —— 组件未安装、有新版本都属于正常的
	// 信息性结果，不该让前端弹红色错误（前端会把 success:false 当操作失败处理）。
	// 只有全部组件的版本查询都失败、完全无法判断状态时才算检查失败。
	success := mihomoCheck.Error == "" || zashCheck.Error == "" || adguardCheck.Error == ""
	return &types.Result{Success: success, Message: msg}, nil
}
