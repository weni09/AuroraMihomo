package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zeromicro/go-zero/core/conf"
)

// 配置文件里的 YAML 拼写错误会让服务在启动时直接崩溃，
// 而这是任何测试都跑不到的路径，因此显式验证随仓库分发的两份配置可被解析，
// 且新增的 Server / TrustedProxies 段真的被读进结构体。
func TestShippedConfigFilesParse(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"本地配置", filepath.Join("..", "..", "etc", "aurora-api.yaml")},
		{"容器配置", filepath.Join("..", "..", "..", "..", "docker", "aurora-api.docker.yaml")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var cfg Config
			if err := conf.LoadConfig(c.path, &cfg); err != nil {
				t.Fatalf("解析 %s 失败: %v", c.path, err)
			}

			if cfg.Server.ReadHeaderTimeoutSec <= 0 {
				t.Errorf("ReadHeaderTimeoutSec 应为正数，实际 %d", cfg.Server.ReadHeaderTimeoutSec)
			}
			// WriteTimeout 必须大于请求处理超时，否则长耗时请求
			// （合并配置、下载内核）会在写响应阶段被连接层掐断
			if cfg.Timeout > 0 {
				writeMs := int64(cfg.Server.WriteTimeoutSec) * 1000
				if writeMs <= cfg.Timeout {
					t.Errorf("WriteTimeoutSec(%ds=%dms) 必须大于 Timeout(%dms)",
						cfg.Server.WriteTimeoutSec, writeMs, cfg.Timeout)
				}
			}
			// 默认不得信任任何代理，否则登录限流可被伪造头部绕过
			if len(cfg.TrustedProxies) != 0 {
				t.Errorf("随仓库分发的配置不应预置可信代理，实际 %v", cfg.TrustedProxies)
			}
		})
	}
}

// 默认值应在未提供配置项时生效
func TestServerTimeoutDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "minimal.yaml")
	// Mihomo 段必填（其 BinaryPath 无可用默认值），其余留空以验证默认值生效
	minimal := "Name: test\nHost: 127.0.0.1\nPort: 8899\n" +
		"Mihomo:\n  BinaryPath: \"\"\n  ConfigDir: \"./data\"\n"
	if err := os.WriteFile(path, []byte(minimal), 0o600); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	if err := conf.LoadConfig(path, &cfg); err != nil {
		t.Fatalf("解析最小配置失败: %v", err)
	}
	if cfg.Server.ReadHeaderTimeoutSec != 10 {
		t.Errorf("ReadHeaderTimeoutSec 默认值应为 10，实际 %d", cfg.Server.ReadHeaderTimeoutSec)
	}
	if cfg.Server.WriteTimeoutSec != 360 {
		t.Errorf("WriteTimeoutSec 默认值应为 360，实际 %d", cfg.Server.WriteTimeoutSec)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Errorf("TrustedProxies 默认应为空，实际 %v", cfg.TrustedProxies)
	}
}
