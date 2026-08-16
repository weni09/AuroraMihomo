package diagnostics

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockDNSServer 是一个最小 UDP DNS 响应器：对 "nx.test" 返回 NXDOMAIN，
// 其余名称按查询类型返回固定的 A/AAAA 记录（文档保留地址，不依赖外网）。
type mockDNSServer struct {
	conn *net.UDPConn
	addr string

	mu       sync.Mutex
	lastName string // 最近一次收到的查询名（供诊断/断言，见 lastQueryName）
}

// lastQueryName 返回 mock 最近收到的查询名，供测试日志输出或断言，
// 便于排查 CI 与本地环境对解析器查询名格式的差异。
func (s *mockDNSServer) lastQueryName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastName
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
		if resp := s.buildDNSResponse(buf[:n]); resp != nil {
			s.conn.WriteToUDP(resp, from)
		}
	}
}

// buildDNSResponse 构造最小 DNS 响应：回显查询的 ID 与问题段，
// 按查询类型回填固定记录（A=192.0.2.10，AAAA=2001:db8::10）。
func (s *mockDNSServer) buildDNSResponse(query []byte) []byte {
	if len(query) < 17 { // 12 字节头 + 问题段（根名 1 + qtype 2 + qclass 2）
		return nil
	}
	name, end, ok := parseQName(query[12:])
	if !ok || len(query) < 12+end+4 {
		return nil
	}
	// 记录实际收到的查询名：Go 解析器在 Linux 上会追加 resolv.conf 的
	// search 域（如 nx.test.<search>），也可能保留尾点——记下来供断言/日志。
	s.mu.Lock()
	s.lastName = name
	s.mu.Unlock()

	qtype := binary.BigEndian.Uint16(query[12+end:])
	question := query[12 : 12+end+4]

	rcode := uint16(0)
	anCount := uint16(1)
	var answer []byte
	clean := strings.TrimSuffix(name, ".")
	switch {
	case strings.Contains(name, "nx") || clean == "nx.test":
		// 主匹配：去尾点后精确等于 nx.test。测试目标已改为带尾点的 FQDN
		// 「nx.test.」——Go 视根化名为绝对名，只查询该名，天然绕过 search 域。
		// 兜底匹配：任何含 "nx" 的查询名一律判 NXDOMAIN。本套件成功用例
		// 只查 a.test/slow.test，不含 "nx"，绝无歧义。
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
	// 注入自定义 resolver 时应标注 resolver=custom
	if rv, _ := detail["resolver"].(string); rv != "custom" {
		t.Fatalf("注入 resolver 应标注 custom, got %v", detail["resolver"])
	}
}

func TestDNSProbeSystemResolverDetail(t *testing.T) {
	// p.Resolver 为 nil 时使用系统默认 resolver：Detail 应标注 resolver=system。
	// 临时替换 net.DefaultResolver 为本地 mock，避免测试依赖真实 DNS
	// （包内测试无 t.Parallel，顺序执行，替换安全）。
	s := startMockDNSServer(t)
	old := net.DefaultResolver
	net.DefaultResolver = mockResolver(t, s)
	defer func() { net.DefaultResolver = old }()

	probe := &DNSProbe{} // Resolver 为 nil → 系统默认
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeDNS, Target: "a.test"}, PathDirect, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("应成功, got %+v", res)
	}
	detail, ok := res.Detail.(map[string]interface{})
	if !ok {
		t.Fatalf("Detail 应为 map, got %T", res.Detail)
	}
	if rv, _ := detail["resolver"].(string); rv != "system" {
		t.Fatalf("系统默认解析器应标注 system, got %v", detail["resolver"])
	}
}

func TestDNSProbeNXDomain(t *testing.T) {
	s := startMockDNSServer(t)
	probe := &DNSProbe{Resolver: mockResolver(t, s)}
	// 带尾点的 FQDN：Go 解析器视其为根化名（rooted），只查询该名本身，
	// 不追加 resolv.conf 的 search 域，CI 与本地行为一致。
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeDNS, Target: "nx.test."}, PathDirect, nil)
	// 记录 mock 实际收到的查询名，便于未来排查 CI 环境差异。
	t.Logf("mock 收到查询名: %q", s.lastQueryName())
	if res.Status != StatusFail {
		t.Fatalf("NXDOMAIN 应标记 fail, got %+v", res)
	}
	if res.Error == "" {
		t.Fatal("失败结果应包含错误信息")
	}
}

func TestDNSProbeProxyPathNotesSameAsDirect(t *testing.T) {
	// DNS 查询不经 HTTP 代理：proxy 路径结果应与 direct 一致，并在成功
	// Detail 中如实标注，避免前端把直出结果误读为代理路径数据。
	s := startMockDNSServer(t)
	probe := &DNSProbe{Resolver: mockResolver(t, s)}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeDNS, Target: "a.test"}, PathProxy, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("应成功, got %+v", res)
	}
	detail, ok := res.Detail.(map[string]interface{})
	if !ok {
		t.Fatalf("Detail 应为 map, got %T", res.Detail)
	}
	note, ok := detail["note"].(string)
	if !ok || !strings.Contains(note, "不经") {
		t.Fatalf("proxy 路径应标注 DNS 不经代理, got %+v", detail)
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
