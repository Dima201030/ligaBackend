package httpapi

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestPublicPhoneHidesUnreliable(t *testing.T) {
	cases := []string{
		"+79990001122",
		"+79995391739",
		"+7 911 000-00-34",
		"0000000000",
		"+7 (999) 111-11-11",
		"123",
	}
	for _, phone := range cases {
		if got := publicPhone(pgtype.Text{String: phone, Valid: true}); got != nil {
			t.Fatalf("%q must be hidden, got %q", phone, *got)
		}
	}
}

func TestPublicPhoneKeepsPlausible(t *testing.T) {
	phone := "+7 921 555-12-34"
	got := publicPhone(pgtype.Text{String: phone, Valid: true})
	if got == nil || *got != phone {
		t.Fatalf("got %#v, want %q", got, phone)
	}
	if publicPhone(pgtype.Text{}) != nil {
		t.Fatal("empty must be nil")
	}
}
