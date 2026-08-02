package protected

import (
	"strings"
	"testing"

	"auroramihomo/backend/internal/updater"
)

func TestDescribeComponentCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		comp string
		c    updater.ComponentCheck
		want string
	}{
		{
			name: "not present",
			comp: "AdGuardHome",
			c:    updater.ComponentCheck{Present: false},
			want: "AdGuardHome 未安装",
		},
		{
			name: "present with check error",
			comp: "AdGuardHome",
			c:    updater.ComponentCheck{Present: true, Error: "network down"},
			want: "AdGuardHome 已安装，检查最新版本失败: network down",
		},
		{
			name: "update needed",
			comp: "mihomo",
			c: updater.ComponentCheck{
				Present: true, LocalVersion: "v1.0.0", LatestVersion: "v1.2.0", UpdateNeeded: true,
			},
			want: "mihomo 有新版本可用 (v1.2.0)",
		},
		{
			name: "up to date with known local version",
			comp: "mihomo",
			c: updater.ComponentCheck{
				Present: true, LocalVersion: "v1.2.0", LatestVersion: "v1.2.0",
			},
			want: "mihomo 已是最新",
		},
		{
			// AdGuard 版本探测未接入：Present=true、LocalVersion 空，不得说「已是最新」
			name: "present unknown local version without remote",
			comp: "AdGuardHome",
			c:    updater.ComponentCheck{Present: true, LocalVersion: ""},
			want: "AdGuardHome 已安装（本地版本未知）",
		},
		{
			name: "present unknown local version with remote",
			comp: "AdGuardHome",
			c: updater.ComponentCheck{
				Present: true, LocalVersion: "", LatestVersion: "v0.107.50",
			},
			want: "AdGuardHome 已安装（本地版本未知），远程 v0.107.50",
		},
		{
			// zashboard 历史安装无 tag 记录时同样适用
			name: "zashboard present unknown local with remote",
			comp: "zashboard",
			c: updater.ComponentCheck{
				Present: true, LocalVersion: "", LatestVersion: "v1.99.0",
			},
			want: "zashboard 已安装（本地版本未知），远程 v1.99.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := describeComponentCheck(tt.comp, tt.c)
			if got != tt.want {
				t.Fatalf("describeComponentCheck() = %q, want %q", got, tt.want)
			}
			if tt.c.Present && tt.c.LocalVersion == "" && tt.c.Error == "" && !tt.c.UpdateNeeded {
				if strings.Contains(got, "已是最新") {
					t.Fatalf("unknown local version must not claim up-to-date: %q", got)
				}
			}
		})
	}
}
