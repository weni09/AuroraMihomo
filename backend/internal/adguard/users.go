package adguard

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost 与 AdGuard Home 安装向导一致（bcrypt.DefaultCost = 10）。
// AGH 从 AdGuardHome.yaml 的 users[].password 读取 bcrypt 哈希（$2a$ / $2y$）。
// 假设：嵌入的 AGH 为 v0.107.x 系，仍用 yaml users 字段；不走 HTTP API。
const bcryptCost = bcrypt.DefaultCost

// SetUserPassword 将 AdGuardHome.yaml 的 users 设为单一管理员（bcrypt 哈希）。
// username 为空时用 "admin"。plainPassword 不可为空。
// 文件不存在时创建最小配置。改完后若进程在跑，调用方宜 Restart 使生效。
func SetUserPassword(workDir, username, plainPassword string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	if plainPassword == "" {
		return fmt.Errorf("密码不能为空")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("生成 bcrypt 哈希失败: %w", err)
	}
	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return err
	}
	// 单一管理员：覆盖整个 users 列表，避免残留旧账号
	m["users"] = []any{
		map[string]any{
			"name":     username,
			"password": string(hash),
		},
	}
	return saveConfigMap(workDir, m)
}

// ReadUsername 读取 yaml users 中第一个用户的 name；无则空串。
func ReadUsername(workDir string) (string, error) {
	m, _, err := loadConfigMap(workDir)
	if err != nil {
		return "", err
	}
	users, ok := m["users"].([]any)
	if !ok || len(users) == 0 {
		return "", nil
	}
	u := asMap(users[0])
	if u == nil {
		return "", nil
	}
	if name, ok := u["name"].(string); ok {
		return strings.TrimSpace(name), nil
	}
	return "", nil
}
