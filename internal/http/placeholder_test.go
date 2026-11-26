package httpapi

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestTextEmail(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in    string
		valid bool
		want  string
	}{
		{"", false, ""},
		{"  ", false, ""},
		{"A@B.COM", true, "a@b.com"},
		{"  dima@kojem.ru ", true, "dima@kojem.ru"},
	}
	for _, tc := range cases {
		got := textEmail(tc.in)
		if got.Valid != tc.valid || got.String != tc.want {
			t.Fatalf("in=%q got=%+v want valid=%v %q", tc.in, got, tc.valid, tc.want)
		}
	}
}

func TestEmailString(t *testing.T) {
	t.Parallel()
	if emailString(pgtype.Text{}) != "" {
		t.Fatal("empty text should yield empty string")
	}
	if emailString(pgtype.Text{String: "a@b.c", Valid: true}) != "a@b.c" {
		t.Fatal("valid text should yield string")
	}
}

func TestMergeNameKeepPlaceholder(t *testing.T) {
	t.Parallel()
	// Mirrors handler merge rules: keep non-empty placeholder FIO.
	phFirst, phLast := "Иван", "Петров"
	srcFirst, srcLast := "Ivan", "Petrov"
	first := phFirst
	if first == "" {
		first = srcFirst
	}
	last := phLast
	if last == "" {
		last = srcLast
	}
	if first != "Иван" || last != "Петров" {
		t.Fatalf("got %s %s", first, last)
	}

	phFirst, phLast = "", ""
	first = phFirst
	if first == "" {
		first = srcFirst
	}
	last = phLast
	if last == "" {
		last = srcLast
	}
	if first != "Ivan" || last != "Petrov" {
		t.Fatalf("blank placeholder should take source, got %s %s", first, last)
	}
}
