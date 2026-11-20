package tournaments

import "testing"

func TestSlugify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"Pushkin Open 2026", "pushkin-open-2026"},
		{"Новогодний парный турнир", "novogodnii-parnyi-turnir"},
		{"  Микст / вечер  ", "mikst-vecher"},
		{"!!!", "tournament"},
	}
	for _, tc := range cases {
		if got := Slugify(tc.in); got != tc.want {
			t.Fatalf("Slugify(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
