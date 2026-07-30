package updater

import (
	"runtime"
	"strings"
	"testing"
)

// 官方 mihomo release 的资产命名与格式，取自 v1.19.29（共 127 个资产）。
//
// 关键事实：只有 Windows 发 .zip，Linux / macOS 一律是 .gz（gzip 压缩的裸
// 二进制，不是 tar 归档）。此前 pickMihomoAsset 统一按 .zip 过滤，在 Linux
// 上匹配不到任何资产，内核根本装不上——透明代理依赖内核，这是前置阻塞项。
//
// 固化成测试，防止后续改动又把非 Windows 平台的扩展名判断丢掉。
func realisticRelease() *githubRelease {
	names := []string{
		// Linux amd64：compatible 与多个 go 版本变体
		"mihomo-linux-amd64-compatible-v1.19.29.gz",
		"mihomo-linux-amd64-v1-go120-v1.19.29.gz",
		"mihomo-linux-amd64-v1.19.29.gz",
		// 同一个 release 里还有发行版包，必须排除
		"mihomo-linux-amd64-v1-v1.19.29.deb",
		"mihomo-linux-amd64-v1.19.29.rpm",
		"mihomo-linux-amd64-v1.19.29.pkg.tar.zst",
		// Linux arm64
		"mihomo-linux-arm64-v1.19.29.gz",
		"mihomo-linux-arm64-v1.19.29.deb",
		// macOS
		"mihomo-darwin-amd64-compatible-v1.19.29.gz",
		"mihomo-darwin-arm64-v1.19.29.gz",
		// Windows 才是 zip
		"mihomo-windows-amd64-compatible-v1.19.29.zip",
		"mihomo-windows-amd64-v1.19.29.zip",
		"mihomo-windows-arm64-v1.19.29.zip",
	}
	rel := &githubRelease{TagName: "v1.19.29"}
	for i, n := range names {
		rel.Assets = append(rel.Assets, githubAsset{
			Name:               n,
			BrowserDownloadURL: "https://example.com/" + n,
			Size:               int64(1000 + i),
		})
	}
	return rel
}

func TestPickMihomoAssetUsesPlatformExtension(t *testing.T) {
	_, name, _, err := pickMihomoAsset(realisticRelease())
	if err != nil {
		t.Fatalf("当前平台 %s/%s 应能匹配到资产: %v", runtime.GOOS, runtime.GOARCH, err)
	}

	wantExt := ".gz"
	if runtime.GOOS == "windows" {
		wantExt = ".zip"
	}
	if !strings.HasSuffix(strings.ToLower(name), wantExt) {
		t.Errorf("平台 %s 应挑 %s 资产，实际挑中 %q", runtime.GOOS, wantExt, name)
	}
	// 必须是当前平台的资产，不能跨平台错拿
	if !strings.Contains(name, runtime.GOOS) {
		t.Errorf("资产 %q 与当前平台 %s 不匹配", name, runtime.GOOS)
	}
}

func TestPickMihomoAssetSkipsDistroPackages(t *testing.T) {
	_, name, _, err := pickMihomoAsset(realisticRelease())
	if err != nil {
		t.Fatalf("挑选失败: %v", err)
	}
	// 发行版包需要包管理器安装、落盘路径不由我们决定，
	// 与"下载到 data/bin 下自管"的模型不符
	for _, bad := range []string{".deb", ".rpm", ".pkg.tar.zst", ".apk"} {
		if strings.HasSuffix(name, bad) {
			t.Errorf("挑中了发行版包 %q，应排除", name)
		}
	}
}

func TestPickMihomoAssetPrefersCompatible(t *testing.T) {
	// compatible 构建不依赖较新 CPU 指令集，兼容性更好，应优先
	if runtime.GOARCH != "amd64" {
		t.Skip("测试数据里只有 amd64 提供 compatible 变体")
	}
	_, name, _, err := pickMihomoAsset(realisticRelease())
	if err != nil {
		t.Fatalf("挑选失败: %v", err)
	}
	if !strings.Contains(name, "compatible") {
		t.Errorf("应优先挑 compatible 变体，实际 %q", name)
	}
}

func TestIsDistroPackage(t *testing.T) {
	cases := map[string]bool{
		"mihomo-linux-amd64-v1.19.29.gz":          false,
		"mihomo-windows-amd64-v1.19.29.zip":       false,
		"mihomo-linux-amd64-v1.19.29.deb":         true,
		"mihomo-linux-amd64-v1.19.29.rpm":         true,
		"mihomo-linux-arm64-v1.19.29.pkg.tar.zst": true,
		"mihomo-linux-amd64-v1.19.29.apk":         true,
	}
	for name, want := range cases {
		if got := isDistroPackage(name); got != want {
			t.Errorf("isDistroPackage(%q) = %v, 期望 %v", name, got, want)
		}
	}
}

// 没有匹配资产时的报错要能看出缺的是什么扩展名
func TestPickMihomoAssetErrorMentionsExtension(t *testing.T) {
	rel := &githubRelease{
		TagName: "v1.0.0",
		Assets:  []githubAsset{{Name: "mihomo-someotherplatform-v1.0.0.gz"}},
	}
	_, _, _, err := pickMihomoAsset(rel)
	if err == nil {
		t.Fatal("无匹配资产时应报错")
	}
	if !strings.Contains(err.Error(), mihomoAssetExt()) {
		t.Errorf("报错应提到期望的扩展名 %s，实际: %v", mihomoAssetExt(), err)
	}
}

// 特化变体（-goNNN- / -v1/v2/v3-）必须跳过。
//
// 官方同一平台会并发布 11 个 .gz（v1.19.29 的 linux-amd64 实测），
// 其中 -v3- 需要 AVX2，老 CPU 直接跑不起来；-goNNN- 是给旧系统的构建。
// 若按"第一个匹配"取，结果取决于 GitHub 返回顺序，既不稳定也可能不可用。
func TestPickMihomoAssetSkipsSpecializedVariants(t *testing.T) {
	// 按当前平台构造资产名，使断言不依赖运行平台（与本文件其它用例一致）。
	// 特化变体排在前、基础版排最后，确保不是靠顺序碰巧选对。
	plat := runtime.GOOS + "-" + runtime.GOARCH
	ext := mihomoAssetExt()
	base := "mihomo-" + plat + "-v1.19.29" + ext
	rel := &githubRelease{
		TagName: "v1.19.29",
		Assets: []githubAsset{
			{Name: "mihomo-" + plat + "-v3-v1.19.29" + ext, BrowserDownloadURL: "u1", Size: 1},
			{Name: "mihomo-" + plat + "-v2-go123-v1.19.29" + ext, BrowserDownloadURL: "u2", Size: 2},
			{Name: "mihomo-" + plat + "-v1-v1.19.29" + ext, BrowserDownloadURL: "u3", Size: 3},
			{Name: base, BrowserDownloadURL: "u4", Size: 4},
		},
	}
	_, name, _, err := pickMihomoAsset(rel)
	if err != nil {
		t.Fatalf("应选中基础版: %v", err)
	}
	if name != base {
		t.Errorf("应选基础版 %q，实际选了 %q", base, name)
	}
}

// 变体识别的边界：不能把正常名字误判成变体
func TestIsSpecializedVariant(t *testing.T) {
	cases := map[string]bool{
		"mihomo-linux-amd64-v1.19.29.gz":            false, // 版本号里的 v1 不算
		"mihomo-linux-amd64-compatible-v1.19.29.gz": false,
		"mihomo-darwin-arm64-v1.19.29.gz":           false,
		"mihomo-linux-amd64-v1-v1.19.29.gz":         true, // 微架构等级
		"mihomo-linux-amd64-v3-v1.19.29.gz":         true,
		"mihomo-darwin-arm64-go120-v1.19.29.gz":     true, // 旧 Go 构建
		"mihomo-linux-amd64-v2-go123-v1.19.29.gz":   true,
	}
	for name, want := range cases {
		if got := isSpecializedVariant(name); got != want {
			t.Errorf("isSpecializedVariant(%q) = %v，期望 %v", name, got, want)
		}
	}
}
