package diagnostics

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"
)

// degradedTrue 断言结果 Detail 标记了 degraded:true。
func degradedTrue(t *testing.T, res ProbeResult) bool {
	t.Helper()
	if res.Detail == nil {
		return false
	}
	m, ok := res.Detail.(map[string]interface{})
	if !ok {
		t.Fatalf("Detail 应为 map, got %T", res.Detail)
	}
	d, ok := m["degraded"].(bool)
	return ok && d
}

func TestPingProbeICMPSuccess(t *testing.T) {
	// 注入 PingCmd 成功：应标记 success，且不降级（Detail 为空）
	probe := &PingProbe{
		PingCmd: func(ctx context.Context, host string) ([]byte, error) {
			return []byte("reply from " + host), nil
		},
	}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypePing, Target: "example.com"}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("ICMP 成功应标记 success, got %+v", res)
	}
	if res.Error != "" {
		t.Fatalf("成功结果不应有 Error, got %q", res.Error)
	}
	if res.Detail != nil {
		t.Fatalf("ICMP 成功不应降级, Detail 应为空, got %+v", res.Detail)
	}
	if res.LatencyMs < 0 {
		t.Fatalf("延迟不应为负, got %d", res.LatencyMs)
	}
}

func TestPingProbeICMPFail(t *testing.T) {
	// 注入 PingCmd 返回普通错误：应标记 fail，且不降级
	probe := &PingProbe{
		PingCmd: func(ctx context.Context, host string) ([]byte, error) {
			return nil, errors.New("ping: 请求超时")
		},
	}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypePing, Target: "example.com"}, PathDirect, nil)
	if res.Status != StatusFail {
		t.Fatalf("普通 ICMP 错误应标记 fail, got %+v", res)
	}
	if res.Error == "" {
		t.Fatal("失败结果应包含错误信息")
	}
	if degradedTrue(t, res) {
		t.Fatalf("非 errNoICMP 失败不应降级, got %+v", res.Detail)
	}
}

func TestPingProbeFallbackToTCP(t *testing.T) {
	// 注入 PingCmd 返回 errNoICMP：应降级为 TCP 建连并标记 degraded
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	probe := &PingProbe{
		PingCmd: func(ctx context.Context, host string) ([]byte, error) {
			return nil, errNoICMP
		},
	}
	// 本地监听端口可连 → 降级成功并标记 degraded
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypePing, Target: "127.0.0.1", Port: port}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("errNoICMP 应降级并连上本地监听端口, got %+v", res)
	}
	if !degradedTrue(t, res) {
		t.Fatalf("降级成功结果应标记 degraded, got %+v", res.Detail)
	}
	// 连接被拒 → 降级失败，但 Detail 仍标记 degraded
	res = probe.Run(context.Background(), DiagnosticTarget{Type: TypePing, Target: "127.0.0.1", Port: 1}, PathDirect, nil)
	if res.Status != StatusFail {
		t.Fatalf("降级后连接被拒应标记 fail, got %+v", res)
	}
	if !degradedTrue(t, res) {
		t.Fatalf("降级失败结果也应标记 degraded, got %+v", res.Detail)
	}
}

func TestPingProbePortOverride(t *testing.T) {
	// 目标指定 Port 时，降级应连接该端口而非默认 443
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	probe := &PingProbe{
		PingCmd: func(ctx context.Context, host string) ([]byte, error) {
			return nil, errNoICMP
		},
	}
	// 接受连接并回传服务端绑定地址，据此断言探测连的确实是 target.Port
	remoteCh := make(chan net.Addr, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			remoteCh <- nil
			return
		}
		remoteCh <- conn.LocalAddr()
		conn.Close()
	}()

	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypePing, Target: "127.0.0.1", Port: port}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("降级应使用 target.Port 连上监听端口, got %+v", res)
	}
	if !degradedTrue(t, res) {
		t.Fatalf("降级成功结果应标记 degraded, got %+v", res.Detail)
	}
	select {
	case remote := <-remoteCh:
		if remote == nil {
			t.Fatal("监听端口未收到连接")
		}
		_, remotePort, err := net.SplitHostPort(remote.String())
		if err != nil || remotePort != strconv.Itoa(port) {
			t.Fatalf("降级应连接 target.Port=%d, 实际连到 %s", port, remote)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待连接超时")
	}
}
