package netcheck

import (
	"fmt"
	"strings"
	"unicode"
)

// 用户自定义防火墙规则的书写与拆除转换。
//
// 自定义规则与内置 nft 规则是两个通道：内置规则由面板生成（nftables），
// 自定义规则由用户书写（iptables 语法），在 Apply 时于内置规则生效后
// 逐条追加执行。iptables 可能是 legacy 或 nf_tables 后端（见 detect 的
// iptablesBackend），二者对规则展示与互见性有差异，调用方需把后端类型
// 一并展示给用户。
//
// 拆除只支持 -A/-I 追加/插入型规则（-D 与 -A 参数结构一致，可直接逆反）；
// -N/-F/-X 等链管理命令无法安全自动逆反，执行时不拦（灵活性的代价），
// 但拆除时会跳过并记日志，UI 与文档需向用户说明。

const (
	// MaxCustomRuleLines 自定义规则最大行数（含注释与空行）。
	MaxCustomRuleLines = 200
	// MaxCustomRuleLineLen 单行命令最大长度，防手滑粘贴进整段脚本。
	MaxCustomRuleLineLen = 512
)

// NormalizeCustomRules 把用户书写的规则文本规范化为完整命令列表。
//
// 每行支持两种形式：
//   - 完整命令：以 iptables / ip6tables 开头（命令与参数用空白分隔）
//   - 裸参数：以 - 开头，自动补 iptables 前缀
//
// 空行与以 # 开头的注释行忽略。返回的命令可直接交给 sh -c 执行。
// 不校验语法（执行时才知道），只负责形式统一与基本防呆。
func NormalizeCustomRules(text string) ([]string, error) {
	var rules []string
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(trimmed) > MaxCustomRuleLineLen {
			return nil, fmt.Errorf("自定义规则第 %d 行超过 %d 字符（疑似粘贴了整段脚本）", i+1, MaxCustomRuleLineLen)
		}
		if len(rules) >= MaxCustomRuleLines {
			return nil, fmt.Errorf("自定义规则最多 %d 行", MaxCustomRuleLines)
		}

		head := strings.Fields(trimmed)[0]
		switch head {
		case "iptables", "ip6tables":
			rules = append(rules, trimmed)
		case "sudo", "sh", "bash":
			return nil, fmt.Errorf("自定义规则第 %d 行不需要 %s 前缀，直接写 iptables 命令", i+1, head)
		default:
			if !strings.HasPrefix(head, "-") {
				return nil, fmt.Errorf("自定义规则第 %d 行必须以 iptables/ip6tables 开头或直接以 - 开头: %q", i+1, trimmed)
			}
			rules = append(rules, "iptables "+trimmed)
		}
	}
	return rules, nil
}

// toDeleteCommand 把应用时的命令转换成拆除命令。
//
// iptables 的 -D 与 -A 参数结构一致，直接替换即可；-I 多一个可选的
// 插入位置参数，-D 不接受位置，需要去掉（-I INPUT 3 -p tcp … 应变成
// -D INPUT -p tcp …，否则 -D 会把 "3" 当作目标链名而报错）。
// 不含 -A/-I 的命令（-N 建链、-F 清链等）无法安全自动逆反，
// 返回空串表示跳过，由调用方记日志提示用户手工清理。
//
// 必须用 shell 风格分词再回拼：Apply 用 sh -c 原串执行，若拆除用
// strings.Fields 会把 --comment "hello world" 拆碎，-D 对不上已应用规则，
// 表现为"关掉了但规则还在"。
func toDeleteCommand(rule string) string {
	fields := splitShellFields(rule)
	// fields[0] 是 iptables/ip6tables，命令主体从下标 1 开始
	for i := 1; i < len(fields); i++ {
		switch fields[i] {
		case "-A":
			fields[i] = "-D"
			return joinShellFields(fields)
		case "-I":
			// -I CHAIN [position] REST… → -D CHAIN REST…
			if i+1 >= len(fields) {
				return ""
			}
			rest := fields[i+2:]
			if len(rest) >= 1 && isAllDigits(rest[0]) {
				rest = rest[1:]
			}
			head := append([]string{}, fields[:i]...)
			head = append(head, "-D", fields[i+1])
			return joinShellFields(append(head, rest...))
		}
	}
	return ""
}

// splitShellFields 按 shell 空白分词，保留引号内空格；去掉包裹引号。
// 不处理反斜杠转义（iptables 规则极少用），足够覆盖 --comment "a b"。
func splitShellFields(s string) []string {
	var (
		fields []string
		cur    strings.Builder
		quote  rune // 0 / ' / "
	)
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		fields = append(fields, cur.String())
		cur.Reset()
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return fields
}

// joinShellFields 把分词结果拼回 sh -c 可执行的命令行。
// 含空白或 shell 元字符的 token 用双引号包裹。
func joinShellFields(fields []string) string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if needsShellQuote(f) {
			out = append(out, `"`+strings.ReplaceAll(f, `"`, `\"`)+`"`)
		} else {
			out = append(out, f)
		}
	}
	return strings.Join(out, " ")
}

func needsShellQuote(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if unicode.IsSpace(r) || strings.ContainsRune(`"'\;|&<>()`, r) {
			return true
		}
	}
	return false
}

// isAllDigits 判断字符串是否全部由十进制数字组成（位置参数的判定）。
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// MergeCustomRuleLists 合并多批自定义规则并去重（保序）。
// Teardown / 失败回滚时常要把「上次已应用」与「本次目标」一并拆掉。
func MergeCustomRuleLists(lists ...[]string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, list := range lists {
		for _, r := range list {
			if r == "" {
				continue
			}
			if _, ok := seen[r]; ok {
				continue
			}
			seen[r] = struct{}{}
			out = append(out, r)
		}
	}
	return out
}
