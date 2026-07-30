package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SQLite 驱动必须保持纯 Go（不依赖 CGO）。
//
// 官方 gorm.io/driver/sqlite 底层是 mattn/go-sqlite3，必须开 CGO：
// 关掉能编译通过，但运行时直接报 "requires cgo to work. This is a stub"。
// 而 CGO 会让发布流程为五个目标平台各备一套 C 工具链（Alpine 还要区分
// musl），代价很高。换成纯 Go 后 CGO_ENABLED=0 即可全平台交叉编译。
//
// 固化成测试是为了防止后来者"顺手"换回官方 driver 或引入其它 CGO 依赖——
// 那类改动在本机（CGO 默认开启）测不出问题，只会在 CI 或交叉编译时爆。
func TestSQLiteDriverStaysPureGo(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("读 go.mod 失败: %v", err)
	}
	mod := string(data)

	// 这些依赖强制 CGO，出现即视为回归
	for _, banned := range []string{
		"github.com/mattn/go-sqlite3",
		"gorm.io/driver/sqlite",
	} {
		if strings.Contains(mod, banned) {
			t.Errorf("go.mod 引入了需要 CGO 的 %s，会破坏无 CGO 交叉编译。"+
				"SQLite 请用 github.com/libtnb/sqlite", banned)
		}
	}

	if !strings.Contains(mod, "github.com/libtnb/sqlite") {
		t.Error("go.mod 里没有 github.com/libtnb/sqlite，纯 Go SQLite 驱动缺失")
	}
}

// modernc.org/libc 必须与 modernc.org/sqlite 声明的版本一致，
// 这是上游的明确要求（issue #177）。单独升 libc 会导致运行期异常，
// 而这种问题不会在编译期暴露。
func TestModerncLibcVersionPinned(t *testing.T) {
	root := findRepoRoot(t)
	ourMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("读 go.mod 失败: %v", err)
	}
	ourLibc := extractModVersion(string(ourMod), "modernc.org/libc")
	if ourLibc == "" {
		t.Skip("未使用 modernc.org/libc")
	}

	sqliteVer := extractModVersion(string(ourMod), "modernc.org/sqlite")
	if sqliteVer == "" {
		t.Skip("未使用 modernc.org/sqlite")
	}

	// 从 module cache 里读 sqlite 自己声明的 libc 版本
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("无法定位 GOPATH")
		}
		gopath = filepath.Join(home, "go")
	}
	upstream := filepath.Join(gopath, "pkg", "mod", "modernc.org",
		"sqlite@"+sqliteVer, "go.mod")
	b, err := os.ReadFile(upstream)
	if err != nil {
		t.Skipf("module cache 中无 %s，跳过", upstream)
	}
	want := extractModVersion(string(b), "modernc.org/libc")
	if want == "" {
		t.Skip("上游未声明 libc 版本")
	}
	if ourLibc != want {
		t.Errorf("modernc.org/libc 版本不匹配：本项目 %s，"+
			"modernc.org/sqlite %s 要求 %s。"+
			"两者必须一致，否则可能出现运行期异常（上游 issue #177）",
			ourLibc, sqliteVer, want)
	}
}

// extractModVersion 从 go.mod 文本里取指定模块的版本号
func extractModVersion(mod, module string) string {
	for _, line := range strings.Split(mod, "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) >= 2 && f[0] == module {
			return f[1]
		}
	}
	return ""
}

// findRepoRoot 从当前包向上找到含 go.mod 的目录
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("未找到仓库根目录（go.mod）")
	return ""
}
