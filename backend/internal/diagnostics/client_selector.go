package diagnostics

import (
	"net/http"
	"time"

	"auroramihomo/backend/internal/fetcher"
)

// dialTimeout 单次 TCP 建连超时，与订阅拉取同款（fetcher 内部 10s）：
// 目标不可达时快速失败，而不是拖满整个探测。
const dialTimeout = 10 * time.Second

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
