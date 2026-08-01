package mihomo

import "testing"

func TestParseLevel(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Level
	}{
		{
			// 真实噪音行：规则集里的 USER-AGENT 是 Surge 语法，mihomo 不支持，
			// 逐行 warning。这类行占了内核日志的绝大多数，是本功能的动机。
			name: "规则解析告警",
			line: `time="2026-08-01T03:31:53.590899090Z" level=warning msg="parse classical rule [USER-AGENT,live4iphone*] error: unsupported rule type: USER-AGENT"`,
			want: LevelWarning,
		},
		{
			name: "info",
			line: `time="2026-08-01T03:31:53Z" level=info msg="Start initial configuration in progress"`,
			want: LevelInfo,
		},
		{
			name: "error",
			line: `time="2026-08-01T03:31:53Z" level=error msg="Parse config error"`,
			want: LevelError,
		},
		{
			name: "debug",
			line: `time="2026-08-01T03:31:53Z" level=debug msg="[TCP] dial DIRECT"`,
			want: LevelDebug,
		},
		{
			// 部分 logrus 版本写 warn，统一归到 warning
			name: "warn 归一为 warning",
			line: `time="2026-08-01T03:31:53Z" level=warn msg="deprecated option"`,
			want: LevelWarning,
		},
		{
			name: "级别在行首",
			line: `level=info msg="no time field"`,
			want: LevelInfo,
		},
		{
			// 启动横幅之类的裸文本没有级别，不该被硬归到某一级
			name: "无级别字段",
			line: "Mihomo Meta v1.19.0 darwin arm64 with go1.23",
			want: "",
		},
		{
			name: "空行",
			line: "",
			want: "",
		},
		{
			// msg 正文里回显用户配置的 log-level 时，前缀不是空白，不能误判
			name: "msg 内的 log-level 不被误认",
			line: `time="2026-08-01T03:31:53Z" msg="unsupported log-level=verbose in config"`,
			want: "",
		},
		{
			name: "未知级别值",
			line: `time="2026-08-01T03:31:53Z" level=verbose msg="x"`,
			want: "",
		},
		{
			// silent 是 mihomo 认的级别，虽不会真出现在日志里，也不该算未知
			name: "silent",
			line: `level=silent msg="x"`,
			want: LevelSilent,
		},
		{
			name: "级别值后紧跟制表符",
			line: "level=error\tmsg=\"x\"",
			want: LevelError,
		},
		{
			// 级别是行尾最后一个字段时没有后继空白，不能漏读
			name: "级别在行尾",
			line: `msg="x" level=info`,
			want: LevelInfo,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseLevel(tc.line); got != tc.want {
				t.Fatalf("parseLevel(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

func TestParseLevelExported(t *testing.T) {
	// API 层传进来的可能带大小写与空白
	cases := map[string]Level{
		"WARNING": LevelWarning,
		" info ":  LevelInfo,
		"warn":    LevelWarning,
		"":        "",
		"nope":    "",
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Fatalf("ParseLevel(%q) = %q, want %q", in, got, want)
		}
	}
}

// appendLog 必须在入口处就把级别解析好并随行存下，
// 否则查询与推送路径都拿不到级别。
func TestAppendLogRecordsLevel(t *testing.T) {
	mgr := NewManager(Config{BinaryPath: "noop"})

	mgr.appendLog("stdout", `time="2026-08-01T03:31:53Z" level=warning msg="parse classical rule [USER-AGENT,youku*] error: unsupported rule type: USER-AGENT"`)
	mgr.appendLog("stderr", `time="2026-08-01T03:31:54Z" level=error msg="Parse config error"`)
	// 本项目自己写的 system 流不含 logfmt 字段，级别应为空
	mgr.appendLog("system", "mihomo started pid=123")

	got := mgr.Logs(0, "")
	if len(got) != 3 {
		t.Fatalf("期望 3 条日志，实得 %d", len(got))
	}
	if got[0].Level != LevelWarning {
		t.Fatalf("第 1 行级别 = %q，期望 warning", got[0].Level)
	}
	if got[1].Level != LevelError {
		t.Fatalf("第 2 行级别 = %q，期望 error", got[1].Level)
	}
	if got[2].Level != "" {
		t.Fatalf("system 流级别 = %q，期望空", got[2].Level)
	}
}

// 按级别筛选时，必须"先筛后取尾部"，且无级别的行始终保留。
// 这是本功能的核心场景：warning 刷屏时仍要能捞出 error 与崩溃栈。
func TestLogsFilterByLevel(t *testing.T) {
	mgr := NewManager(Config{BinaryPath: "noop"})

	// 先写一条 error，再用大量 warning 把它挤到很靠前的位置——
	// 若实现是"先取尾部再筛"，这条 error 就会被漏掉。
	mgr.appendLog("stderr", `level=error msg="the error we must not lose"`)
	mgr.appendLog("system", "mihomo started pid=1")
	for i := 0; i < 200; i++ {
		mgr.appendLog("stdout", `level=warning msg="unsupported rule type: USER-AGENT"`)
	}

	errs := mgr.Logs(10, LevelError)
	// 期望：那条 error + 无级别的 system 行
	if len(errs) != 2 {
		t.Fatalf("筛 error 期望 2 条（含无级别行），实得 %d", len(errs))
	}
	var foundErr bool
	for _, ln := range errs {
		if ln.Level == LevelWarning {
			t.Fatal("筛 error 时不应出现 warning")
		}
		if ln.Level == LevelError {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatal("被 warning 挤到前面的 error 行丢失了（疑似先取尾部再筛）")
	}

	// 不带级别时是全量（受缓冲上限约束）
	if all := mgr.Logs(0, ""); len(all) != 202 {
		t.Fatalf("全量期望 202 条，实得 %d", len(all))
	}

	// limit 在筛选后生效
	if warns := mgr.Logs(5, LevelWarning); len(warns) != 5 {
		t.Fatalf("筛 warning 限 5 条，实得 %d", len(warns))
	}
}
