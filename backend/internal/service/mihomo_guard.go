package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"auroramihomo/backend/internal/mihomo"
	"auroramihomo/backend/internal/repository"

	"github.com/zeromicro/go-zero/core/logx"
)

// settingMihomoBoot 内核「期望运行」状态（settings 键）。
// 未设置（首次启动/老版本升级）默认 true：内核是网关/旁路由的代理核心，
// 保持现状「面板启动自动拉起、异常退出自动恢复」；用户手动 Stop 会写 false，
// 此后面板重启也不自动拉，直到手动 Start 或重新开启守护。
const settingMihomoBoot = "mihomo.enabled_at_boot"

// mihomoGuardDesiredDefaults 未设置期望键时的默认值。
const mihomoGuardDesiredDefaults = true

// MihomoGuard 承担 mihomo 内核的「期望运行」守护。
//
// 为什么需要它：内核一旦异常退出（崩溃、被外部 kill），TProxy 规则仍在
// 宿主上，流量会进无人监听的端口——全面断网。面板作为代理控制面应该
// 检测到停止并自动拉起，但受两个约束：
//   - 用户手动 Stop 后不得再拉（尊重「期望运行」）；
//   - 启动接管（自升级后 Attach 旧内核 / 正常 Start）完成前不得动作，
//     否则启动早期的状态探测会把「正在被接管的内核」误判为已死再拉一个。
//
// 与 AdGuardService 的 enabled_at_boot 同一套 settings 模型，行为多平台一致
// （Alpine/OpenRC、Debian/systemd、Windows 都由面板子进程托管内核，无服务模式）。
type MihomoGuard struct {
	db     *repository.Database
	mgr    mihomo.Manager
	logger logx.Logger

	// mu 保护以下字段；db 写入在锁外做（避免持锁写库阻塞状态查询）。
	mu sync.Mutex
	// armed 启动接管流程完成后由 main 置位；置位前 Guard 不动作。
	armed bool
	// attempts 近 guardWindow 内的启动尝试时间戳（滑动窗口限次）。
	attempts []time.Time
}

// NewMihomoGuard 构造内核守护。mgr 为 mihomo 进程管理器。
func NewMihomoGuard(db *repository.Database, mgr mihomo.Manager) *MihomoGuard {
	return &MihomoGuard{
		db:     db,
		mgr:    mgr,
		logger: logx.WithContext(context.Background()),
	}
}

// DesiredRunning 读取用户期望的运行态（settings: mihomo.enabled_at_boot）。
// 未设置（首次启动/老版本升级）默认 true。
func (g *MihomoGuard) DesiredRunning() bool {
	v, err := g.db.GetSetting(settingMihomoBoot)
	if err != nil {
		return mihomoGuardDesiredDefaults
	}
	s := strings.TrimSpace(v)
	return s == "" || s == "1" || strings.EqualFold(s, "true") ||
		strings.EqualFold(s, "on") || strings.EqualFold(s, "yes")
}

// SetDesiredRunning 持久化期望运行态。失败只返回 error，由调用方记日志。
func (g *MihomoGuard) SetDesiredRunning(want bool) error {
	if g.db == nil {
		return errors.New("数据库未初始化")
	}
	val := "false"
	if want {
		val = "true"
	}
	return g.db.SetSetting(settingMihomoBoot, val)
}

// SetArmed 置位/解除守护武装。
//
// 启动接管流程（AttachExternal / Start 完成）后必须 SetArmed(true)，
// 否则第一个 5s tick 会把「正在被接管但 Status 仍为 stopped」的内核
// 再 Start 一遍。普通运行期保持 true。
func (g *MihomoGuard) SetArmed(armed bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.armed = armed
}

// Guard 在期望运行且已武装时，检测内核停止并自动拉起（限次）。
//
// 限次策略：近 guardWindow（60s）内最多 guardMaxAttempts（3）次启动尝试，
// 超出则放弃本轮，等待窗口滑出再试。滑动窗口防「内核起来就崩」死循环，
// 又不至于在快速恢复场景下卡死。
func (g *MihomoGuard) Guard(ctx context.Context) {
	if !g.isArmed() {
		return
	}
	if !g.DesiredRunning() {
		// 用户手动停过：不拉。也顺带清空尝试计数，避免重新开启后
		// 仍被旧的失败记录挡住。
		g.resetAttempts()
		return
	}

	st := g.mgr.Status()
	if st.IsRunning {
		// 正常状态：成功即清零，让后续一次性尝试失败后仍能拉起
		g.resetAttempts()
		return
	}

	if !g.allowAttempt() {
		g.logger.Errorf("内核异常停止，但近 %d 秒内已尝试 %d 次，暂停自动拉起", guardWindow/time.Second, guardMaxAttempts)
		return
	}

	g.logger.Infof("检测到内核停止且期望运行，自动拉起（第 %d 次尝试）", g.attemptCount())
	if err := g.mgr.Start(ctx); err != nil {
		// "mihomo is already running" 说明刚被其它路径拉起，不算失败
		if !strings.Contains(err.Error(), "already running") {
			g.logger.Errorf("自动拉起内核失败: %v", err)
		}
		return
	}
	g.resetAttempts()
}

// attemptCount 返回近窗口内的尝试次数。
func (g *MihomoGuard) attemptCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.attempts)
}

// armed 是否已武装。
func (g *MihomoGuard) isArmed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.armed
}

// allowAttempt 判断是否允许再试一次，并把本次尝试记入窗口。
func (g *MihomoGuard) allowAttempt() bool {
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	cutoff := now.Add(-guardWindow)
	kept := g.attempts[:0]
	for _, t := range g.attempts {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	g.attempts = kept
	if len(g.attempts) >= guardMaxAttempts {
		return false
	}
	g.attempts = append(g.attempts, now)
	return true
}

// resetAttempts 清零尝试计数（内核恢复 / 期望关闭 / 手动操作成功时调用）。
func (g *MihomoGuard) resetAttempts() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.attempts = nil
}

// ResetAttempts 供手动启动/重启成功后清零尝试计数。
func (g *MihomoGuard) ResetAttempts() {
	g.resetAttempts()
}

// guardWindow 守护限次窗口。
const guardWindow = 60 * time.Second

// guardMaxAttempts 守护在窗口内允许的最大启动尝试次数。
const guardMaxAttempts = 3
