package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// 口令哈希格式：pbkdf2$sha256$<迭代次数>$<base64 盐>$<base64 派生密钥>
// 用标准库 PBKDF2-HMAC-SHA256，避免引入额外依赖。
const (
	algoPrefix  = "pbkdf2$sha256"
	defaultIter = 210000 // 参考 OWASP 2023 对 PBKDF2-SHA256 的建议值
	saltLen     = 16
	keyLen      = 32
)

// HashPassword 生成带随机盐的口令哈希
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成盐失败: %w", err)
	}
	dk, err := pbkdf2.Key(sha256.New, password, salt, defaultIter, keyLen)
	if err != nil {
		return "", fmt.Errorf("派生密钥失败: %w", err)
	}
	return fmt.Sprintf("%s$%d$%s$%s", algoPrefix, defaultIter,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(dk)), nil
}

// VerifyPassword 校验口令。
// 第二个返回值指示存量数据是否为明文（需要在校验通过后升级为哈希）。
func VerifyPassword(stored, password string) (ok bool, needsUpgrade bool) {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return false, false
	}

	// 兼容升级前写入的明文密码，校验通过后由调用方改写为哈希
	if !strings.HasPrefix(stored, algoPrefix+"$") {
		return subtle.ConstantTimeCompare([]byte(stored), []byte(password)) == 1, true
	}

	parts := strings.Split(stored, "$")
	if len(parts) != 5 {
		return false, false
	}
	iter, err := strconv.Atoi(parts[2])
	if err != nil || iter <= 0 {
		return false, false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false, false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false, false
	}
	return subtle.ConstantTimeCompare(got, want) == 1, false
}

// GenerateSecret 生成 n 字节的随机十六进制串，用于初始密码与 JWT 密钥
func GenerateSecret(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成随机串失败: %w", err)
	}
	out := make([]byte, 0, n*2)
	const hexdigits = "0123456789abcdef"
	for _, c := range b {
		out = append(out, hexdigits[c>>4], hexdigits[c&0x0f])
	}
	return string(out), nil
}
