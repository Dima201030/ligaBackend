package auth

import (
	"sync"
	"time"
)

// RateLimiter is a simple in-memory limiter keyed by arbitrary strings (IP+email).
type RateLimiter struct {
	mu       sync.Mutex
	window   time.Duration
	maxHits  int
	attempts map[string][]time.Time
}

// NewRateLimiter creates a limiter that allows maxHits within window.
func NewRateLimiter(maxHits int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		window:   window,
		maxHits:  maxHits,
		attempts: make(map[string][]time.Time),
	}
}

// Allow returns false when the key has exceeded the limit.
func (l *RateLimiter) Allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	hits := l.attempts[key]
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.maxHits {
		l.attempts[key] = kept
		return false
	}
	l.attempts[key] = append(kept, now)
	return true
}
