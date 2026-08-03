package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// SealString 用 AES-256-GCM 可逆加密明文，密钥由 master 经 SHA-256 派生。
// 返回 base64.RawURLEncoding 编码的 nonce||ciphertext，便于落库。
// master 通常为跨重启稳定的 JWT AccessSecret，勿用易变随机串。
func SealString(master, plaintext string) (string, error) {
	if strings.TrimSpace(plaintext) == "" {
		return "", fmt.Errorf("明文为空")
	}
	key := sha256.Sum256([]byte(master))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("生成 nonce 失败: %w", err)
	}
	out := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

// OpenString 解密 SealString 的产物；master 必须与加密时一致。
func OpenString(master, sealed string) (string, error) {
	sealed = strings.TrimSpace(sealed)
	if sealed == "" {
		return "", fmt.Errorf("密文为空")
	}
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		// 兼容标准 base64
		raw, err = base64.StdEncoding.DecodeString(sealed)
		if err != nil {
			return "", fmt.Errorf("密文解码失败: %w", err)
		}
	}
	key := sha256.Sum256([]byte(master))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns+1 {
		return "", fmt.Errorf("密文过短")
	}
	nonce, ct := raw[:ns], raw[ns:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("解密失败: %w", err)
	}
	return string(pt), nil
}
