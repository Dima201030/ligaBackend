package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
)

func TestPublicClubsListActiveOnlyNoSettings(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := db.Connect(ctx, url)
	if err != nil {
		t.Skipf("db unavailable: %v", err)
	}
	t.Cleanup(store.Close)

	suffix := time.Now().UnixNano()
	activeSlug := "pub-active-" + itoaClubs(suffix)
	suspendedSlug := "pub-susp-" + itoaClubs(suffix)

	active, err := store.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		Slug:     activeSlug,
		Name:     "Public Active",
		City:     "Пушкин",
		Status:   sqlc.ClubStatusActive,
		Settings: []byte(`{"timezone":"Europe/Moscow","secret":"nope"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM clubs WHERE id = $1`, active.ID)
	})

	suspended, err := store.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		Slug:     suspendedSlug,
		Name:     "Public Suspended",
		City:     "Hidden",
		Status:   sqlc.ClubStatusSuspended,
		Settings: []byte(`{"timezone":"Europe/Moscow"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM clubs WHERE id = $1`, suspended.ID)
	})

	srv := NewWithOptions(Options{
		Store: store,
		Cfg: config.Config{
			AppEnv:      "test",
			CORSOrigins: []string{"https://pairly.space"},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/clubs", nil)
	req.Header.Set("Origin", "https://pairly.space")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://pairly.space" {
		t.Fatalf("cors origin=%q", got)
	}

	var payload struct {
		Clubs []map[string]any `json:"clubs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}

	var foundActive, foundSuspended bool
	for _, club := range payload.Clubs {
		slug, _ := club["slug"].(string)
		if slug == activeSlug {
			foundActive = true
			if _, ok := club["settings"]; ok {
				t.Fatalf("public list must omit settings: %+v", club)
			}
			if club["name"] != "Public Active" || club["status"] != "active" {
				t.Fatalf("active club payload: %+v", club)
			}
		}
		if slug == suspendedSlug {
			foundSuspended = true
		}
	}
	if !foundActive {
		t.Fatal("expected active club in list")
	}
	if foundSuspended {
		t.Fatal("suspended club must not appear in public list")
	}

	reqGet := httptest.NewRequest(http.MethodGet, "/api/clubs/"+activeSlug, nil)
	recGet := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", recGet.Code, recGet.Body.String())
	}
	var one struct {
		Club map[string]any `json:"club"`
	}
	if err := json.Unmarshal(recGet.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if _, ok := one.Club["settings"]; ok {
		t.Fatalf("public get must omit settings: %+v", one.Club)
	}

	reqSusp := httptest.NewRequest(http.MethodGet, "/api/clubs/"+suspendedSlug, nil)
	recSusp := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recSusp, reqSusp)
	if recSusp.Code != http.StatusNotFound {
		t.Fatalf("suspended get want 404, got %d", recSusp.Code)
	}
}

func TestPublicClubFromRowOmitsSettings(t *testing.T) {
	row := sqlc.Club{
		Slug:     "x",
		Name:     "X",
		City:     "Y",
		Status:   sqlc.ClubStatusActive,
		Settings: []byte(`{"secret":1}`),
	}
	got := publicClubFromRow(row)
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"id":"","slug":"x","name":"X","city":"Y","status":"active"}` {
		t.Fatalf("json=%s", raw)
	}
}

func itoaClubs(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
