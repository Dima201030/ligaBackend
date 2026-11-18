package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpapi "github.com/Dima201030/ligaBackend/internal/http"
)

func TestHealth_OKNoSecrets(t *testing.T) {
	t.Parallel()
	srv := httpapi.New(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body["status"] != "degraded" && body["status"] != "ok" {
		t.Fatalf("status=%v", body["status"])
	}

	raw := strings.ToLower(rec.Body.String())
	for _, secret := range []string{"password", "encryption", "database_url", "postgres://"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("health body must not contain %q: %s", secret, rec.Body.String())
		}
	}

	checks, ok := body["checks"].(map[string]any)
	if !ok || checks["api"] == nil || checks["database"] == nil {
		t.Fatalf("expected api and database checks: %v", body)
	}
}

func TestStatusPage_OK(t *testing.T) {
	t.Parallel()
	srv := httpapi.New(nil)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("content-type=%q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Статус сервисов") {
		t.Fatalf("missing title in status page")
	}
}
