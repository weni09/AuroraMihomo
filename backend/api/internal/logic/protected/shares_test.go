package protected

import (
	"strings"
	"testing"
	"time"
)

// 有效期入参需同时接受 RFC3339 与浏览器 datetime-local 控件的格式。
// 后者不带时区，必须按本地时区解释——按 UTC 解释会让用户设的
// 「今天 18:00 过期」在东八区变成凌晨 2 点，链接提前失效。
func TestParseExpiry(t *testing.T) {
	// 空串表示永不过期
	got, err := parseExpiry("")
	if err != nil {
		t.Fatalf("空串应合法: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("空串应解析为零值，实际 %v", got)
	}

	// 仅空白也视为永不过期
	if got, err := parseExpiry("   "); err != nil || !got.IsZero() {
		t.Errorf("空白串应解析为零值，实际 %v (err=%v)", got, err)
	}

	// RFC3339
	want := time.Date(2026, 8, 1, 10, 30, 0, 0, time.UTC)
	got, err = parseExpiry("2026-08-01T10:30:00Z")
	if err != nil {
		t.Fatalf("RFC3339 应可解析: %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("RFC3339 解析错误\n want %v\n got  %v", want, got)
	}

	// datetime-local（无时区），应按本地时区解释
	got, err = parseExpiry("2026-08-01T18:00")
	if err != nil {
		t.Fatalf("datetime-local 应可解析: %v", err)
	}
	wantLocal := time.Date(2026, 8, 1, 18, 0, 0, 0, time.Local)
	if !got.Equal(wantLocal) {
		t.Errorf("datetime-local 应按本地时区解析\n want %v\n got  %v", wantLocal, got)
	}

	// 带秒的 datetime-local
	got, err = parseExpiry("2026-08-01T18:00:30")
	if err != nil {
		t.Fatalf("带秒格式应可解析: %v", err)
	}
	if got.Second() != 30 {
		t.Errorf("秒未被解析，实际 %v", got)
	}

	// 非法输入必须报错，而不是静默当成永不过期
	// （静默会让用户以为设了有效期，实际链接永久有效）
	for _, bad := range []string{"not-a-time", "2026/08/01", "18:00"} {
		if _, err := parseExpiry(bad); err == nil {
			t.Errorf("非法输入 %q 应报错", bad)
		}
	}
}

// 分享链接必须在无凭据时返回空串，避免前端拼出 /api/v1/share/ 这类
// 尾部为空的地址——那会被后端当成空 token 请求。
func TestShareURLEmptyWhenNoToken(t *testing.T) {
	if got := shareURL(""); got != "" {
		t.Errorf("无 token 时应返回空串，实际 %q", got)
	}
	if got := shareURL("   "); got != "" {
		t.Errorf("空白 token 应返回空串，实际 %q", got)
	}
	if got := shareURL("abc123"); got != "/api/v1/share/abc123" {
		t.Errorf("分享地址拼接错误，实际 %q", got)
	}
	if got := fileURL(""); got != "" {
		t.Errorf("无 token 时文件地址应为空串，实际 %q", got)
	}
	if got := fileURL("abc123"); got != "/api/v1/file/abc123" {
		t.Errorf("文件地址拼接错误，实际 %q", got)
	}
}

func TestExpiredHelper(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	if expired(time.Time{}, now) {
		t.Error("零值应视为永不过期")
	}
	if !expired(now.Add(-time.Second), now) {
		t.Error("早于当前时间应判定为过期")
	}
	if expired(now.Add(time.Second), now) {
		t.Error("晚于当前时间不应判定为过期")
	}
}

// randomToken 必须产出足够长度且互不相同的凭据。
// 此前实现丢弃了 rand.Read 的错误，随机源异常时会返回全零 token。
// 分享凭据统一为 shareTokenBytes（16）→ 32 位十六进制 / 128 bit。
func TestRandomTokenUniqueAndSized(t *testing.T) {
	if shareTokenBytes != 16 {
		t.Fatalf("shareTokenBytes 应为 16（128 bit），实际 %d", shareTokenBytes)
	}
	seen := map[string]bool{}
	wantLen := shareTokenBytes * 2 // hex
	for i := 0; i < 50; i++ {
		tok, err := randomToken(shareTokenBytes)
		if err != nil {
			t.Fatalf("生成凭据失败: %v", err)
		}
		if len(tok) != wantLen {
			t.Fatalf("凭据长度应为 %d，实际 %d (%q)", wantLen, len(tok), tok)
		}
		if tok == strings.Repeat("0", wantLen) {
			t.Fatal("凭据不应为全零（随机源失效的征兆）")
		}
		if seen[tok] {
			t.Fatalf("凭据重复: %s", tok)
		}
		seen[tok] = true
	}
}
