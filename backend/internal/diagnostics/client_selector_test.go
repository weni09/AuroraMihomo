package diagnostics

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// startCONNECTProxy 启动一个本地 TCP「代理」：接受连接、读取并记录 CONNECT
// 请求行、回 200 Connection Established。返回监听地址与已收到的 CONNECT
// 请求行通道（channel 带缓冲，读不读都不阻塞代理回包）。
func startCONNECTProxy(t *testing.T) (addr string, gotConnect <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	ch := make(chan string, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				var sb strings.Builder
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					sb.WriteString(line)
					if strings.TrimSpace(line) == "" {
						break
					}
				}
				ch <- strings.TrimSpace(strings.SplitN(sb.String(), "\r\n", 2)[0])
				_, _ = io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
			}(conn)
		}
	}()
	return ln.Addr().String(), ch
}

func TestDefaultClientSelectorDirectGuarded(t *testing.T) {
	sel := defaultClientSelector(func() string { return "" })
	direct := sel(PathDirect)
	tr, ok := direct.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("direct client 应为 *http.Transport, got %T", direct.Transport)
	}
	// 直连 client 必须带 guardedDialContext：拨号 metadata IP 字面量在
	// 建连前即被拦截（DNS 复验在 IP 字面量分支同样生效）。
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := tr.DialContext(ctx, "tcp", "169.254.169.254:80")
	if err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("direct client 应拦截 metadata 地址, got %v", err)
	}
}

func TestDefaultClientSelectorProxyConfigured(t *testing.T) {
	sel := defaultClientSelector(func() string { return "http://127.0.0.1:1" })
	client := sel(PathProxy)
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("proxy client 应为 *http.Transport, got %T", client.Transport)
	}
	if tr.Proxy == nil {
		t.Fatal("proxy client 应配置 Proxy")
	}
	if got := proxyAddrOf(client); got != "127.0.0.1:1" {
		t.Fatalf("代理地址应提取为 127.0.0.1:1, got %q", got)
	}
	// proxy client 同样带 guardedDialContext：与直连路径同款 SSRF 防线。
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := tr.DialContext(ctx, "tcp", "169.254.169.254:80")
	if err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("proxy client 应拦截 metadata 地址, got %v", err)
	}
}

func TestDefaultClientSelectorProxyFallback(t *testing.T) {
	// 代理地址为空：proxy 路径应回落直连 client（无 Proxy），不 panic。
	sel := defaultClientSelector(func() string { return "" })
	client := sel(PathProxy)
	tr, ok := client.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatalf("代理不可用时 proxy 路径应回落直连 client, got %#v", client)
	}
}

func TestHTTPProbeProxyPathViaProxy(t *testing.T) {
	// 目标域名 target.invalid 永不解析：若请求未走代理必然 DNS 失败。
	// 代理服务器直接回 200 并记录收到请求——成功且 proxied=true 证明
	// 流量真实经代理转发，而非探测忽略 path 走直连。
	var mu sync.Mutex
	proxied := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		proxied = true
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	probe := &HTTPProbe{Selector: defaultClientSelector(func() string { return proxy.URL })}
	res := probe.Run(context.Background(), DiagnosticTarget{Type: TypeHTTP, Target: "http://target.invalid/"}, PathProxy, nil)
	if res.Status != StatusSuccess {
		t.Fatalf("经代理应成功, got %+v", res)
	}
	mu.Lock()
	ok := proxied
	mu.Unlock()
	if !ok {
		t.Fatal("请求应经代理服务器转发")
	}
}
