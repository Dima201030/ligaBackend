package rating

import (
	"testing"
	"time"
)

func TestNextRunAtSameDay(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	now := time.Date(2026, 11, 19, 1, 0, 0, 0, loc)
	next := NextRunAt(now, 3, 30)
	want := time.Date(2026, 11, 19, 3, 30, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("got %v want %v", next, want)
	}
}

func TestNextRunAtTomorrow(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	now := time.Date(2026, 11, 19, 3, 30, 0, 0, loc)
	next := NextRunAt(now, 3, 30)
	want := time.Date(2026, 11, 20, 3, 30, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("got %v want %v", next, want)
	}
}

func TestNextRunAtJustAfter(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	now := time.Date(2026, 11, 19, 3, 31, 0, 0, loc)
	next := NextRunAt(now, 3, 30)
	want := time.Date(2026, 11, 20, 3, 30, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("got %v want %v", next, want)
	}
}
