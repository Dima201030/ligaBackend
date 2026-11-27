package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
)

func TestOrganizerAnalyticsSummaryAccess(t *testing.T) {
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
	clubSlug := "anl-" + itoaMem(suffix)
	club, err := store.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		Slug:     clubSlug,
		Name:     "Analytics Club",
		City:     "Test",
		Status:   sqlc.ClubStatusActive,
		Settings: []byte(`{"timezone":"Europe/Moscow"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM club_memberships WHERE club_id=$1`, club.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM clubs WHERE id=$1`, club.ID)
	})

	owner := createAccessUser(t, store, "anl-owner-"+itoaMem(suffix)+"@example.com", sqlc.UserRolePlayer)
	org := createAccessUser(t, store, "anl-org-"+itoaMem(suffix)+"@example.com", sqlc.UserRolePlayer)
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM profiles WHERE user_id=$1 OR user_id=$2`, owner.ID, org.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1 OR id=$2`, owner.ID, org.ID)
	})

	if _, err := store.Queries.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
		ClubID: club.ID, UserID: owner.ID, Role: sqlc.ClubMemberRoleOwner, Status: sqlc.ClubMemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queries.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
		ClubID: club.ID, UserID: org.ID, Role: sqlc.ClubMemberRoleOrganizer, Status: sqlc.ClubMemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	srv := NewWithOptions(Options{Store: store, Cfg: config.Config{AppEnv: "test", DefaultClubSlug: clubSlug}})

	unauthReq := httptest.NewRequest(http.MethodGet, "/api/organizer/analytics/summary", nil)
	unauthReq.Header.Set("X-Club-Slug", clubSlug)
	unauthRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth want 401, got %d %s", unauthRec.Code, unauthRec.Body.String())
	}

	orgReq := httptest.NewRequest(http.MethodGet, "/api/organizer/analytics/summary", nil)
	orgReq.Header.Set("X-Club-Slug", clubSlug)
	orgReq = orgReq.WithContext(auth.SetUser(orgReq.Context(), &auth.SessionUser{
		ID: org.ID, Email: emailString(org.Email), Role: org.Role, Status: org.Status,
	}))
	orgRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(orgRec, orgReq)
	if orgRec.Code != http.StatusForbidden {
		t.Fatalf("org want 403, got %d %s", orgRec.Code, orgRec.Body.String())
	}

	ownerReq := httptest.NewRequest(http.MethodGet, "/api/organizer/analytics/summary", nil)
	ownerReq.Header.Set("X-Club-Slug", clubSlug)
	ownerReq = ownerReq.WithContext(auth.SetUser(ownerReq.Context(), &auth.SessionUser{
		ID: owner.ID, Email: emailString(owner.Email), Role: owner.Role, Status: owner.Status,
	}))
	ownerRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(ownerRec, ownerReq)
	if ownerRec.Code != http.StatusOK {
		t.Fatalf("owner want 200, got %d %s", ownerRec.Code, ownerRec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(ownerRec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["club_slug"] != clubSlug {
		t.Fatalf("club_slug=%v", body["club_slug"])
	}
	players, ok := body["players"].(map[string]any)
	if !ok {
		t.Fatalf("players missing: %v", body)
	}
	if _, ok := players["approved_active"]; !ok {
		t.Fatalf("approved_active missing: %v", players)
	}
	growth, ok := body["growth"].(map[string]any)
	if !ok {
		t.Fatalf("growth missing: %v", body)
	}
	if _, ok := growth["players"]; !ok {
		t.Fatalf("growth.players missing: %v", growth)
	}
}
