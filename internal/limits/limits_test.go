package limits_test

import (
	"testing"
	"time"

	"github.com/Dima201030/ligaBackend/internal/limits"
)

func TestValidateClubName(t *testing.T) {
	if msg := limits.ValidateClubName("ab"); msg == "" {
		t.Fatal("expected short name error")
	}
	if msg := limits.ValidateClubName("Demo"); msg != "" {
		t.Fatalf("unexpected: %s", msg)
	}
	long := stringsRepeat("я", 81)
	if msg := limits.ValidateClubName(long); msg == "" {
		t.Fatal("expected long name error")
	}
}

func TestValidateClubSlug(t *testing.T) {
	if msg := limits.ValidateClubSlug("ab"); msg == "" {
		t.Fatal("expected short slug error")
	}
	if msg := limits.ValidateClubSlug("my-club"); msg != "" {
		t.Fatalf("unexpected: %s", msg)
	}
	if msg := limits.ValidateClubSlug("Bad_Slug"); msg == "" {
		t.Fatal("expected format error")
	}
	long := stringsRepeat("a", 41)
	if msg := limits.ValidateClubSlug(long); msg == "" {
		t.Fatal("expected long slug error")
	}
}

func TestValidateAge(t *testing.T) {
	asOf := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	if msg := limits.ValidateAge(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), asOf); msg == "" {
		t.Fatal("expected too young")
	}
	if msg := limits.ValidateAge(time.Date(1990, 5, 1, 0, 0, 0, 0, time.UTC), asOf); msg != "" {
		t.Fatalf("unexpected: %s", msg)
	}
	if msg := limits.ValidateAge(time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), asOf); msg == "" {
		t.Fatal("expected too old")
	}
}

func stringsRepeat(s string, n int) string {
	out := make([]rune, 0, n)
	r := []rune(s)[0]
	for i := 0; i < n; i++ {
		out = append(out, r)
	}
	return string(out)
}
