package service

import "testing"

// external-controller 的各种写法都要能正确解析出面板需要的主机与端口
func TestKernelAPITargetParsing(t *testing.T) {
	cases := []struct {
		name       string
		controller string
		secret     string
		wantHost   string
		wantPort   string
	}{
		{"标准本机地址", "127.0.0.1:9090", "s1", "127.0.0.1", "9090"},
		{"监听所有网卡应清空主机", "0.0.0.0:9090", "s2", "", "9090"},
		{"省略主机", ":9090", "", "", "9090"},
		{"自定义端口", "127.0.0.1:19090", "", "127.0.0.1", "19090"},
	}

	for _, c := range cases {
		svc, _, _ := newTestConfigService(t)
		if err := svc.writeConfigAtomically([]byte(
			"external-controller: " + c.controller + "\nsecret: " + c.secret + "\n")); err != nil {
			t.Fatal(err)
		}
		got, err := svc.KernelAPITarget()
		if err != nil {
			t.Fatalf("[%s] 解析失败: %v", c.name, err)
		}
		if got.Host != c.wantHost || got.Port != c.wantPort {
			t.Errorf("[%s] 期望 host=%q port=%q，实际 host=%q port=%q",
				c.name, c.wantHost, c.wantPort, got.Host, got.Port)
		}
		if got.Secret != c.secret {
			t.Errorf("[%s] secret 应为 %q，实际 %q", c.name, c.secret, got.Secret)
		}
	}
}

// 未启用 external-controller 时应明确报错，而不是给出一个连不上的地址
func TestKernelAPITargetWithoutController(t *testing.T) {
	svc, _, _ := newTestConfigService(t)
	if err := svc.writeConfigAtomically([]byte("mode: rule\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.KernelAPITarget(); err == nil {
		t.Fatal("未配置 external-controller 时应返回错误")
	}
}
