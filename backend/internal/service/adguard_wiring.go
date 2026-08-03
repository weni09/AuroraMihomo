package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AdGuard 相关 settings 键。无新 migration：一律走 settings KV。
const (
	settingAdGuardBoot           = "adguard.enabled_at_boot"
	settingAdGuardWebAddr        = "adguard.web_addr"
	settingAdGuardDNSPort        = "adguard.dns_port"
	settingAdGuardVersion        = "adguard.version"
	settingAdGuardWiring         = "adguard.dns_wiring"          // "off" | "on"
	settingAdGuardSnapshot       = "adguard.dns_wiring_snapshot" // JSON of WiringPlan
	settingAdGuardComponent      = "adguard.component_enabled"   // "true" / "false"，默认 false
	settingAdGuardDNSMode        = "adguard.dns_mode"            // "0" | "1" | "2"，默认 "0"
	settingAdGuardCDNProviders   = "adguard.cdn_providers"       // JSON 字符串数组
	settingAdGuardAutoUpdate     = "adguard.auto_update"         // "true" / "false"
	settingAdGuardAutoUpdateCron = "adguard.auto_update_cron"    // 6 段 cron
	settingAdGuardUsername       = "adguard.username"            // AGH 管理员用户名（明文可存）
)

// defaultAdGuardAutoUpdateCron 与全局组件自动更新默认一致（每天 4 点）。
const defaultAdGuardAutoUpdateCron = "0 0 4 * * *"

const (
	adguardWiringOff = "off"
	adguardWiringOn  = "on"

	// 冲突时 mihomo 默认挪到的回环地址：AGH 占 1053 时用 1054，
	// 保留 fake-ip/policy 能力且不与 AGH 抢口。
	defaultConflictMihomoListen = "127.0.0.1:1054"
)

// WiringOptions 是一键对接向导的可勾选项（P1）。
type WiringOptions struct {
	RedirectTProxy  bool // A. TProxy DNS → AGH
	ResolveConflict bool // B. 避免 mihomo/AGH 端口冲突
	PatchUpstream   bool // C. AGH 上游 → mihomo DNS
	WeakenTUNHijack bool // D. 弱化 TUN dns-hijack（默认 false）
}

// currentDNSState 是 buildWiringPlan 的输入快照（纯数据，便于单测）。
type currentDNSState struct {
	AGHDNSPort       int
	MihomoDNSListen  string // 当前 base/最终 listen 原文，如 "0.0.0.0:1053"
	MihomoDNSPort    int
	MihomoDNSEnabled bool
	TProxyEnabled    bool
	TUNEnabled       bool
	// OriginalUpstream 仅在 apply 前探测，计划阶段可为空
	OriginalUpstream []string
}

// WiringPlan 描述一次对接将执行（或已执行）的动作与回滚所需原文。
//
// 同时作为 settings 里的 snapshot JSON 形态：回滚只依赖这一份，
// 不另建表。字段名与任务约定的 snapshot 格式对齐。
type WiringPlan struct {
	Actions              []string `json:"actions"`
	Warnings             []string `json:"warnings,omitempty"`
	AGHDNSPort           int      `json:"aghDnsPort"`
	MihomoDNSListen      string   `json:"mihomoDnsListen,omitempty"` // 变更后目标
	OriginalDNSPort      int      `json:"originalDNSPort"`
	OriginalMihomoListen string   `json:"originalMihomoListen,omitempty"`
	OriginalUpstream     []string `json:"originalUpstream,omitempty"`
	// OriginalDNSHijack 清空 tun.dns-hijack 前的原文，供回滚恢复
	OriginalDNSHijack []string `json:"originalDNSHijack,omitempty"`
	WiringOn          bool     `json:"wiringOn"`

	// 下列标志记录「计划里选中了哪些动作」，apply/rollback 按标志执行，
	// 避免仅靠中文 Actions 文案做分支。
	DidRedirect        bool `json:"didRedirect,omitempty"`
	DidResolveConflict bool `json:"didResolveConflict,omitempty"`
	DidPatchUpstream   bool `json:"didPatchUpstream,omitempty"`
	DidWeakenTUN       bool `json:"didWeakenTUN,omitempty"`
	// DidDNSOnlyRedirect：未开 TProxy 时用 aurora_agh_dns 表做 53→AGH 端口
	DidDNSOnlyRedirect bool `json:"didDNSOnlyRedirect,omitempty"`
	// DidBind53：模式 1，AGH 直接监听 53（入口 DNS）
	DidBind53 bool `json:"didBind53,omitempty"`
}

// buildWiringPlan 根据选项与当前 DNS 状态生成变更清单（纯函数，无 IO）。
//
// 设计取舍：
//   - 不在这里改任何配置，只产出 plan；apply 负责快照与执行。
//   - TProxy 未启用时 Redirect 被勾选 → 写入 warning 并跳过，而不是硬失败：
//     用户可能只想先看计划，或只勾选上游补丁。
//   - 端口冲突且 ResolveConflict 勾选时，默认把 mihomo 挪到 127.0.0.1:1054
//     （AGH 默认 1053 的成对端口）；若 AGH 不是 1053，则用 aghPort+1。
func buildWiringPlan(opts WiringOptions, cur currentDNSState) (WiringPlan, error) {
	if cur.AGHDNSPort <= 0 {
		return WiringPlan{}, fmt.Errorf("AdGuard DNS 端口无效: %d", cur.AGHDNSPort)
	}

	plan := WiringPlan{
		Actions:              make([]string, 0, 4),
		Warnings:             make([]string, 0, 2),
		AGHDNSPort:           cur.AGHDNSPort,
		OriginalDNSPort:      cur.MihomoDNSPort,
		OriginalMihomoListen: cur.MihomoDNSListen,
		OriginalUpstream:     append([]string(nil), cur.OriginalUpstream...),
		WiringOn:             true,
	}
	if plan.OriginalDNSPort <= 0 && cur.MihomoDNSListen != "" {
		plan.OriginalDNSPort = parseListenPort(cur.MihomoDNSListen)
	}

	// A. TProxy DNS → AGH
	if opts.RedirectTProxy {
		if cur.TProxyEnabled {
			plan.DidRedirect = true
			plan.Actions = append(plan.Actions,
				fmt.Sprintf("TProxy DNS 重定向目标 → AdGuard :%d", cur.AGHDNSPort))
		} else {
			plan.Warnings = append(plan.Warnings,
				"未启用 TProxy，已跳过「TProxy DNS → AdGuard」；可稍后启用 TProxy 再对接")
		}
	}

	// B. 端口冲突：mihomo listen 与 AGH 同端口时挪开 mihomo
	if opts.ResolveConflict {
		conflict := cur.MihomoDNSPort > 0 && cur.MihomoDNSPort == cur.AGHDNSPort
		// 端口未知但 listen 原文能解析出与 AGH 相同也算冲突
		if !conflict && cur.MihomoDNSListen != "" {
			if p := parseListenPort(cur.MihomoDNSListen); p > 0 && p == cur.AGHDNSPort {
				conflict = true
			}
		}
		if conflict {
			newListen := conflictMihomoListen(cur.AGHDNSPort)
			plan.DidResolveConflict = true
			plan.MihomoDNSListen = newListen
			plan.Actions = append(plan.Actions,
				fmt.Sprintf("改 mihomo dns.listen → %s（避开 AdGuard :%d）", newListen, cur.AGHDNSPort))
		} else if cur.MihomoDNSPort == 0 && cur.MihomoDNSListen == "" {
			plan.Warnings = append(plan.Warnings, "当前未配置 mihomo dns.listen，跳过冲突处理")
		}
	}

	// C. AGH 上游 → mihomo DNS（保留 fake-ip / policy）
	if opts.PatchUpstream {
		if cur.MihomoDNSPort <= 0 && plan.MihomoDNSListen == "" {
			plan.Warnings = append(plan.Warnings,
				"mihomo DNS 端口未知，已跳过「AdGuard 上游 → mihomo」")
		} else {
			upPort := cur.MihomoDNSPort
			if plan.DidResolveConflict {
				upPort = parseListenPort(plan.MihomoDNSListen)
			}
			if upPort <= 0 {
				upPort = parseListenPort(cur.MihomoDNSListen)
			}
			if upPort > 0 {
				plan.DidPatchUpstream = true
				plan.Actions = append(plan.Actions,
					fmt.Sprintf("AdGuard 上游 DNS → 127.0.0.1:%d（mihomo）", upPort))
			} else {
				plan.Warnings = append(plan.Warnings,
					"无法确定 mihomo DNS 端口，已跳过上游补丁")
			}
		}
	}

	// D. TUN 弱化 hijack（P1 只记动作文案；真正改 base 的 dns-hijack 在 apply）
	if opts.WeakenTUNHijack {
		if cur.TUNEnabled {
			plan.DidWeakenTUN = true
			plan.Actions = append(plan.Actions,
				"弱化 TUN dns-hijack（清空面板注入的 any:53，流量可能绕过 AdGuard）")
		} else {
			plan.Warnings = append(plan.Warnings, "未启用 TUN，已跳过 dns-hijack 弱化")
		}
	}

	if len(plan.Actions) == 0 {
		plan.Warnings = append(plan.Warnings, "当前勾选未产生任何可执行变更")
	}
	return plan, nil
}

// conflictMihomoListen 给出冲突时 mihomo 的新 listen。
// AGH 默认 1053 → 1054；其它端口用 agh+1（越界则 1054）。
func conflictMihomoListen(aghPort int) string {
	if aghPort == 1053 {
		return defaultConflictMihomoListen
	}
	next := aghPort + 1
	if next <= 0 || next > 65535 {
		return defaultConflictMihomoListen
	}
	return fmt.Sprintf("127.0.0.1:%d", next)
}

// marshalWiringSnapshot / unmarshalWiringSnapshot 负责 settings 快照 JSON。
func marshalWiringSnapshot(plan WiringPlan) (string, error) {
	b, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("序列化 wiring 快照失败: %w", err)
	}
	return string(b), nil
}

func unmarshalWiringSnapshot(raw string) (WiringPlan, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return WiringPlan{}, fmt.Errorf("wiring 快照为空")
	}
	var plan WiringPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return WiringPlan{}, fmt.Errorf("解析 wiring 快照失败: %w", err)
	}
	return plan, nil
}

// wiringUpstreamForPlan 计算 apply 时应写入 AGH 的上游列表。
func wiringUpstreamForPlan(plan WiringPlan, curMihomoPort int) []string {
	port := curMihomoPort
	if plan.DidResolveConflict && plan.MihomoDNSListen != "" {
		if p := parseListenPort(plan.MihomoDNSListen); p > 0 {
			port = p
		}
	}
	if port <= 0 {
		return nil
	}
	return []string{fmt.Sprintf("127.0.0.1:%d", port)}
}
