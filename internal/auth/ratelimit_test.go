package auth

import (
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	t.Parallel()
	l := NewRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("hit %d should be allowed", i+1)
		}
	}
	if l.Allow("a") {
		t.Fatal("4th hit should be blocked")
	}
	if !l.Allow("b") {
		t.Fatal("other key should be allowed")
	}
}
