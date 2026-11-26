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

// Allowed reports whether key is still under the limit without recording a hit.
func (l *RateLimiter) Allowed(key string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	kept := l.pruneLocked(key, cutoff)
	l.attempts[key] = kept
	return len(kept) < l.maxHits
}

// Record adds a hit for key. Returns false when the key is over the limit
// after recording (caller may still choose to reject earlier via Allowed).
func (l *RateLimiter) Record(key string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	kept := l.pruneLocked(key, cutoff)
	if len(kept) >= l.maxHits {
		l.attempts[key] = kept
		return false
	}
	l.attempts[key] = append(kept, now)
	return true
}

// Allow records a hit and returns whether it was under the limit.
// Prefer Allowed + Record for login so successful attempts do not burn budget.
func (l *RateLimiter) Allow(key string) bool {
	return l.Record(key)
}

// Reset clears recorded hits for key (e.g. after a successful login).
func (l *RateLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// Clear wipes every key — ops escape hatch after a deploy/smoke lockout.
func (l *RateLimiter) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts = make(map[string][]time.Time)
}

func (l *RateLimiter) pruneLocked(key string, cutoff time.Time) []time.Time {
	hits := l.attempts[key]
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	return kept
}
