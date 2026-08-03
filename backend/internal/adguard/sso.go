package adguard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CredStore 持久化 AGH 管理员口令（可逆密文），供面板重启后免密接管。
// 实现方负责加解密与落库；桥本身不碰密钥材料。
type CredStore interface {
	// Save 保存用户名与明文密码（实现内加密）。
	Save(username, password string) error
	// Load 返回用户名与明文密码；无记录时 password 为空、err 为 nil。
	Load() (username, password string, err error)
	// Clear 删除持久化凭据。
	Clear() error
}

// SessionBridge 在 Aurora 已登录前提下，代持 AdGuard 的 agh_session，
// 使 /adguard-ui 反代可免密进入 AGH 控制台。
// 口令可经 CredStore 落盘（加密），进程重启后仍能接管。
type SessionBridge struct {
	mu sync.Mutex
	// key → AGH 会话（仅内存）
	sessions map[string]bridgeSession
	// key → 明文密码（内存缓存；持久化由 store 负责）
	passwords map[string]bridgeSecret
	// AGH 管理员用户名（默认 admin）
	username string
	// webAddr 如 127.0.0.1:3000
	webAddrFn func() string
	client    *http.Client
	store     CredStore
}

type bridgeSession struct {
	cookie string // agh_session 值
	expiry time.Time
}

type bridgeSecret struct {
	password string
	expiry   time.Time
}

// NewSessionBridge 创建会话桥。webAddrFn 每次解析 AGH Web 地址。
func NewSessionBridge(webAddrFn func() string) *SessionBridge {
	return &SessionBridge{
		sessions:  make(map[string]bridgeSession),
		passwords: make(map[string]bridgeSecret),
		username:  "admin",
		webAddrFn: webAddrFn,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// SetCredStore 注入持久化后端；可在 New 之后、Hydrate 之前调用。
func (b *SessionBridge) SetCredStore(store CredStore) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.store = store
	b.mu.Unlock()
}

// HydrateFromStore 从磁盘加载用户名/密码到内存（不主动 Establish，等反代按需换 cookie）。
func (b *SessionBridge) HydrateFromStore() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	store := b.store
	b.mu.Unlock()
	if store == nil {
		return nil
	}
	user, pass, err := store.Load()
	if err != nil {
		return err
	}
	if pass == "" {
		return nil
	}
	if user == "" {
		user = "admin"
	}
	b.mu.Lock()
	b.username = user
	// 永久接管：内存密码无固定过期，用远期 expiry；真正失效靠 AGH 改密后 Establish 失败
	b.passwords["1"] = bridgeSecret{password: pass, expiry: time.Now().Add(365 * 24 * time.Hour)}
	b.mu.Unlock()
	return nil
}

// SetUsername 设置 AGH 登录名。
func (b *SessionBridge) SetUsername(name string) {
	if b == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	b.mu.Lock()
	b.username = name
	b.mu.Unlock()
}

// PersistCredentials 写入内存并加密落库，实现永久接管。
func (b *SessionBridge) PersistCredentials(username, password string) error {
	if b == nil {
		return fmt.Errorf("session bridge nil")
	}
	if password == "" {
		return fmt.Errorf("empty password")
	}
	if username == "" {
		username = "admin"
	}
	b.mu.Lock()
	b.username = username
	b.passwords["1"] = bridgeSecret{password: password, expiry: time.Now().Add(365 * 24 * time.Hour)}
	store := b.store
	b.mu.Unlock()
	if store != nil {
		if err := store.Save(username, password); err != nil {
			return fmt.Errorf("持久化 AGH 凭据失败: %w", err)
		}
	}
	return nil
}

// RememberPassword 在 Aurora 登录/改密时缓存明文到内存（可选落库由 PersistCredentials 负责）。
// ttl 建议与 JWT 有效期一致；永久接管场景请用 PersistCredentials。
func (b *SessionBridge) RememberPassword(userKey, password string, ttl time.Duration) {
	if b == nil || userKey == "" || password == "" {
		return
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	b.mu.Lock()
	b.passwords[userKey] = bridgeSecret{password: password, expiry: time.Now().Add(ttl)}
	b.mu.Unlock()
}

// Clear 清除某用户的内存会话与密码缓存（登出时调用）。
// 不删除 CredStore 中的持久化凭据，以便下次登录仍能免密接管。
func (b *SessionBridge) Clear(userKey string) {
	if b == nil || userKey == "" {
		return
	}
	b.mu.Lock()
	delete(b.sessions, userKey)
	delete(b.passwords, userKey)
	b.mu.Unlock()
}

// ForgetStoredCredentials 清除持久化凭据与全部内存缓存（卸载/主动撤销接管）。
func (b *SessionBridge) ForgetStoredCredentials() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.sessions = make(map[string]bridgeSession)
	b.passwords = make(map[string]bridgeSecret)
	store := b.store
	b.mu.Unlock()
	if store != nil {
		_ = store.Clear()
	}
}

// Establish 使用用户名密码向 AGH 换取 agh_session 并缓存；成功后可持久化口令。
func (b *SessionBridge) Establish(ctx context.Context, userKey, username, password string) error {
	if b == nil {
		return fmt.Errorf("session bridge nil")
	}
	if password == "" {
		return fmt.Errorf("empty password")
	}
	if username == "" {
		b.mu.Lock()
		username = b.username
		b.mu.Unlock()
	}
	cookie, exp, err := b.loginAGH(ctx, username, password)
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.sessions[userKey] = bridgeSession{cookie: cookie, expiry: exp}
	b.username = username
	// 会话期内内存可重登；同时尽量与 AGH session 寿命对齐，下限 24h
	ttl := time.Until(exp)
	if ttl < 24*time.Hour {
		ttl = 24 * time.Hour
	}
	b.passwords[userKey] = bridgeSecret{password: password, expiry: time.Now().Add(ttl)}
	store := b.store
	b.mu.Unlock()
	// 登录成功即落库，重启后仍可接管（失败只记，不阻断本次会话）
	if store != nil {
		_ = store.Save(username, password)
	}
	return nil
}

// SessionCookie 返回可注入上游的 agh_session 值；必要时用缓存/持久化密码重登。
func (b *SessionBridge) SessionCookie(ctx context.Context, userKey string) string {
	if b == nil || userKey == "" {
		return ""
	}
	b.mu.Lock()
	sess, ok := b.sessions[userKey]
	if ok && time.Now().Before(sess.expiry) && sess.cookie != "" {
		c := sess.cookie
		b.mu.Unlock()
		return c
	}
	sec, hasPass := b.passwords[userKey]
	user := b.username
	store := b.store
	b.mu.Unlock()

	pass := ""
	fromMemory := false
	if hasPass && sec.password != "" && time.Now().Before(sec.expiry) {
		pass = sec.password
		fromMemory = true
	}
	// 内存没有有效密码时，从持久化加载（面板重启后的主路径）
	if pass == "" && store != nil {
		u, p, err := store.Load()
		if err == nil && p != "" {
			pass = p
			fromMemory = false
			if u != "" {
				user = u
			}
			b.mu.Lock()
			b.username = user
			b.passwords[userKey] = bridgeSecret{password: pass, expiry: time.Now().Add(365 * 24 * time.Hour)}
			b.mu.Unlock()
		}
	}
	if pass == "" {
		return ""
	}
	if err := b.Establish(ctx, userKey, user, pass); err != nil {
		// 内存口令可能是错误缓存（例如曾误把 Aurora 密码写进 RememberPassword）：
		// 丢掉后强制再试 CredStore 一次，避免永久挡住真正的 AGH 凭据。
		if fromMemory && store != nil {
			b.mu.Lock()
			delete(b.passwords, userKey)
			delete(b.sessions, userKey)
			b.mu.Unlock()
			u, p, lerr := store.Load()
			if lerr == nil && p != "" && p != pass {
				if u != "" {
					user = u
				}
				if e2 := b.Establish(ctx, userKey, user, p); e2 == nil {
					b.mu.Lock()
					defer b.mu.Unlock()
					if s, ok := b.sessions[userKey]; ok {
						return s.cookie
					}
				}
			}
		}
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.sessions[userKey]; ok {
		return s.cookie
	}
	return ""
}

// InvalidateSession 仅丢掉会话（保留密码以便下次 Establish）。
func (b *SessionBridge) InvalidateSession(userKey string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	delete(b.sessions, userKey)
	b.mu.Unlock()
}

type aghLoginReq struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

func (b *SessionBridge) loginAGH(ctx context.Context, username, password string) (cookie string, expiry time.Time, err error) {
	addr := "127.0.0.1:3000"
	if b.webAddrFn != nil {
		if a := strings.TrimSpace(b.webAddrFn()); a != "" {
			addr = strings.TrimPrefix(strings.TrimPrefix(a, "http://"), "https://")
		}
	}
	body, _ := json.Marshal(aghLoginReq{Name: username, Password: password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/control/login", bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+addr)
	resp, err := b.client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("AGH login HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	for _, c := range resp.Cookies() {
		if c.Name == "agh_session" && c.Value != "" {
			exp := c.Expires
			if exp.IsZero() {
				exp = time.Now().Add(24 * time.Hour)
			}
			return c.Value, exp, nil
		}
	}
	for _, line := range resp.Header.Values("Set-Cookie") {
		if !strings.Contains(line, "agh_session=") {
			continue
		}
		part := line
		if i := strings.Index(part, "agh_session="); i >= 0 {
			part = part[i+len("agh_session="):]
			if j := strings.Index(part, ";"); j >= 0 {
				part = part[:j]
			}
			part = strings.TrimSpace(part)
			if part != "" {
				return part, time.Now().Add(24 * time.Hour), nil
			}
		}
	}
	return "", time.Time{}, fmt.Errorf("AGH login OK but no agh_session cookie")
}
