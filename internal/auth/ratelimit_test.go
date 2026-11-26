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

func TestRateLimiterAllowedRecordReset(t *testing.T) {
	t.Parallel()
	l := NewRateLimiter(2, time.Minute)

	if !l.Allowed("login") {
		t.Fatal("fresh key should be allowed")
	}
	if !l.Record("login") || !l.Record("login") {
		t.Fatal("first two records should succeed")
	}
	if l.Allowed("login") {
		t.Fatal("should be blocked after 2 hits")
	}
	if l.Record("login") {
		t.Fatal("record while blocked should return false")
	}

	l.Reset("login")
	if !l.Allowed("login") {
		t.Fatal("reset should clear hits")
	}
	if !l.Record("login") {
		t.Fatal("record after reset should succeed")
	}
}

func TestRateLimiterClear(t *testing.T) {
	t.Parallel()
	l := NewRateLimiter(1, time.Minute)
	if !l.Record("x") {
		t.Fatal("first record ok")
	}
	if l.Allowed("x") {
		t.Fatal("should be blocked")
	}
	l.Clear()
	if !l.Allowed("x") {
		t.Fatal("clear should wipe all keys")
	}
}
