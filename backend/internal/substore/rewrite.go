package substore

import (
	"regexp"
	"sort"
	"strings"
)

func ApplyRewrite(nodes []Node, rules []RewriteRule) []Node {
	if len(rules) == 0 {
		return nodes
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Priority < rules[j].Priority })

	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		keep := true
		for _, r := range rules {
			if !r.Enabled || strings.TrimSpace(r.Pattern) == "" {
				continue
			}
			re, err := regexp.Compile(r.Pattern)
			if err != nil {
				continue
			}
			val := nodeScopeValue(n, r.Scope)
			switch strings.ToLower(r.FilterMode) {
			case "include":
				if !re.MatchString(val) {
					keep = false
				}
			case "exclude":
				if re.MatchString(val) {
					keep = false
				}
			default: // rewrite
				switch r.Scope {
				case "name", "": // 未指定作用域时默认改写节点名
					n.Name = re.ReplaceAllString(n.Name, r.Replace)
				case "server":
					n.Server = re.ReplaceAllString(n.Server, r.Replace)
				case "type":
					n.Type = re.ReplaceAllString(n.Type, r.Replace)
				}
			}
			if !keep {
				break
			}
		}
		if keep {
			out = append(out, n)
		}
	}
	return out
}

func nodeScopeValue(n Node, scope string) string {
	switch strings.ToLower(scope) {
	case "server":
		return n.Server
	case "type":
		return n.Type
	default:
		return n.Name
	}
}
