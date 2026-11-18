package crypto_test

import (
	"bytes"
	"testing"

	"github.com/Dima201030/ligaBackend/internal/crypto"
)

func TestHashPassword_VerifyOK(t *testing.T) {
	t.Parallel()
	password := "correct-horse-battery-staple"
	hash, err := crypto.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	ok, err := crypto.VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Fatal("expected verify to succeed for correct password")
	}
}

func TestHashPassword_VerifyWrongFails(t *testing.T) {
	t.Parallel()
	hash, err := crypto.HashPassword("right-password-long")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	ok, err := crypto.VerifyPassword("wrong-password-long", hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Fatal("expected verify to fail for wrong password")
	}
}

func TestAESGCM_RoundTrip(t *testing.T) {
	t.Parallel()
	key, err := crypto.RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes: %v", err)
	}
	plain := []byte("totp-secret-example")
	blob, err := crypto.EncryptAESGCM(key, plain)
	if err != nil {
		t.Fatalf("EncryptAESGCM: %v", err)
	}
	if bytes.Equal(blob, plain) {
		t.Fatal("ciphertext must differ from plaintext")
	}
	got, err := crypto.DecryptAESGCM(key, blob)
	if err != nil {
		t.Fatalf("DecryptAESGCM: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("roundtrip mismatch: got %q want %q", got, plain)
	}
}

func TestHashToken_Deterministic(t *testing.T) {
	t.Parallel()
	token := []byte("session-token-bytes")
	a := crypto.HashToken(token)
	b := crypto.HashToken(token)
	if a != b {
		t.Fatal("SHA-256 of same token must be stable")
	}
	other := crypto.HashToken([]byte("other-token"))
	if a == other {
		t.Fatal("different tokens must not collide in this test")
	}
}

func TestRandomBytes_Length(t *testing.T) {
	t.Parallel()
	b, err := crypto.RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes: %v", err)
	}
	if len(b) != 32 {
		t.Fatalf("len=%d want 32", len(b))
	}
}
