package mail

import (
	"strings"
	"testing"
)

func TestRenderConfirmEmail(t *testing.T) {
	msg, err := Render("confirm_email", []byte(`{"first_name":"Ира","confirm_url":"https://example/confirm?token=abc"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Subject, "email") {
		t.Fatalf("subject=%q", msg.Subject)
	}
	if !strings.Contains(msg.TextBody, "Ира") || !strings.Contains(msg.TextBody, "https://example/confirm") {
		t.Fatalf("body=%q", msg.TextBody)
	}
}

func TestRenderPasswordReset(t *testing.T) {
	msg, err := Render("password_reset", []byte(`{"token":"tok123"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.TextBody, "tok123") {
		t.Fatalf("body=%q", msg.TextBody)
	}
}

func TestRenderUnknown(t *testing.T) {
	msg, err := Render("custom_thing", []byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.TextBody, "custom_thing") {
		t.Fatalf("body=%q", msg.TextBody)
	}
}

func TestRenderTournamentRegistrationTemplates(t *testing.T) {
	payload := []byte(`{"first_name":"Юсуф","tournament_title":"Open A"}`)
	cases := []struct {
		template string
		subject  string
		snippet  string
	}{
		{"tournament_registration_confirmed", "Заявка подтверждена", "подтверждена"},
		{"tournament_registration_rejected", "Заявка отклонена", "отклонена"},
		{"tournament_registration_waitlist", "Лист ожидания", "листе ожидания"},
	}
	for _, tc := range cases {
		msg, err := Render(tc.template, payload)
		if err != nil {
			t.Fatalf("%s: %v", tc.template, err)
		}
		if !strings.Contains(msg.Subject, tc.subject) || !strings.Contains(msg.Subject, "Open A") {
			t.Fatalf("%s subject=%q", tc.template, msg.Subject)
		}
		if !strings.Contains(msg.TextBody, "Юсуф") || !strings.Contains(msg.TextBody, tc.snippet) || !strings.Contains(msg.TextBody, "Open A") {
			t.Fatalf("%s body=%q", tc.template, msg.TextBody)
		}
	}
}

func TestTournamentRegistrationTemplate(t *testing.T) {
	if tpl, ok := TournamentRegistrationTemplate("confirmed"); !ok || tpl != "tournament_registration_confirmed" {
		t.Fatalf("confirmed: %s %v", tpl, ok)
	}
	if tpl, ok := TournamentRegistrationTemplate("rejected"); !ok || tpl != "tournament_registration_rejected" {
		t.Fatalf("rejected: %s %v", tpl, ok)
	}
	if tpl, ok := TournamentRegistrationTemplate("waitlist"); !ok || tpl != "tournament_registration_waitlist" {
		t.Fatalf("waitlist: %s %v", tpl, ok)
	}
	if _, ok := TournamentRegistrationTemplate("pending"); ok {
		t.Fatal("pending should have no template")
	}
	if _, ok := TournamentRegistrationTemplate("cancelled"); ok {
		t.Fatal("cancelled should have no template")
	}
}
