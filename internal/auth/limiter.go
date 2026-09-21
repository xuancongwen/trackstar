package auth

import (
	"sync"
	"time"
)

// Limiter is a fixed-window attempt counter used to slow down password
// guessing. It lives in memory: a restart simply resets the windows.
type Limiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	now     func() time.Time
	entries map[string]*limiterEntry
}

type limiterEntry struct {
	count int
	reset time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, now: time.Now, entries: map[string]*limiterEntry{}}
}

// Allow records an attempt for key and reports whether it may proceed.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.entries) > 10_000 {
		for k, e := range l.entries {
			if now.After(e.reset) {
				delete(l.entries, k)
			}
		}
	}
	e, ok := l.entries[key]
	if !ok || now.After(e.reset) {
		l.entries[key] = &limiterEntry{count: 1, reset: now.Add(l.window)}
		return true
	}
	e.count++
	return e.count <= l.max
}
