package diagnostics

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTCPProbeSuccess(t *testing.T) {
	// 本地监听一个 TCP 端口，验证建连成功、延迟为正
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	probe := &TCPProbe{}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeTCP, Target: "127.0.0.1", Port: port}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("应成功, got %+v", res)
	}
	if res.Error != "" {
		t.Fatalf("成功结果不应有 Error, got %q", res.Error)
	}
	// 本机回环握手常在 1ms 内，LatencyMs 可能为 0——只断言非负（毫秒级延迟合法）
	if res.LatencyMs < 0 {
		t.Fatalf("延迟不应为负, got %d", res.LatencyMs)
	}
}

func TestTCPProbeRefused(t *testing.T) {
	// 连接未监听端口应失败（无端口在 1 上监听）
	probe := &TCPProbe{}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeTCP, Target: "127.0.0.1", Port: 1}, PathDirect, nil)
	if res.Status != StatusFail {
		t.Fatalf("连接被拒应标记 fail, got %+v", res)
	}
	if res.Error == "" {
		t.Fatal("失败结果应包含错误信息")
	}
}

func TestTCPProbeTimeout(t *testing.T) {
	// ctx 已取消：DialContext 立即返回，应标记 timeout 而非 fail
	probe := &TCPProbe{DialTimeout: 100 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	res := probe.Run(ctx, DiagnosticTarget{Type: TypeTCP, Target: "127.0.0.1", Port: port}, PathDirect, nil)
	if res.Status != StatusTimeout {
		t.Fatalf("ctx 取消应标记 timeout, got %+v", res)
	}
	if res.Error == "" {
		t.Fatal("timeout 结果应包含错误信息")
	}
}

func TestTCPProbePortRequired(t *testing.T) {
	// 端口必填：Port<=0 应回填 StatusError
	probe := &TCPProbe{}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeTCP, Target: "127.0.0.1"}, PathDirect, nil)
	if res.Status != StatusError {
		t.Fatalf("缺少端口应标记 error, got %+v", res)
	}
	if res.Error == "" {
		t.Fatal("缺少端口结果应包含错误信息")
	}
}

func TestTCPProbeProxyPathViaConnect(t *testing.T) {
	// 本地「代理」收到 CONNECT 请求即视为 proxy 路径连通；目标用
	// target.invalid（永不解析）——若探测直连目标必然解析失败，成功且
	// CONNECT 目标正确即证明流量真实经代理。
	addr, gotConnect := startCONNECTProxy(t)

	probe := &TCPProbe{
		Selector: func(path string) *http.Client {
			return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: addr})}}
		},
	}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeTCP, Target: "target.invalid", Port: 443}, PathProxy, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("经代理 CONNECT 应成功, got %+v", res)
	}
	if res.Error != "" {
		t.Fatalf("成功结果不应有 Error, got %q", res.Error)
	}
	select {
	case line := <-gotConnect:
		if !strings.Contains(line, "target.invalid:443") {
			t.Fatalf("CONNECT 目标应含 target.invalid:443, got %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("代理未收到 CONNECT 请求")
	}
}

func TestTCPProbeProxyPathFallbackToDirect(t *testing.T) {
	// 选择器回落直连（client 无 Proxy）：proxy 路径应直连目标并标注回落，
	// 而不是报错或谎称走了代理。
	probe := &TCPProbe{Selector: func(path string) *http.Client { return directHTTPClient(0) }}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeTCP, Target: "127.0.0.1", Port: 1}, PathProxy, nil)
	if res.Status != StatusFail {
		t.Fatalf("127.0.0.1:1 直连应失败, got %+v", res)
	}
	detail, ok := res.Detail.(map[string]interface{})
	if !ok {
		t.Fatalf("回落结果 Detail 应为 map, got %T", res.Detail)
	}
	if fb, _ := detail["proxyFallback"].(bool); !fb {
		t.Fatalf("回落结果应标注 proxyFallback, got %+v", detail)
	}
}

func TestTCPProbeConcurrentSafe(t *testing.T) {
	// 同一实例并发执行：无全局可变状态，配合 -race 验证并发安全
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	probe := &TCPProbe{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeTCP, Target: "127.0.0.1", Port: port}, PathDirect, nil)
			if res.Status != StatusSuccess {
				t.Errorf("并发探测应成功, got %+v", res)
			}
		}()
	}
	wg.Wait()
}
