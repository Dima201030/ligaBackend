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
