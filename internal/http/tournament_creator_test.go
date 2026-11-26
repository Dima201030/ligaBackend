package httpapi

import (
	"encoding/json"
	"testing"
)

func TestCreatorJSONOmitsEmail(t *testing.T) {
	raw, err := json.Marshal(creatorJSON{
		ID:        "11111111-1111-4111-8111-111111111111",
		FirstName: "Дмитрий",
		LastName:  "Кожемякин",
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["email"]; ok {
		t.Fatalf("public creator JSON must not include email, got %s", raw)
	}
	if m["first_name"] != "Дмитрий" || m["last_name"] != "Кожемякин" {
		t.Fatalf("unexpected payload: %s", raw)
	}
}
