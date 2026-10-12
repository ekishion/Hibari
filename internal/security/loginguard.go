package security

import (
	"sync"
	"time"
)

// LoginGuard 针对 /api/login 的按 IP 登录失败指数退避锁定。
type LoginGuard struct {
	mu           sync.Mutex
	entries      map[string]*loginAttempt
	lastCleanup  time.Time
	freeAttempts int
	baseDelay    time.Duration
	maxDelay     time.Duration
	decayAfter   time.Duration
}

type loginAttempt struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

const maxLoginGuardEntries = 10000

func NewLoginGuard() *LoginGuard {
	return &LoginGuard{
		entries:      make(map[string]*loginAttempt),
		lastCleanup:  time.Now(),
		freeAttempts: 5,
		baseDelay:    time.Second,
		maxDelay:     5 * time.Minute,
		decayAfter:   15 * time.Minute,
	}
}

// Allow 判断该 IP 当前是否处于锁定期。锁定时返回剩余等待时长。
func (g *LoginGuard) Allow(ip string, now time.Time) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cleanupLocked(now)

	e, ok := g.entries[ip]
	if !ok {
		return true, 0
	}
	if now.Before(e.lockedUntil) {
		return false, e.lockedUntil.Sub(now)
	}
	return true, 0
}

// RecordFailure 记录一次失败并按指数退避计算下一次锁定时长。
func (g *LoginGuard) RecordFailure(ip string, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cleanupLocked(now)

	e, ok := g.entries[ip]
	if !ok {
		if len(g.entries) >= maxLoginGuardEntries {
			g.evictOneLocked()
		}
		e = &loginAttempt{}
		g.entries[ip] = e
	}
	e.failures++
	e.lastSeen = now

	// 超过容忍次数后启动退避：baseDelay * 2^(failures - freeAttempts - 1)
	if e.failures > g.freeAttempts {
		shift := e.failures - g.freeAttempts - 1
		if shift > 16 {
			shift = 16
		}
		delay := g.baseDelay << shift
		if delay > g.maxDelay || delay <= 0 {
			delay = g.maxDelay
		}
		e.lockedUntil = now.Add(delay)
	}
}

// RecordSuccess 登录成功，重置失败计数与锁定状态。
func (g *LoginGuard) RecordSuccess(ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.entries, ip)
}

func (g *LoginGuard) cleanupLocked(now time.Time) {
	if now.Sub(g.lastCleanup) < time.Minute {
		return
	}
	for ip, e := range g.entries {
		if now.Sub(e.lastSeen) > g.decayAfter && !now.Before(e.lockedUntil) {
			delete(g.entries, ip)
		}
	}
	g.lastCleanup = now
}

func (g *LoginGuard) evictOneLocked() {
	EvictOldestSampled(g.entries, func(e *loginAttempt) time.Time {
		return e.lastSeen
	}, evictionSampleSize)
}
