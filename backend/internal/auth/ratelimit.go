package auth

import (
	"sync"
	"time"
)

// LoginLimiter 对登录接口做按来源 IP 的失败次数限流，
// 防止管理面板暴露在公网时被离线爆破。
type LoginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attemptState
	max      int
	window   time.Duration
	lockout  time.Duration
}

type attemptState struct {
	count       int
	firstFailAt time.Time
	lockedUntil time.Time
}

func NewLoginLimiter(max int, window, lockout time.Duration) *LoginLimiter {
	if max <= 0 {
		max = 5
	}
	if window <= 0 {
		window = 5 * time.Minute
	}
	if lockout <= 0 {
		lockout = 15 * time.Minute
	}
	return &LoginLimiter{
		attempts: map[string]*attemptState{},
		max:      max,
		window:   window,
		lockout:  lockout,
	}
}

// Allow 返回该来源当前是否允许尝试登录，以及仍需等待的时长
func (l *LoginLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gcLocked()

	st, ok := l.attempts[key]
	if !ok {
		return true, 0
	}
	if now := time.Now(); now.Before(st.lockedUntil) {
		return false, time.Until(st.lockedUntil).Round(time.Second)
	}
	return true, 0
}

// Fail 记录一次失败尝试，达到阈值后锁定该来源
func (l *LoginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	st, ok := l.attempts[key]
	// 首次失败或窗口已过期，重新开始计数
	if !ok || now.Sub(st.firstFailAt) > l.window {
		st = &attemptState{firstFailAt: now}
		l.attempts[key] = st
	}
	st.count++
	if st.count >= l.max {
		st.lockedUntil = now.Add(l.lockout)
		st.count = 0
		st.firstFailAt = now
	}
}

// Reset 登录成功后清除该来源的失败计数
func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// gcLocked 清理过期条目，避免长期运行下 map 无限增长
func (l *LoginLimiter) gcLocked() {
	now := time.Now()
	for k, st := range l.attempts {
		if now.After(st.lockedUntil) && now.Sub(st.firstFailAt) > l.window {
			delete(l.attempts, k)
		}
	}
}
