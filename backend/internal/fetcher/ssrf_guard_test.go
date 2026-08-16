package fetcher

import (
	"context"
	"strings"
	"testing"
	"time"
)

// guardedDialContext 应拒绝解析到 169.254/16 与 fd00:ec2::254 的域名。
func TestGuardedDialContextBlocksMetadataIPs(t *testing.T) {
	dial := guardedDialContext(time.Second)
	for _, host := range []string{
		"169.254.169.254:80", // AWS/GC 等云 metadata 端点
		"169.254.0.1:80",     // 链路本地
		"100.100.100.200:80", // 阿里云 metadata
		"[fd00:ec2::254]:80", // AWS IMDSv6
	} {
		if _, err := dial(context.Background(), "tcp", host); err == nil {
			t.Errorf("应拒绝拨号到 %s", host)
		} else if !strings.Contains(err.Error(), "metadata") {
			t.Errorf("错误应说明是 metadata, 实际: %v", err)
		}
	}
}

// 正常 IP 应能通过（连接失败会报网络错误而非 metadata 拦截）。
func TestGuardedDialContextAllowsNormalIPs(t *testing.T) {
	dial := guardedDialContext(time.Second)
	for _, host := range []string{
		"127.0.0.1:1",   // loopback 放行（本地 mock）
		"192.168.0.1:1", // RFC1918 放行
	} {
		// 连接会失败（端口无监听），但错误不应是 metadata 拦截
		_, err := dial(context.Background(), "tcp", host)
		if err == nil {
			continue
		}
		if strings.Contains(err.Error(), "metadata") {
			t.Errorf("普通地址 %s 不应被 metadata 拦截: %v", host, err)
		}
	}
}
