package auth

import (
	"strings"
	"testing"
	"time"
)

func TestHashAndVerify(t *testing.T) {
	hashed, err := HashPassword("s3cret-pass")
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}
	// 哈希不得包含原文
	if strings.Contains(hashed, "s3cret-pass") {
		t.Fatal("哈希结果泄露了原文口令")
	}
	if ok, upgrade := VerifyPassword(hashed, "s3cret-pass"); !ok || upgrade {
		t.Fatalf("正确口令校验失败: ok=%v upgrade=%v", ok, upgrade)
	}
	if ok, _ := VerifyPassword(hashed, "wrong"); ok {
		t.Fatal("错误口令被判为通过")
	}
}

// 同一口令两次哈希应因随机盐而不同
func TestHashUsesRandomSalt(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Fatal("两次哈希结果相同，盐未随机化")
	}
}

// 存量明文密码需能校验通过并被标记为待升级
func TestVerifyLegacyPlaintext(t *testing.T) {
	ok, upgrade := VerifyPassword("plainpwd", "plainpwd")
	if !ok || !upgrade {
		t.Fatalf("明文兼容校验失败: ok=%v upgrade=%v", ok, upgrade)
	}
	if ok, _ := VerifyPassword("plainpwd", "other"); ok {
		t.Fatal("明文模式下错误口令被判为通过")
	}
}

func TestVerifyMalformedHash(t *testing.T) {
	for _, bad := range []string{
		"",
		"pbkdf2$sha256$broken",
		"pbkdf2$sha256$abc$xx$yy",
		"pbkdf2$sha256$1000$!!!$!!!",
	} {
		if ok, _ := VerifyPassword(bad, "any"); ok {
			t.Fatalf("异常哈希 %q 不应校验通过", bad)
		}
	}
}

func TestGenerateSecretLength(t *testing.T) {
	s, err := GenerateSecret(16)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 32 {
		t.Fatalf("16 字节应产出 32 位十六进制，实际 %d", len(s))
	}
	other, _ := GenerateSecret(16)
	if s == other {
		t.Fatal("两次生成的随机串相同")
	}
}

func TestLoginLimiterLocksAfterMaxFailures(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute, time.Minute)
	const ip = "1.2.3.4"

	for i := 0; i < 2; i++ {
		l.Fail(ip)
		if ok, _ := l.Allow(ip); !ok {
			t.Fatalf("第 %d 次失败后不应锁定", i+1)
		}
	}

	l.Fail(ip) // 第 3 次达到阈值
	ok, wait := l.Allow(ip)
	if ok {
		t.Fatal("达到失败上限后应锁定")
	}
	if wait <= 0 {
		t.Fatalf("锁定应返回剩余等待时间，实际 %v", wait)
	}

	// 其他来源不受影响
	if ok, _ := l.Allow("5.6.7.8"); !ok {
		t.Fatal("限流误伤了其他来源 IP")
	}

	// 成功登录后解锁
	l.Reset(ip)
	if ok, _ := l.Allow(ip); !ok {
		t.Fatal("Reset 后应恢复允许")
	}
}

// 锁定期满后应自动恢复
func TestLoginLimiterUnlocksAfterTimeout(t *testing.T) {
	l := NewLoginLimiter(1, time.Minute, 50*time.Millisecond)
	l.Fail("ip")
	if ok, _ := l.Allow("ip"); ok {
		t.Fatal("应处于锁定状态")
	}
	time.Sleep(80 * time.Millisecond)
	if ok, _ := l.Allow("ip"); !ok {
		t.Fatal("锁定期满后应自动恢复")
	}
}
