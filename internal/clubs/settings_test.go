package clubs

import (
	"encoding/json"
	"testing"
)

func TestSanitizeSettingsAllowList(t *testing.T) {
	got, err := SanitizeSettings(map[string]any{
		"timezone": "Europe/Moscow",
		"locale":   "ru-RU",
		"secret":   "nope",
		"api_key":  "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["timezone"] != "Europe/Moscow" || got["locale"] != "ru-RU" {
		t.Fatalf("got=%v", got)
	}
	if _, err := SanitizeSettings(map[string]any{"timezone": "Not/AZone"}); err == nil {
		t.Fatal("expected timezone validation error")
	}
}

func TestMergeSettingsOverlayAndNull(t *testing.T) {
	base := []byte(`{"timezone":"Europe/Moscow","legacy":"keep"}`)
	merged, err := MergeSettings(base, map[string]any{
		"timezone": "Asia/Yekaterinburg",
		"secret":   "drop",
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(merged, &m); err != nil {
		t.Fatal(err)
	}
	if m["timezone"] != "Asia/Yekaterinburg" {
		t.Fatalf("timezone=%v", m["timezone"])
	}
	if m["legacy"] != "keep" {
		t.Fatalf("legacy should remain in storage: %v", m)
	}
	if _, ok := m["secret"]; ok {
		t.Fatal("secret must not be stored")
	}

	cleared, err := MergeSettings(merged, map[string]any{"locale": nil})
	if err != nil {
		t.Fatal(err)
	}
	_ = cleared

	pub := PublicSettingsJSON([]byte(`{"timezone":"Europe/Moscow","secret":1}`))
	var p map[string]any
	if err := json.Unmarshal(pub, &p); err != nil {
		t.Fatal(err)
	}
	if len(p) != 1 || p["timezone"] != "Europe/Moscow" {
		t.Fatalf("public=%v", p)
	}
}
