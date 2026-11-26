package config

import "testing"

func TestEmailConfirmURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		cfg    Config
		token  string
		want   string
	}{
		{
			name:  "front preferred",
			cfg:   Config{FrontBaseURL: "https://pairly.space", PublicBaseURL: "https://api.example"},
			token: "abc",
			want:  "https://pairly.space/confirm-email?token=abc",
		},
		{
			name:  "api fallback",
			cfg:   Config{PublicBaseURL: "http://127.0.0.1:3020"},
			token: "abc",
			want:  "http://127.0.0.1:3020/api/auth/confirm-email?token=abc",
		},
		{
			name:  "empty token",
			cfg:   Config{FrontBaseURL: "https://pairly.space"},
			token: "",
			want:  "",
		},
		{
			name:  "no bases",
			cfg:   Config{},
			token: "abc",
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.cfg.EmailConfirmURL(tc.token); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
