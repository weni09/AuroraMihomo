package service

import (
	"errors"
	"fmt"
	"strings"

	"auroramihomo/backend/internal/auth"
	"auroramihomo/backend/internal/repository"
)

const settingAdGuardSSOPasswordEnc = "adguard.sso_password_enc"

// AGHCredStore 把 AGH 管理员口令以 AES-GCM 密文存入 settings。
// masterKey 使用跨重启稳定的 JWT AccessSecret。
type AGHCredStore struct {
	db        *repository.Database
	masterKey string
}

// NewAGHCredStore 构造持久化存储；db/masterKey 不可空。
func NewAGHCredStore(db *repository.Database, masterKey string) *AGHCredStore {
	return &AGHCredStore{db: db, masterKey: strings.TrimSpace(masterKey)}
}

func (s *AGHCredStore) Save(username, password string) error {
	if s == nil || s.db == nil {
		return errors.New("凭据存储未初始化")
	}
	if s.masterKey == "" {
		return errors.New("加密主密钥为空")
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return errors.New("密码不能为空")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		username = "admin"
	}
	sealed, err := auth.SealString(s.masterKey, password)
	if err != nil {
		return fmt.Errorf("加密 AGH 密码失败: %w", err)
	}
	if err := s.db.SetSetting(settingAdGuardUsername, username); err != nil {
		return fmt.Errorf("保存用户名失败: %w", err)
	}
	if err := s.db.SetSetting(settingAdGuardSSOPasswordEnc, sealed); err != nil {
		return fmt.Errorf("保存加密密码失败: %w", err)
	}
	return nil
}

func (s *AGHCredStore) Load() (username, password string, err error) {
	if s == nil || s.db == nil {
		return "", "", nil
	}
	if s.masterKey == "" {
		return "", "", errors.New("加密主密钥为空")
	}
	username, _ = s.db.GetSetting(settingAdGuardUsername)
	username = strings.TrimSpace(username)
	sealed, err := s.db.GetSetting(settingAdGuardSSOPasswordEnc)
	if err != nil || strings.TrimSpace(sealed) == "" {
		return username, "", nil
	}
	plain, err := auth.OpenString(s.masterKey, sealed)
	if err != nil {
		return username, "", fmt.Errorf("解密 AGH 密码失败: %w", err)
	}
	return username, plain, nil
}

func (s *AGHCredStore) Clear() error {
	if s == nil || s.db == nil {
		return nil
	}
	_ = s.db.SetSetting(settingAdGuardSSOPasswordEnc, "")
	return nil
}
