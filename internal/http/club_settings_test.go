package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/clubs"
	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPatchOrganizerClubSettings(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	store, err := db.Connect(ctx, url)
	if err != nil {
		t.Skipf("db unavailable: %v", err)
	}
	t.Cleanup(store.Close)

	suffix := time.Now().UnixNano()
	slug := "set-" + itoaSettings(suffix)
	club, err := store.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		Slug:     slug,
		Name:     "Old Name",
		City:     "Old City",
		Status:   sqlc.ClubStatusActive,
		Settings: []byte(`{"timezone":"Europe/Moscow","secret":"nope"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM club_memberships WHERE club_id=$1`, club.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM clubs WHERE id=$1`, club.ID)
	})

	owner := createSettingsUser(t, store, "set-owner-"+itoaSettings(suffix)+"@example.com")
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM profiles WHERE user_id=$1`, owner.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, owner.ID)
	})
	if _, err := store.Queries.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
		ClubID: club.ID, UserID: owner.ID, Role: sqlc.ClubMemberRoleOwner, Status: sqlc.ClubMemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	srv := NewWithOptions(Options{Store: store, Cfg: config.Config{AppEnv: "test", DefaultClubSlug: slug}})
	actor := &auth.SessionUser{ID: owner.ID, Email: owner.Email, Role: owner.Role, Status: owner.Status}

	getReq := httptest.NewRequest(http.MethodGet, "/api/organizer/club", nil)
	getReq.Header.Set("X-Club-Slug", slug)
	getReq = getReq.WithContext(auth.SetUser(getReq.Context(), actor))
	getRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get=%d %s", getRec.Code, getRec.Body.String())
	}
	var got struct {
		Club map[string]any `json:"club"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	settings, _ := got.Club["settings"].(map[string]any)
	if settings["timezone"] != "Europe/Moscow" {
		t.Fatalf("settings=%v", settings)
	}
	if _, ok := settings["secret"]; ok {
		t.Fatal("secret must not leak")
	}

	body, _ := json.Marshal(map[string]any{
		"name": "New Name",
		"city": "Пушкин",
		"settings": map[string]any{
			"timezone": "Asia/Yekaterinburg",
			"secret":   "still-no",
		},
	})
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/organizer/club", bytes.NewReader(body))
	patchReq.Header.Set("Content-Type", "application/json")
	patchReq.Header.Set("X-Club-Slug", slug)
	patchReq = patchReq.WithContext(auth.SetUser(patchReq.Context(), actor))
	patchRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch=%d %s", patchRec.Code, patchRec.Body.String())
	}
	var patched struct {
		Club map[string]any `json:"club"`
	}
	if err := json.Unmarshal(patchRec.Body.Bytes(), &patched); err != nil {
		t.Fatal(err)
	}
	if patched.Club["name"] != "New Name" || patched.Club["city"] != "Пушкин" {
		t.Fatalf("club=%v", patched.Club)
	}
	ps, _ := patched.Club["settings"].(map[string]any)
	if ps["timezone"] != "Asia/Yekaterinburg" {
		t.Fatalf("settings=%v", ps)
	}
	if _, ok := ps["secret"]; ok {
		t.Fatal("secret must not be returned")
	}

	// bad timezone
	bad, _ := json.Marshal(map[string]any{"settings": map[string]any{"timezone": "Nope/Zone"}})
	badReq := httptest.NewRequest(http.MethodPatch, "/api/organizer/club", bytes.NewReader(bad))
	badReq.Header.Set("Content-Type", "application/json")
	badReq.Header.Set("X-Club-Slug", slug)
	badReq = badReq.WithContext(auth.SetUser(badReq.Context(), actor))
	badRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("bad tz want 400 got %d", badRec.Code)
	}
}

func createSettingsUser(t *testing.T, store *db.Store, email string) sqlc.User {
	t.Helper()
	ctx := context.Background()
	hash, err := crypto.HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.Queries.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
		Email: email, PasswordHash: hash, FirstName: "Set", LastName: "Owner",
		Role: sqlc.UserRoleOrganizer, Status: sqlc.UserStatusApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queries.CreateProfile(ctx, rating.NewProfileParams(user.ID, clubs.DefaultUUID(), pgtype.Text{}, "challenger", "Set", "Owner")); err != nil {
		t.Fatal(err)
	}
	return user
}

func itoaSettings(n int64) string {
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
