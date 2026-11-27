package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPatchMeRejectsSelfProfileUpdate(t *testing.T) {
	srv := NewWithOptions(Options{Cfg: config.Config{AppEnv: "test"}})

	body, _ := json.Marshal(map[string]string{
		"first_name": "Новое",
		"last_name":  "Имя",
		"phone":      "+79990001122",
		"level":      "challenger",
		"city":       "Москва",
		"bio":        "нельзя",
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/auth/me", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	var uid pgtype.UUID
	if err := uid.Scan("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"); err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(auth.SetUser(req.Context(), &auth.SessionUser{
		ID:        uid,
		Email:     "player@example.com",
		FirstName: "Иван",
		LastName:  "Иванов",
		Role:      sqlc.UserRolePlayer,
		Status:    sqlc.UserStatusApproved,
		Level:     "challenger",
	}))

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["code"] != "profile_readonly" {
		t.Fatalf("code=%v body=%s", payload["code"], rec.Body.String())
	}
}

func TestPatchMeUnauthorizedWithoutSession(t *testing.T) {
	srv := NewWithOptions(Options{Cfg: config.Config{AppEnv: "test"}})
	body, _ := json.Marshal(map[string]string{"first_name": "X"})
	req := httptest.NewRequest(http.MethodPatch, "/api/auth/me", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}
