package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auroramihomo/backend/internal/model"
)

// serveTexts 起一个按路径返回固定文本的服务器，路径为 "/0"、"/1" …
// 值为空串表示该地址返回 500，用于验证失败策略。
func serveTexts(t *testing.T, bodies ...string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for i, b := range bodies {
		body := b
		mux.HandleFunc("/"+itoa(i), func(w http.ResponseWriter, _ *http.Request) {
			if body == "" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(body))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func itoa(i int) string {
	return string(rune('0' + i))
}

func urlsFor(srv *httptest.Server, n int) string {
	lines := make([]string, 0, n)
	for i := 0; i < n; i++ {
		lines = append(lines, srv.URL+"/"+itoa(i))
	}
	return strings.Join(lines, "\n")
}

func TestResolveLocalOnly(t *testing.T) {
	r := NewFileContentResolver()
	f := &model.SubFile{Name: "f", Content: "local-body", SourceMode: model.FileSourceLocal}

	got, err := r.Resolve(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "local-body" {
		t.Fatalf("expected local content, got %q", got.Content)
	}
}

// 存量文件没有 SourceMode，其正文就是编辑器里的内容，必须仍按本地解释
func TestResolveEmptySourceModeTreatedAsLocal(t *testing.T) {
	r := NewFileContentResolver()
	f := &model.SubFile{Name: "legacy", Content: "old-body"}

	got, err := r.Resolve(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "old-body" {
		t.Fatalf("存量文件应按本地内容输出，got %q", got.Content)
	}
}

// 多个远程地址必须按配置顺序拼接，不能按返回快慢排列：
// 规则片段与配置模板都对顺序敏感。
func TestResolveRemoteKeepsConfiguredOrder(t *testing.T) {
	srv := serveTexts(t, "first", "second", "third")
	r := NewFileContentResolver()
	f := &model.SubFile{
		Name:       "f",
		SourceMode: model.FileSourceRemote,
		SyncURL:    urlsFor(srv, 3),
	}

	got, err := r.Resolve(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "first\nsecond\nthird" {
		t.Fatalf("远程内容顺序错误: %q", got.Content)
	}
}

// 纯远程模式不应带入本地正文
func TestResolveRemoteIgnoresLocalWhenNotMerging(t *testing.T) {
	srv := serveTexts(t, "remote-body")
	r := NewFileContentResolver()
	f := &model.SubFile{
		Name:       "f",
		Content:    "local-body",
		SourceMode: model.FileSourceRemote,
		SyncURL:    srv.URL + "/0",
	}

	got, err := r.Resolve(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Content, "local-body") {
		t.Fatalf("不合并时不应带入本地正文: %q", got.Content)
	}
}

func TestResolveMergeLocalFirst(t *testing.T) {
	srv := serveTexts(t, "R1", "R2")
	r := NewFileContentResolver()
	f := &model.SubFile{
		Name:         "f",
		Content:      "L",
		SourceMode:   model.FileSourceLocal,
		SyncURL:      urlsFor(srv, 2),
		MergeSources: model.FileMergeLocalFirst,
	}

	got, err := r.Resolve(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "L\nR1\nR2" {
		t.Fatalf("localFirst 顺序错误: %q", got.Content)
	}
}

func TestResolveMergeRemoteFirst(t *testing.T) {
	srv := serveTexts(t, "R1", "R2")
	r := NewFileContentResolver()
	f := &model.SubFile{
		Name:         "f",
		Content:      "L",
		SourceMode:   model.FileSourceLocal,
		SyncURL:      urlsFor(srv, 2),
		MergeSources: model.FileMergeRemoteFirst,
	}

	got, err := r.Resolve(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "R1\nR2\nL" {
		t.Fatalf("remoteFirst 顺序错误: %q", got.Content)
	}
}

// 默认从严：任一地址失败即报错。静默产出缺内容的文件会让客户端
// 拿到不完整配置而不自知。
func TestResolveFailStrictReturnsError(t *testing.T) {
	srv := serveTexts(t, "ok", "") // 第二个地址返回 500
	r := NewFileContentResolver()
	f := &model.SubFile{
		Name:       "f",
		SourceMode: model.FileSourceRemote,
		SyncURL:    urlsFor(srv, 2),
	}

	if _, err := r.Resolve(context.Background(), f); err == nil {
		t.Fatal("默认策略下单个地址失败应报错")
	}
}

func TestResolveFailSkipReportsWarning(t *testing.T) {
	srv := serveTexts(t, "ok", "")
	r := NewFileContentResolver()
	f := &model.SubFile{
		Name:               "f",
		SourceMode:         model.FileSourceRemote,
		SyncURL:            urlsFor(srv, 2),
		IgnoreFailedRemote: model.FileFailSkip,
	}

	got, err := r.Resolve(context.Background(), f)
	if err != nil {
		t.Fatalf("跳过策略下不应报错: %v", err)
	}
	if got.Content != "ok" {
		t.Fatalf("应保留成功的那一段: %q", got.Content)
	}
	if len(got.Warnings) != 1 {
		t.Fatalf("跳过应产生一条提示，实际 %d 条", len(got.Warnings))
	}
}

func TestResolveFailQuietIsSilent(t *testing.T) {
	srv := serveTexts(t, "ok", "")
	r := NewFileContentResolver()
	f := &model.SubFile{
		Name:               "f",
		SourceMode:         model.FileSourceRemote,
		SyncURL:            urlsFor(srv, 2),
		IgnoreFailedRemote: model.FileFailQuiet,
	}

	got, err := r.Resolve(context.Background(), f)
	if err != nil {
		t.Fatalf("静默策略下不应报错: %v", err)
	}
	if got.Content != "ok" {
		t.Fatalf("应保留成功的那一段: %q", got.Content)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("静默策略不应产生提示，实际 %d 条", len(got.Warnings))
	}
}

// 全部地址都失败时，即便配了跳过策略也不能算成功：
// 那只会产出一个空文件，把原有内容覆盖掉。
func TestResolveAllRemoteFailedIsError(t *testing.T) {
	srv := serveTexts(t, "", "")
	for _, strategy := range []string{model.FileFailSkip, model.FileFailQuiet} {
		f := &model.SubFile{
			Name:               "f",
			SourceMode:         model.FileSourceRemote,
			SyncURL:            urlsFor(srv, 2),
			IgnoreFailedRemote: strategy,
		}
		if _, err := NewFileContentResolver().Resolve(context.Background(), f); err == nil {
			t.Fatalf("策略 %q 下全部失败仍应报错", strategy)
		}
	}
}

func TestResolveRemoteWithoutURLIsError(t *testing.T) {
	f := &model.SubFile{Name: "f", SourceMode: model.FileSourceRemote}
	if _, err := NewFileContentResolver().Resolve(context.Background(), f); err == nil {
		t.Fatal("远程来源未填地址应报错")
	}
}

func TestSplitFileURLs(t *testing.T) {
	got := SplitFileURLs("  https://a.com/x \n\n# 暂时停用\nhttps://b.com/y\r\n  \n")
	want := []string{"https://a.com/x", "https://b.com/y"}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

// 上游文件末尾常无换行，段间必须补换行，否则前一段的末行会与
// 后一段的首行粘成一行。
func TestJoinFilePartsSeparatesLines(t *testing.T) {
	got := joinFileParts([]string{"a: 1", "  ", "b: 2\n"})
	// 最后一段原样保留，其尾部换行不被吃掉
	if got != "a: 1\nb: 2\n" {
		t.Fatalf("拼接结果错误: %q", got)
	}
	// 前段自带尾换行时不应多出空行
	if got := joinFileParts([]string{"a: 1\n", "b: 2"}); got != "a: 1\nb: 2" {
		t.Fatalf("段间不应多出空行: %q", got)
	}
}

// 单段必须逐字输出：纯本地文件走的就是这条路径，
// 末尾换行属于文件内容，擅自去掉就不是「原样输出」了。
func TestJoinFilePartsSinglePartVerbatim(t *testing.T) {
	raw := "payload:\n  - DOMAIN-SUFFIX,example.com\n"
	if got := joinFileParts([]string{raw}); got != raw {
		t.Fatalf("单段应逐字返回\n want: %q\n got:  %q", raw, got)
	}
}
