package fetcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseUserInfo(t *testing.T) {
	info := ParseUserInfo("upload=1024; download=2048; total=107374182400; expire=1740000000")
	if info.Upload != 1024 || info.Download != 2048 {
		t.Fatalf("上传下载解析错误: %+v", info)
	}
	if info.Total != 107374182400 || info.Expire != 1740000000 {
		t.Fatalf("总量或到期解析错误: %+v", info)
	}
	if info.Used() != 3072 {
		t.Fatalf("已用流量应为 3072，实际 %d", info.Used())
	}
	if info.IsZero() {
		t.Fatal("有数据时 IsZero 不应为真")
	}
}

// 缺字段、含空格、非法值都不应导致解析失败
func TestParseUserInfoTolerant(t *testing.T) {
	info := ParseUserInfo("  upload=100 ;  total = 200 ; junk ; expire=abc")
	if info.Upload != 100 || info.Total != 200 {
		t.Fatalf("宽松解析失败: %+v", info)
	}
	if info.Expire != 0 {
		t.Fatalf("非法 expire 应被忽略，实际 %d", info.Expire)
	}
	if !ParseUserInfo("").IsZero() {
		t.Fatal("空串应解析为零值")
	}
}

func TestFetchWithMetaReadsHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("subscription-userinfo", "upload=1; download=2; total=3; expire=4")
		_, _ = w.Write([]byte("ss://YWVzLTI1Ni1nY206cHc=@1.1.1.1:8388#n1\n"))
	}))
	defer srv.Close()

	body, info, err := New(0).FetchWithMeta(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("响应体为空")
	}
	if info.Upload != 1 || info.Download != 2 || info.Total != 3 || info.Expire != 4 {
		t.Fatalf("流量头未被读取: %+v", info)
	}
}
