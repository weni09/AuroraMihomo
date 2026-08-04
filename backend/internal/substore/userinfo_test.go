package substore

import (
	"testing"
	"time"
)

// 单位换算辅助：与实现 sizeToBytes 一致（常量表达式无法直接做浮点→int64 转换）。
func gb(v float64) int64 { return int64(v * float64(1<<30)) }
func tb(v float64) int64 { return int64(v * float64(1<<40)) }

// 机场（V2Board 类）不下发 subscription-userinfo 响应头时，
// 流量信息只能从节点名兜底解析——这是「良心云」等订阅能显示流量的关键。
func TestParseUserInfoFromNames(t *testing.T) {
	cases := []struct {
		name  string
		names []string
		total int64
		used  int64
		exp   int64 // 0 表示不限期
	}{
		{
			name:  "剩余流量 GB",
			names: []string{"剩余流量：1000 GB", "日本高速01|CTCU|0.5x"},
			total: gb(1000),
		},
		{
			name:  "剩余流量带空格与小数",
			names: []string{"节点A", "剩余流量: 2.5 TB"},
			total: tb(2.5),
		},
		{
			name:  "裸流量兜底",
			names: []string{"流量：500MB"},
			total: 500 << 20,
		},
		{
			name:  "已用流量进 used",
			names: []string{"已用流量：123.45 GB"},
			used:  gb(123.45),
		},
		{
			name:  "到期日期",
			names: []string{"套餐到期：2026-12-31"},
			exp:   mustLocalDate(t, "2026-12-31"),
		},
		{
			name:  "到期日期斜杠与时间",
			names: []string{"到期时间：2026/8/1 23:59"},
			exp:   mustLocalDate(t, "2026-8-1 23:59"),
		},
		{
			name:  "长期有效不设到期",
			names: []string{"套餐到期：长期有效"},
		},
		{
			name:  "节点名全是普通名字",
			names: []string{"日本高速01", "香港01|IEPL"},
		},
		{
			name:  "综合多个节点",
			names: []string{"日本01", "剩余流量：1.2 TB", "套餐到期：2027-03-15"},
			total: tb(1.2),
			exp:   mustLocalDate(t, "2027-3-15"),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info := parseUserInfoFromNames(c.names)
			if info.Total != c.total {
				t.Errorf("Total = %d, want %d", info.Total, c.total)
			}
			if info.Download != c.used {
				t.Errorf("Download(已用) = %d, want %d", info.Download, c.used)
			}
			if info.Expire != c.exp {
				t.Errorf("Expire = %d, want %d", info.Expire, c.exp)
			}
		})
	}
}

func TestParseUserInfoFromNames_NoMatchIsZero(t *testing.T) {
	info := parseUserInfoFromNames([]string{"普通节点", "另一个节点|IEPL"})
	if !info.IsZero() {
		t.Fatalf("无匹配时应为零值，实际 %+v", info)
	}
}

func mustLocalDate(t *testing.T, s string) int64 {
	t.Helper()
	layouts := []string{"2006-1-2 15:4", "2006-1-2"}
	for _, l := range layouts {
		if tm, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return tm.Unix()
		}
	}
	t.Fatalf("测试日期无法解析: %s", s)
	return 0
}
