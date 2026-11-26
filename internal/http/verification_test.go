package httpapi

import (
	"os"
	"strings"
	"testing"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
)

func TestConfirmProfileVerificationSQLCastsVerifiedByUUID(t *testing.T) {
	t.Parallel()
	// Regression: Postgres typed CASE … THEN $n ELSE NULL as text and rejected
	// assignment to verified_by uuid (42804) under pgx prepared statements.
	src, err := os.ReadFile("sql/queries/players.sql")
	if err != nil {
		// tests may run from module root or package dir
		src, err = os.ReadFile("../../sql/queries/players.sql")
	}
	if err != nil {
		t.Fatalf("read players.sql: %v", err)
	}
	text := string(src)
	idx := strings.Index(text, "-- name: ConfirmProfileVerification")
	if idx < 0 {
		t.Fatal("ConfirmProfileVerification query missing")
	}
	chunk := text[idx:]
	if end := strings.Index(chunk[1:], "\n-- name:"); end >= 0 {
		chunk = chunk[:end+1]
	}
	if !strings.Contains(chunk, "NULL::uuid") {
		t.Fatal("ConfirmProfileVerification must cast ELSE NULL::uuid for verified_by")
	}
	if !strings.Contains(chunk, "sqlc.arg(verified_by)::uuid") {
		t.Fatal("ConfirmProfileVerification must use sqlc.arg(verified_by)::uuid")
	}
}

func TestProfileAllowsTournamentRegistration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status sqlc.ProfileVerificationStatus
		want   bool
	}{
		{sqlc.ProfileVerificationStatusPending, false},
		{sqlc.ProfileVerificationStatusRejected, false},
		{sqlc.ProfileVerificationStatusApproved, true},
		{sqlc.ProfileVerificationStatus(""), false},
	}
	for _, tc := range cases {
		if got := profileAllowsTournamentRegistration(tc.status); got != tc.want {
			t.Fatalf("status=%q got=%v want=%v", tc.status, got, tc.want)
		}
	}
}

func TestSanitizeSSONext(t *testing.T) {
	t.Parallel()
	fallback := "/mp-7f3a9c2e5b8d4106/tournaments"
	cases := []struct {
		in   string
		want string
	}{
		{"", fallback},
		{"https://evil.example/x", fallback},
		{"//evil.example", fallback},
		{"/api/auth/login", fallback},
		{"/mp-7f3a9c2e5b8d4106/tournaments", "/mp-7f3a9c2e5b8d4106/tournaments"},
		{"/mp-7f3a9c2e5b8d4106/tournaments/foo", "/mp-7f3a9c2e5b8d4106/tournaments/foo"},
		{"/tournaments", "/tournaments"},
	}
	for _, tc := range cases {
		if got := sanitizeSSONext(tc.in); got != tc.want {
			t.Fatalf("in=%q got=%q want=%q", tc.in, got, tc.want)
		}
	}
}
