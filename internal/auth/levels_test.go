package auth

import "testing"

func TestValidLevel(t *testing.T) {
	t.Parallel()
	if !ValidLevel("rookies") {
		t.Fatal("rookies should be valid")
	}
	if ValidLevel("juniors") {
		t.Fatal("juniors should be invalid")
	}
}

func TestResolveLevel(t *testing.T) {
	t.Parallel()
	got, ok := ResolveLevel("")
	if !ok || got != DefaultLevel {
		t.Fatalf("empty → DefaultLevel, got %q ok=%v", got, ok)
	}
	got, ok = ResolveLevel("  challenger ")
	if !ok || got != "challenger" {
		t.Fatalf("challenger, got %q ok=%v", got, ok)
	}
	if _, ok := ResolveLevel("juniors"); ok {
		t.Fatal("juniors should be rejected")
	}
}
