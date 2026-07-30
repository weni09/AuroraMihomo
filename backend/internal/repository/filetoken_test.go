package repository

import (
	"path/filepath"
	"testing"

	"auroramihomo/backend/internal/model"
)

func newFileTokenDB(t *testing.T) *Database {
	t.Helper()
	db, err := NewDatabase(filepath.Join(t.TempDir(), "filetoken.db"))
	if err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// 核心回归：文件直链必须按不可枚举的 token 寻址，
// 不能再按用户可猜测的文件名寻址。
func TestGetFileByToken(t *testing.T) {
	db := newFileTokenDB(t)

	f := &model.SubFile{Name: "rules.yaml", Content: "payload", ShareToken: "abc123"}
	if err := db.SaveFile(f); err != nil {
		t.Fatalf("保存文件失败: %v", err)
	}

	got, err := db.GetFileByToken("abc123")
	if err != nil {
		t.Fatalf("按 token 查询应成功: %v", err)
	}
	if got.Content != "payload" {
		t.Fatalf("内容不符，实际 %q", got.Content)
	}
}

// 空 token 不得命中任何记录，否则未带凭据的请求会读到数据。
// 这一点尤其关键：历史记录的 share_token 可能为空串。
func TestGetFileByTokenRejectsEmpty(t *testing.T) {
	db := newFileTokenDB(t)

	// 故意写入一条 token 为空的记录，模拟未补齐的历史数据
	f := &model.SubFile{Name: "legacy.yaml", Content: "secret", ShareToken: ""}
	if err := db.SaveFile(f); err != nil {
		t.Fatalf("保存文件失败: %v", err)
	}

	for _, token := range []string{"", "   "} {
		if _, err := db.GetFileByToken(token); err == nil {
			t.Fatalf("空 token(%q) 不应命中任何文件", token)
		}
	}
}

func TestGetFileByTokenNotFound(t *testing.T) {
	db := newFileTokenDB(t)
	if _, err := db.GetFileByToken("nonexistent"); err == nil {
		t.Fatal("不存在的 token 应返回错误")
	}
}

// 升级路径：老版本的文件没有 share_token，必须能被补齐，
// 否则这些文件的直链在升级后永久失效。
func TestBackfillFileShareTokens(t *testing.T) {
	db := newFileTokenDB(t)

	for _, name := range []string{"a.yaml", "b.yaml"} {
		if err := db.SaveFile(&model.SubFile{Name: name, Content: "x"}); err != nil {
			t.Fatalf("保存文件失败: %v", err)
		}
	}
	// 已有 token 的记录不应被改写
	if err := db.SaveFile(&model.SubFile{Name: "c.yaml", Content: "x", ShareToken: "keepme"}); err != nil {
		t.Fatalf("保存文件失败: %v", err)
	}

	seq := 0
	gen := func() string {
		seq++
		return "generated-" + string(rune('0'+seq))
	}
	if err := db.BackfillFileShareTokens(gen); err != nil {
		t.Fatalf("补齐 token 失败: %v", err)
	}

	files, err := db.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.ShareToken == "" {
			t.Fatalf("文件 %s 的 token 未被补齐", f.Name)
		}
		if f.Name == "c.yaml" && f.ShareToken != "keepme" {
			t.Fatalf("已有 token 不应被改写，实际 %q", f.ShareToken)
		}
	}

	// 幂等：再次执行不应改动任何已有 token
	before := map[string]string{}
	for _, f := range files {
		before[f.Name] = f.ShareToken
	}
	if err := db.BackfillFileShareTokens(gen); err != nil {
		t.Fatalf("再次补齐失败: %v", err)
	}
	after, err := db.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range after {
		if before[f.Name] != f.ShareToken {
			t.Fatalf("文件 %s 的 token 被重复改写：%q -> %q", f.Name, before[f.Name], f.ShareToken)
		}
	}
}
