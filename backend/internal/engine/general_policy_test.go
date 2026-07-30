package engine

import (
	"strings"
	"testing"

	"auroramihomo/backend/internal/domain"
)

const localFull = `
mode: rule
port: 7890
mixed-port: 7891
find-process-mode: strict
tcp-concurrent: false
geo-auto-update: false
external-controller: 127.0.0.1:9090
secret: localsecret
sniffer:
  enable: false
listeners:
  - name: local-in
    type: http
`

const remotePartial = `
mode: global
port: 8888
tcp-concurrent: true
sniffer:
  enable: true
listeners:
  - name: remote-in
    type: socks
`

// 本地优先（默认）：所有冲突键都应保留本地值
func TestGeneralPolicyLocalFirst(t *testing.T) {
	e := NewMergeEngine()
	base, err := e.LoadAndParse([]byte(localFull))
	if err != nil {
		t.Fatal(err)
	}
	remote, err := e.LoadAndParse([]byte(remotePartial))
	if err != nil {
		t.Fatal(err)
	}

	res := e.MergeDetailedWithPolicy(base, remote, nil, nil, domain.DefaultMergePolicy())
	c := res.Config

	if c.Mode != "rule" {
		t.Errorf("本地优先时 mode 应为 rule，实际 %q", c.Mode)
	}
	if c.Port != 7890 {
		t.Errorf("本地优先时 port 应为 7890，实际 %d", c.Port)
	}
	if c.TCPConcurrent {
		t.Error("本地优先时 tcp-concurrent 应保持本地的 false")
	}
	if c.Sniffer.Enable {
		t.Error("本地优先时 sniffer.enable 应保持本地的 false")
	}
	// General 兜底 map 中的同名键也应保留本地
	if lv, ok := c.General["listeners"]; ok {
		if s, _ := lv.([]interface{}); len(s) > 0 {
			if m, _ := s[0].(map[string]interface{}); m["name"] != "local-in" {
				t.Errorf("本地优先时 listeners 应保留本地定义，实际 %v", m["name"])
			}
		}
	}
}

// 远程优先：远程声明了的键采用远程值，远程未声明的键仍保留本地值
func TestGeneralPolicyRemoteFirstKeepsUndeclaredLocal(t *testing.T) {
	e := NewMergeEngine()
	base, err := e.LoadAndParse([]byte(localFull))
	if err != nil {
		t.Fatal(err)
	}
	remote, err := e.LoadAndParse([]byte(remotePartial))
	if err != nil {
		t.Fatal(err)
	}

	policy := domain.DefaultMergePolicy()
	policy.GeneralPriority = "remote"
	res := e.MergeDetailedWithPolicy(base, remote, nil, nil, policy)
	c := res.Config

	// 远程声明过的键 -> 用远程
	if c.Mode != "global" {
		t.Errorf("远程优先时 mode 应为 global，实际 %q", c.Mode)
	}
	if c.Port != 8888 {
		t.Errorf("远程优先时 port 应为 8888，实际 %d", c.Port)
	}
	if !c.TCPConcurrent {
		t.Error("远程优先时 tcp-concurrent 应为远程的 true")
	}
	if !c.Sniffer.Enable {
		t.Error("远程优先时 sniffer.enable 应为远程的 true")
	}
	if lv, ok := c.General["listeners"]; ok {
		if s, _ := lv.([]interface{}); len(s) > 0 {
			if m, _ := s[0].(map[string]interface{}); m["name"] != "remote-in" {
				t.Errorf("远程优先时 listeners 应采用远程定义，实际 %v", m["name"])
			}
		}
	}

	// 远程完全没声明的键 -> 必须保留本地，不能被零值抹掉
	if c.MixedPort != 7891 {
		t.Errorf("远程未声明 mixed-port，应保留本地 7891，实际 %d", c.MixedPort)
	}
	if c.FindProcessMode != "strict" {
		t.Errorf("远程未声明 find-process-mode，应保留本地 strict，实际 %q", c.FindProcessMode)
	}
	if c.ExternalController != "127.0.0.1:9090" {
		t.Errorf("远程未声明 external-controller，应保留本地值，实际 %q", c.ExternalController)
	}
	if c.Secret != "localsecret" {
		t.Errorf("远程未声明 secret，应保留本地值，实际 %q", c.Secret)
	}
}

// 远程配置整体为空时，合并结果应完整等于本地配置
func TestGeneralPolicyEmptyRemoteKeepsLocal(t *testing.T) {
	e := NewMergeEngine()
	base, err := e.LoadAndParse([]byte(localFull))
	if err != nil {
		t.Fatal(err)
	}
	empty := &domain.Config{}

	for _, prio := range []string{"local", "remote"} {
		policy := domain.DefaultMergePolicy()
		policy.GeneralPriority = prio
		res := e.MergeDetailedWithPolicy(base, empty, nil, nil, policy)
		c := res.Config

		if c.Mode != "rule" || c.Port != 7890 || c.MixedPort != 7891 {
			t.Errorf("[%s] 远程为空时应完整保留本地配置，实际 mode=%q port=%d mixed=%d",
				prio, c.Mode, c.Port, c.MixedPort)
		}
		if c.ExternalController != "127.0.0.1:9090" || c.Secret != "localsecret" {
			t.Errorf("[%s] 远程为空时不应抹掉本地的 controller/secret", prio)
		}
	}
}

// 敏感的运行环境字段不应因远程优先而被订阅劫持到意外值：
// 这里验证的是"远程没写就一定不动"，避免订阅悄悄改掉管理端口/密钥
func TestGeneralPolicyRemoteCannotBlankOutSecrets(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte("external-controller: 127.0.0.1:9090\nsecret: keepme\n"))
	remote, _ := e.LoadAndParse([]byte("mode: global\n"))

	policy := domain.DefaultMergePolicy()
	policy.GeneralPriority = "remote"
	c := e.MergeDetailedWithPolicy(base, remote, nil, nil, policy).Config

	if c.Secret != "keepme" || c.ExternalController != "127.0.0.1:9090" {
		t.Fatalf("远程未声明时不得清空管理接口配置，实际 controller=%q secret=%q",
			c.ExternalController, c.Secret)
	}
}

// conflicts 表保留历史记录，若解决结果允许无条件插入，
// 机场早已下线的节点会被历史冲突永久重新注入 config.yaml。
// local / remote / merge 只应替换已存在项。
func TestResolvedConflictDoesNotReviveRemovedItems(t *testing.T) {
	e := NewMergeEngine()
	cur := "proxies:\n  - {name: Alive, type: ss, server: a.com, port: 1}\nrules:\n  - DOMAIN,live.com,DIRECT\n"

	for _, res := range []string{"local", "remote", "merge"} {
		base, _ := e.LoadAndParse([]byte(cur))
		remote, _ := e.LoadAndParse([]byte(cur))
		resolved := []domain.Conflict{
			{
				Type: "proxy", Resolution: res,
				Local:  map[string]any{"name": "GoneNode", "type": "ss", "server": "g.com", "port": 9},
				Remote: map[string]any{"name": "GoneNode", "type": "ss", "server": "g.com", "port": 9},
			},
			{
				Type: "rule", Resolution: res,
				Local:  "DOMAIN,gone.com,DIRECT",
				Remote: "DOMAIN,gone.com,REJECT",
			},
		}
		out := e.MergeDetailedWithPolicy(base, remote, nil, resolved, domain.DefaultMergePolicy()).Config

		for _, p := range out.Proxies {
			if p.Name == "GoneNode" {
				t.Errorf("[%s] 已下线节点不应被历史冲突重新注入", res)
			}
		}
		for _, r := range out.Rules {
			if strings.Contains(r, "gone.com") {
				t.Errorf("[%s] 已移除规则不应被历史冲突重新注入", res)
			}
		}
	}
}

// manual 是用户显式手写的值，必须仍然允许新增
func TestManualResolutionCanStillInsert(t *testing.T) {
	e := NewMergeEngine()
	base, _ := e.LoadAndParse([]byte("proxies:\n  - {name: Alive, type: ss, server: a.com, port: 1}\n"))
	remote, _ := e.LoadAndParse([]byte("proxies:\n  - {name: Alive, type: ss, server: a.com, port: 1}\n"))

	resolved := []domain.Conflict{{
		Type: "proxy", Resolution: "manual",
		Manual: map[string]any{"name": "HandWritten", "type": "ss", "server": "h.com", "port": 7},
	}}
	out := e.MergeDetailedWithPolicy(base, remote, nil, resolved, domain.DefaultMergePolicy()).Config

	found := false
	for _, p := range out.Proxies {
		if p.Name == "HandWritten" {
			found = true
		}
	}
	if !found {
		t.Fatal("manual 手写节点应被写入最终配置")
	}
}
