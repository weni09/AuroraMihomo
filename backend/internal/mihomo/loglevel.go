package mihomo

import "strings"

// Level 是内核日志的级别。
//
// 取值对齐 mihomo 自身 log-level 的用词（silent/error/warning/info/debug），
// 刻意不与 applog.Level 共用类型：两者语义不同（那边有 slow/stat/severe，
// 这边有 warning），混用会让某一侧出现永远匹配不到的级别。
type Level string

const (
	LevelDebug   Level = "debug"
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
	// LevelSilent 是 mihomo 认的级别之一，实际不会出现在日志行里
	// （silent 表示不输出），但解析时接受它，避免把它当成未知级别。
	LevelSilent Level = "silent"
)

// levelPrefix 是 mihomo 输出中级别字段的前缀。
//
// mihomo 用 logrus 的 logfmt formatter，每行形如：
//
//	time="2026-08-01T03:31:53Z" level=warning msg="parse classical rule ..."
//
// 级别值不带引号（logrus 只在值含空格或特殊字符时才加引号，而级别值恒为
// 单个小写单词），因此这里按裸值解析，不处理引号。
const levelPrefix = "level="

// parseLevel 从内核日志行中提取级别。
//
// 无法识别时返回空串，表示"这行没有级别"——mihomo 并非每行都走 logrus
// （启动横幅、panic 栈、Go runtime 的输出都是裸文本），把它们硬归到某个
// 级别会让按级别筛选时凭空多出或漏掉内容。空级别在过滤时的处理见 Logs。
//
// 只做前缀扫描而不上正则：这个函数在每一行日志上都会被调用，而内核在
// 加载大规则集时能一次刷出上千行。
func parseLevel(line string) Level {
	idx := strings.Index(line, levelPrefix)
	if idx < 0 {
		return ""
	}

	// 要求 level= 前是行首或空白，否则 msg 正文里出现的 "level=" 会被误当成
	// 级别字段（例如内核报错时回显用户配置里的 log-level 取值）。
	if idx > 0 && line[idx-1] != ' ' && line[idx-1] != '\t' {
		return ""
	}

	rest := line[idx+len(levelPrefix):]
	// 级别值到下一个空白为止
	if end := strings.IndexAny(rest, " \t"); end >= 0 {
		rest = rest[:end]
	}

	return normalizeLevel(rest)
}

// normalizeLevel 把级别字符串规范化，不认识的返回空串。
//
// 接受 warn 是因为部分 logrus 版本与第三方库会写 warn 而非 warning，
// 统一归到 warning，避免前端筛选出现两个等价选项。
func normalizeLevel(s string) Level {
	switch Level(strings.ToLower(strings.TrimSpace(s))) {
	case LevelDebug:
		return LevelDebug
	case LevelInfo:
		return LevelInfo
	case LevelWarning, "warn":
		return LevelWarning
	case LevelError:
		return LevelError
	case LevelSilent:
		return LevelSilent
	default:
		return ""
	}
}

// ParseLevel 供 API 层规范化用户传入的级别筛选参数，
// 无法识别时返回空串（表示不过滤）。
func ParseLevel(s string) Level {
	return normalizeLevel(s)
}
