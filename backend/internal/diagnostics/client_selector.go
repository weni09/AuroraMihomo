package diagnostics

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"auroramihomo/backend/internal/fetcher"
)

// dialTimeout 单次 TCP 建连超时，与订阅拉取同款（fetcher 内部 10s）：
// 目标不可达时快速失败，而不是拖满整个探测。
const dialTimeout = 10 * time.Second

// ClientSelector 按出网路径返回 http.Client。
//
// direct 返回直连 client；proxy 返回经 mihomo 代理的 client（代理地址不可用
// 或解析失败时回落直连 client）。两条路径的 client 都带 fetcher 同款 SSRF
// 防线：guardedDialContext（建连时 DNS 复验拦 metadata）+ checkRedirect
// （逐跳校验）。
//
// 探测器（HTTPProbe/TCPProbe/PingProbe）在 proxy 路径据此走代理而非裸直连，
// 使直连/代理对比输出真实路径的结果。
type ClientSelector func(path string) *http.Client

// defaultClientSelector 构造 Service 默认的出网客户端选择器。
// proxyURLFn 返回当前 mihomo 代理地址；nil 或空串视为代理不可用，回落直连。
func defaultClientSelector(proxyURLFn func() string) ClientSelector {
	direct := directHTTPClient(0)
	return func(path string) *http.Client {
		if path == PathProxy && proxyURLFn != nil {
			if c := proxyHTTPClient(proxyURLFn()); c != nil {
				return c
			}
		}
		return direct
	}
}

// directHTTPClient 构造直连 http.Client：带 fetcher 同款 guardedDialContext
// （建连时 DNS 复验拦 metadata）与 checkRedirect（逐跳校验），SSRF 防线与
// 订阅拉取完全一致（同一份实现，见 fetcher.GuardedDialContext/CheckRedirect）。
//
// timeout<=0 时 client 自身不设超时，由请求的 context 控制（探测框架总是传入
// 带超时的 ctx，见 Execute 的 TimeoutProbe 覆盖）。
func directHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: fetcher.CheckRedirect,
		Transport: &http.Transport{
			DialContext: fetcher.GuardedDialContext(dialTimeout),
		},
	}
}

// proxyHTTPClient 构造经 mihomo 代理的 http.Client；代理地址为空或解析失败时
// 返回 nil（调用方据此回落直连）。与直连 client 一样带 SSRF 防线：到代理的
// 建连同样经 guardedDialContext（代理多为本地回环，检查为无操作；域名代理
// 地址也会先做 DNS 复验），逐跳校验经 checkRedirect 保留。
func proxyHTTPClient(proxyURL string) *http.Client {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil || u.Host == "" {
		return nil
	}
	return &http.Client{
		CheckRedirect: fetcher.CheckRedirect,
		Transport: &http.Transport{
			Proxy:       http.ProxyURL(u),
			DialContext: fetcher.GuardedDialContext(dialTimeout),
		},
	}
}

// proxyAddrOf 从 http.Client 的 Transport 取 HTTP 代理地址（host:port）。
// 仅识别 *http.Transport 且设置了 Proxy 的 client；取不到返回空串
// （例如回落直连的 client 或自定义 Transport）。
func proxyAddrOf(client *http.Client) string {
	if client == nil {
		return ""
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		return ""
	}
	// Proxy 函数通常（http.ProxyURL）忽略请求参数；这里用占位请求取地址。
	u, err := tr.Proxy(&http.Request{URL: &url.URL{Scheme: "http", Host: "placeholder.invalid"}})
	if err != nil || u == nil || u.Host == "" {
		return ""
	}
	return u.Host
}

// proxyConnect 经 HTTP 代理向目标 host:port 发 CONNECT 请求建立隧道，测隧道
// 建立延迟；成功（响应行 2xx）后立即关闭连接，只测建立耗时。
//
// 为什么手写 CONNECT：http.Client 不暴露 CONNECT 原语（它只在内部为 https
// 请求发 CONNECT），而 TCP/Ping 的目标是 host:port 而非 URL。这里从 client
// 的 Transport 取代理地址，用裸 TCP 连代理并手写 CONNECT 请求——与标准代理
// 客户端（golang.org/x/net/proxy）做法一致，且不引入外部依赖。
// 目标主机名不做解析（由代理解析），保证「直连解析失败、经代理成功」的
// 对比语义真实。超时取 ctx 截止（探测框架总是带超时），无截止时用
// dialTimeout 兜底，避免连上代理却不响应的场景挂死。
func proxyConnect(ctx context.Context, client *http.Client, host string, port int) (time.Duration, error) {
	proxyAddr := proxyAddrOf(client)
	if proxyAddr == "" {
		return 0, errors.New("出网选择器未提供代理地址")
	}
	target := net.JoinHostPort(host, strconv.Itoa(port))

	deadline := time.Now().Add(dialTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}

	start := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(deadline); err != nil {
		return 0, err
	}

	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	if _, err := conn.Write([]byte(req)); err != nil {
		return 0, err
	}
	br := bufio.NewReader(conn)
	statusLine, err := br.ReadString('\n')
	if err != nil {
		return 0, err
	}
	// 形如 "HTTP/1.1 200 Connection Established"
	parts := strings.SplitN(strings.TrimSpace(statusLine), " ", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[1], "2") {
		return 0, fmt.Errorf("代理 CONNECT 被拒: %s", strings.TrimSpace(statusLine))
	}
	return time.Since(start), nil
}

// withSelector 为已知需要出网客户端的探测器（HTTP/TCP/Ping）注入
// ClientSelector。克隆实例后设置字段，避免污染调用方传入的探测 map/实例；
// 未注册类型或非内建探测器原样保留。
func withSelector(probes map[string]Probe, sel ClientSelector) map[string]Probe {
	if len(probes) == 0 || sel == nil {
		return probes
	}
	out := make(map[string]Probe, len(probes))
	for k, v := range probes {
		out[k] = v
	}
	if hp, ok := out[TypeHTTP].(*HTTPProbe); ok {
		cp := *hp
		cp.Selector = sel
		out[TypeHTTP] = &cp
	}
	if tp, ok := out[TypeTCP].(*TCPProbe); ok {
		cp := *tp
		cp.Selector = sel
		out[TypeTCP] = &cp
	}
	if pp, ok := out[TypePing].(*PingProbe); ok {
		cp := *pp
		cp.Selector = sel
		out[TypePing] = &cp
	}
	return out
}
