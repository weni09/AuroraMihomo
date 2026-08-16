package diagnostics

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

// mockDNSServer 是一个最小 UDP DNS 响应器：对 "nx.test" 返回 NXDOMAIN，
// 其余名称按查询类型返回固定的 A/AAAA 记录（文档保留地址，不依赖外网）。
type mockDNSServer struct {
	conn *net.UDPConn
	addr string
}

// startMockDNSServer 启动 mock DNS 服务器，测试结束时自动关闭。
func startMockDNSServer(t *testing.T) *mockDNSServer {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("监听 UDP 失败: %v", err)
	}
	s := &mockDNSServer{conn: conn, addr: conn.LocalAddr().String()}
	go s.serve()
	t.Cleanup(func() { conn.Close() })
	return s
}

func (s *mockDNSServer) serve() {
	buf := make([]byte, 512)
	for {
		n, from, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return // 连接关闭
		}
		if resp := buildDNSResponse(buf[:n]); resp != nil {
			s.conn.WriteToUDP(resp, from)
		}
	}
}

// buildDNSResponse 构造最小 DNS 响应：回显查询的 ID 与问题段，
// 按查询类型回填固定记录（A=192.0.2.10，AAAA=2001:db8::10）。
func buildDNSResponse(query []byte) []byte {
	if len(query) < 17 { // 12 字节头 + 问题段（根名 1 + qtype 2 + qclass 2）
		return nil
	}
	name, end, ok := parseQName(query[12:])
	if !ok || len(query) < 12+end+4 {
		return nil
	}
	qtype := binary.BigEndian.Uint16(query[12+end:])
	question := query[12 : 12+end+4]

	rcode := uint16(0)
	anCount := uint16(1)
	var answer []byte
	switch {
	case name == "nx.test":
		rcode = 3 // NXDOMAIN
		anCount = 0
	case qtype == 28: // AAAA
		answer = append(answer, 0xC0, 0x0C, 0x00, 0x1C, 0x00, 0x01, 0x00, 0x00, 0x00, 0x3C, 0x00, 0x10)
		answer = append(answer, 0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10) // 2001:db8::10
	default: // A
		answer = append(answer, 0xC0, 0x0C, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x3C, 0x00, 0x04)
		answer = append(answer, 192, 0, 2, 10) // 192.0.2.10
	}

	flags := uint16(0x8180) | rcode // QR|RD|RA
	resp := make([]byte, 0, 12+len(question)+len(answer))
	resp = append(resp, query[0:2]...)
	resp = append(resp, byte(flags>>8), byte(flags))
	resp = append(resp, 0x00, 0x01, byte(anCount>>8), byte(anCount), 0x00, 0x00, 0x00, 0x00)
	resp = append(resp, question...)
	resp = append(resp, answer...)
	return resp
}

// parseQName 解析 DNS 名字标签，返回点分字符串与消耗的字节数。
func parseQName(q []byte) (string, int, bool) {
	var sb strings.Builder
	i := 0
	for i < len(q) {
		l := int(q[i])
		if l == 0 {
			return sb.String(), i + 1, true
		}
		if l&0xC0 != 0 || i+1+l > len(q) {
			return "", 0, false // 压缩指针或越界，查询中不应出现
		}
		if sb.Len() > 0 {
			sb.WriteByte('.')
		}
		sb.Write(q[i+1 : i+1+l])
		i += 1 + l
	}
	return "", 0, false
}

// mockResolver 返回指向本地 mock DNS 服务器的 Resolver。
func mockResolver(t *testing.T, s *mockDNSServer) *net.Resolver {
	t.Helper()
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", s.addr)
		},
	}
}

func TestDNSProbeSuccess(t *testing.T) {
	s := startMockDNSServer(t)
	probe := &DNSProbe{Resolver: mockResolver(t, s)}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeDNS, Target: "a.test"}, PathDirect, nil)
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
	records, ok := detail["records"].([]string)
	if !ok {
		t.Fatalf("records 应为 []string, got %T", detail["records"])
	}
	have := map[string]bool{}
	for _, r := range records {
		have[r] = true
	}
	if !have["192.0.2.10"] || !have["2001:db8::10"] {
		t.Fatalf("records 应包含 mock 返回的 A/AAAA 记录, got %v", records)
	}
}

func TestDNSProbeNXDomain(t *testing.T) {
	s := startMockDNSServer(t)
	probe := &DNSProbe{Resolver: mockResolver(t, s)}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeDNS, Target: "nx.test"}, PathDirect, nil)
	if res.Status != StatusFail {
		t.Fatalf("NXDOMAIN 应标记 fail, got %+v", res)
	}
	if res.Error == "" {
		t.Fatal("失败结果应包含错误信息")
	}
}

func TestDNSProbeTimeout(t *testing.T) {
	// Dial 挂起直到 ctx 截止：LookupIPAddr 必然在极短超时后失败
	probe := &DNSProbe{
		Timeout: 50 * time.Millisecond,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
	}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeDNS, Target: "slow.test"}, PathDirect, nil)
	if res.Status != StatusTimeout {
		t.Fatalf("超时应标记 timeout, got %+v", res)
	}
	if res.Error == "" {
		t.Fatal("超时结果应包含错误信息")
	}
}
