package httpapi

import (
	"testing"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
)

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
