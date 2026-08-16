package diagnostics

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// tracerouteSample 是 Unix traceroute -n 风格的固定输出：含头部与 3 跳。
const tracerouteSample = `traceroute to 8.8.8.8 (8.8.8.8), 30 hops max, 60 byte packets
 1  192.168.1.1  1.2 ms  1.1 ms  1.3 ms
 2  10.0.0.1  2.1 ms  2.2 ms  2.0 ms
 3  8.8.8.8  10.1 ms  9.8 ms  10.3 ms
`

func TestTracerouteParse(t *testing.T) {
	hops := parseTraceroute(tracerouteSample)
	if len(hops) != 3 {
		t.Fatalf("应解析出 3 跳, got %d: %+v", len(hops), hops)
	}
	want := []Hop{
		{Hop: 1, Addr: "192.168.1.1", RTT: "1.2 ms"},
		{Hop: 2, Addr: "10.0.0.1", RTT: "2.1 ms"},
		{Hop: 3, Addr: "8.8.8.8", RTT: "10.1 ms"},
	}
	for i, h := range hops {
		if h.Hop != want[i].Hop || h.Addr != want[i].Addr || h.RTT != want[i].RTT {
			t.Fatalf("第 %d 跳不匹配: got %+v, want %+v", i, h, want[i])
		}
	}
}

func TestTracerouteParseEmpty(t *testing.T) {
	if hops := parseTraceroute(""); len(hops) != 0 {
		t.Fatalf("空输出应无跳, got %+v", hops)
	}
}

func TestTracerouteParseHeadersOnly(t *testing.T) {
	out := `traceroute to 8.8.8.8 (8.8.8.8), 30 hops max, 60 byte packets
Trace complete.`
	if hops := parseTraceroute(out); len(hops) != 0 {
		t.Fatalf("仅头部应无跳, got %+v", hops)
	}
}

func TestTracerouteProbeSuccess(t *testing.T) {
	probe := &TracerouteProbe{
		RunCmd: func(ctx context.Context, host string) ([]byte, error) {
			if host != "8.8.8.8" {
				t.Fatalf("应把目标主机传给命令, got %q", host)
			}
			return []byte(tracerouteSample), nil
		},
	}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeTraceroute, Target: "8.8.8.8"}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("应成功, got %+v", res)
	}
	if res.Error != "" {
		t.Fatalf("成功结果不应有 Error, got %q", res.Error)
	}
	detail, ok := res.Detail.(map[string]interface{})
	if !ok {
		t.Fatalf("Detail 应为 map, got %T", res.Detail)
	}
	hops, ok := detail["hops"].([]Hop)
	if !ok || len(hops) != 3 {
		t.Fatalf("Detail.hops 应为 3 跳, got %#v", detail["hops"])
	}
	if hops[0].Hop != 1 || hops[0].Addr != "192.168.1.1" {
		t.Fatalf("第 1 跳不匹配, got %+v", hops[0])
	}
	if raw, ok := detail["raw"].(string); !ok || raw != tracerouteSample {
		t.Fatalf("Detail.raw 应保留原始输出, got %#v", detail["raw"])
	}
}

func TestTracerouteProbeCmdMissing(t *testing.T) {
	probe := &TracerouteProbe{
		RunCmd: func(ctx context.Context, host string) ([]byte, error) {
			return nil, &exec.Error{Name: "traceroute", Err: exec.ErrNotFound}
		},
	}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeTraceroute, Target: "8.8.8.8"}, PathDirect, nil)
	if res.Status != StatusFail {
		t.Fatalf("命令缺失应标记 fail, got %+v", res)
	}
	if !strings.Contains(res.Error, "未安装") {
		t.Fatalf("错误信息应含「未安装」, got %q", res.Error)
	}
}

func TestTracerouteProbeTimeout(t *testing.T) {
	// RunCmd 阻塞到上下文取消后才返回错误，探测应把超时表现为 timeout。
	probe := &TracerouteProbe{
		RunCmd: func(ctx context.Context, host string) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	pctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	res := probe.Run(pctx, DiagnosticTarget{Type: TypeTraceroute, Target: "8.8.8.8"}, PathDirect, nil)
	if res.Status != StatusTimeout {
		t.Fatalf("应超时, got %+v", res)
	}
}
