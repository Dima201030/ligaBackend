package httpapi

import (
	"testing"
)

func TestDecodeNotificationPrefsDefaults(t *testing.T) {
	got := decodeNotificationPrefs(nil)
	if !got.Tournaments || !got.Reminders || !got.Results || got.News {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	got = decodeNotificationPrefs([]byte(`{"tournaments":false,"news":true}`))
	if got.Tournaments || !got.News || !got.Reminders || !got.Results {
		t.Fatalf("partial decode should keep defaults for omitted keys: %+v", got)
	}
}

func TestMergeNotificationPrefs(t *testing.T) {
	base := defaultNotificationPrefs()
	off := false
	on := true
	got, err := mergeNotificationPrefs(base, map[string]*bool{
		"news":        &on,
		"tournaments": &off,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Tournaments || !got.News || !got.Reminders || !got.Results {
		t.Fatalf("merge failed: %+v", got)
	}
	_, err = mergeNotificationPrefs(base, map[string]*bool{"spam": &on})
	if err == nil {
		t.Fatal("expected unknown key error")
	}
}

func TestEncodeNotificationPrefsRoundTrip(t *testing.T) {
	raw, err := encodeNotificationPrefs(notificationPrefsJSON{
		Tournaments: false,
		Reminders:   true,
		Results:     false,
		News:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := decodeNotificationPrefs(raw)
	if got.Tournaments || !got.Reminders || got.Results || !got.News {
		t.Fatalf("round-trip failed: %+v", got)
	}
}

func TestTournamentRegistrationEmailRespectsTournamentsPref(t *testing.T) {
	if !allowsTournamentRegistrationEmails(nil) {
		t.Fatal("nil prefs should default to allow")
	}
	if !allowsTournamentRegistrationEmails([]byte(`{}`)) {
		t.Fatal("empty object should keep tournaments default true")
	}
	if !allowsTournamentRegistrationEmails([]byte(`{"tournaments":true,"news":false}`)) {
		t.Fatal("explicit true should allow")
	}
	if allowsTournamentRegistrationEmails([]byte(`{"tournaments":false}`)) {
		t.Fatal("tournaments=false must skip outbox")
	}
}
