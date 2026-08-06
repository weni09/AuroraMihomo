package service

import (
	"testing"
	"time"
)

// 覆盖 networkRates 的四类边界：正常差分、首次采样无基线、
// 计数器重置（网卡/系统重启）、时钟倒退。这些场景在真实部署里
// 都会出现（面板长驻 + 内核重启），速率给 0 而不是负值或巨数。
func TestNetworkRates(t *testing.T) {
	base := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		prevUp   uint64
		prevDown uint64
		prevTime time.Time
		hasPrev  bool
		curUp    uint64
		curDown  uint64
		curTime  time.Time
		wantUp   uint64
		wantDown uint64
	}{
		{
			name:   "正常差分：10 秒内上行 10MB 下行 5MB",
			prevUp: 1000, prevDown: 2000, prevTime: base, hasPrev: true,
			curUp: 10_485_800, curDown: 5_244_000, curTime: base.Add(10 * time.Second),
			wantUp: 1_048_480, wantDown: 524_200,
		},
		{
			name:    "首次采样无基线：速率归零只记录基线",
			hasPrev: false,
			curUp:   10_000, curDown: 20_000, curTime: base,
			wantUp: 0, wantDown: 0,
		},
		{
			name:   "计数器重置：当前值小于基线按 0 处理",
			prevUp: 100_000, prevDown: 200_000, prevTime: base, hasPrev: true,
			curUp: 5_000, curDown: 150_000, curTime: base.Add(5 * time.Second),
			wantUp: 0, wantDown: 0,
		},
		{
			name:   "时钟倒退：不产出负速率",
			prevUp: 100_000, prevDown: 200_000, prevTime: base, hasPrev: true,
			curUp: 110_000, curDown: 210_000, curTime: base.Add(-1 * time.Second),
			wantUp: 0, wantDown: 0,
		},
		{
			name:   "间隔为零：同一时刻两次采样不除零",
			prevUp: 100_000, prevDown: 200_000, prevTime: base, hasPrev: true,
			curUp: 110_000, curDown: 210_000, curTime: base,
			wantUp: 0, wantDown: 0,
		},
		{
			name:   "上行下降下行上升：只丢弃异常方向",
			prevUp: 100_000, prevDown: 200_000, prevTime: base, hasPrev: true,
			curUp: 110_000, curDown: 200_000, curTime: base.Add(2 * time.Second),
			wantUp: 5_000, wantDown: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up, down := networkRates(
				tt.prevUp, tt.prevDown, tt.prevTime, tt.hasPrev,
				tt.curUp, tt.curDown, tt.curTime,
			)
			if up != tt.wantUp || down != tt.wantDown {
				t.Errorf("networkRates() = (up=%d, down=%d), want (up=%d, down=%d)",
					up, down, tt.wantUp, tt.wantDown)
			}
		})
	}
}
