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

func TestCreateTournamentClubOnCard(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	store, err := db.Connect(ctx, url)
	if err != nil {
		t.Skipf("db unavailable: %v", err)
	}
	t.Cleanup(store.Close)

	suffix := time.Now().UnixNano()
	defaultClub, err := store.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		Slug: "def-" + itoaMem(suffix), Name: "Default Club", City: "Test",
		Status: sqlc.ClubStatusActive, Settings: []byte(`{"timezone":"Europe/Moscow"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	targetClub, err := store.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		Slug: "tgt-" + itoaMem(suffix), Name: "Target Club", City: "Test",
		Status: sqlc.ClubStatusActive, Settings: []byte(`{"timezone":"Europe/Moscow"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM tournament_rule_versions WHERE tournament_id IN (SELECT id FROM tournaments WHERE club_id=$1 OR club_id=$2)`, defaultClub.ID, targetClub.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM tournaments WHERE club_id=$1 OR club_id=$2`, defaultClub.ID, targetClub.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM club_memberships WHERE club_id=$1 OR club_id=$2`, defaultClub.ID, targetClub.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM clubs WHERE id=$1 OR id=$2`, defaultClub.ID, targetClub.ID)
	})

	org := createPublishUser(t, store, "pub-org-"+itoaMem(suffix)+"@example.com", sqlc.UserRoleOrganizer)
	owner := createPublishUser(t, store, "pub-owner-"+itoaMem(suffix)+"@example.com", sqlc.UserRolePlayer)
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM profiles WHERE user_id=$1 OR user_id=$2`, org.ID, owner.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1 OR id=$2`, org.ID, owner.ID)
	})
	if _, err := store.Queries.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
		ClubID: defaultClub.ID, UserID: owner.ID, Role: sqlc.ClubMemberRoleOwner, Status: sqlc.ClubMemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	srv := NewWithOptions(Options{Store: store, Cfg: config.Config{AppEnv: "test", DefaultClubSlug: defaultClub.Slug}})

	starts := time.Now().UTC().Add(14 * 24 * time.Hour).Truncate(time.Minute)
	closes := starts.Add(-48 * time.Hour)
	body := map[string]any{
		"title":                   "Club Card Cup",
		"description":             "Publish under selected club without current-club switcher",
		"type":                    "singles",
		"category":                "challenger",
		"surface":                 "hard",
		"format":                  "olympic",
		"status":                  "draft",
		"starts_at":               starts.Format(time.RFC3339),
		"registration_opens_at":   closes.Add(-24 * time.Hour).Format(time.RFC3339),
		"registration_closes_at":  closes.Format(time.RFC3339),
		"venue_name":              "Court A",
		"venue_address":           "Test",
		"venue_courts":            2,
		"venue_surface":           "hard",
		"entry_fee":               0,
		"max_participants":        16,
		"regulations":             "Матчи до двух побед. Тай-брейк до семи. Олимпийская система с матчем за третье место.",
		"club_slug":               targetClub.Slug,
	}
	raw, _ := json.Marshal(body)

	// Global organizer publishes to any club via club_slug on the card.
	orgReq := httptest.NewRequest(http.MethodPost, "/api/organizer/tournaments", bytes.NewReader(raw))
	orgReq.Header.Set("Content-Type", "application/json")
	orgReq.Header.Set("X-Club-Slug", defaultClub.Slug) // header tenant ≠ body club
	orgReq = orgReq.WithContext(auth.SetUser(orgReq.Context(), &auth.SessionUser{
		ID: org.ID, Email: emailString(org.Email), Role: org.Role, Status: org.Status,
	}))
	orgRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(orgRec, orgReq)
	if orgRec.Code != http.StatusCreated {
		t.Fatalf("org create want 201, got %d %s", orgRec.Code, orgRec.Body.String())
	}
	var orgResp map[string]any
	if err := json.Unmarshal(orgRec.Body.Bytes(), &orgResp); err != nil {
		t.Fatal(err)
	}
	tourObj, _ := orgResp["tournament"].(map[string]any)
	if tourObj["club_slug"] != targetClub.Slug {
		t.Fatalf("club_slug=%v want %s", tourObj["club_slug"], targetClub.Slug)
	}
	if tourObj["club_id"] != uuidString(targetClub.ID) {
		t.Fatalf("club_id=%v", tourObj["club_id"])
	}

	// Club owner can publish under another club (prefill own, option for other).
	ownerBody := map[string]any{}
	for k, v := range body {
		ownerBody[k] = v
	}
	ownerBody["title"] = "Owner Other Club Cup"
	ownerBody["club_id"] = uuidString(targetClub.ID)
	delete(ownerBody, "club_slug")
	ownerRaw, _ := json.Marshal(ownerBody)
	ownerReq := httptest.NewRequest(http.MethodPost, "/api/organizer/tournaments", bytes.NewReader(ownerRaw))
	ownerReq.Header.Set("Content-Type", "application/json")
	ownerReq.Header.Set("X-Club-Slug", defaultClub.Slug)
	ownerReq = ownerReq.WithContext(auth.SetUser(ownerReq.Context(), &auth.SessionUser{
		ID: owner.ID, Email: emailString(owner.Email), Role: owner.Role, Status: owner.Status,
	}))
	ownerRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(ownerRec, ownerReq)
	if ownerRec.Code != http.StatusCreated {
		t.Fatalf("owner create want 201, got %d %s", ownerRec.Code, ownerRec.Body.String())
	}

	// List is multi-club for platform organizer (includes target club tour).
	listReq := httptest.NewRequest(http.MethodGet, "/api/organizer/tournaments", nil)
	listReq.Header.Set("X-Club-Slug", defaultClub.Slug)
	listReq = listReq.WithContext(auth.SetUser(listReq.Context(), &auth.SessionUser{
		ID: org.ID, Email: emailString(org.Email), Role: org.Role, Status: org.Status,
	}))
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list want 200, got %d %s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Tournaments []map[string]any `json:"tournaments"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range listResp.Tournaments {
		if item["club_slug"] == targetClub.Slug && item["title"] == "Club Card Cup" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected cross-club tournament in organizer list, got %d items", len(listResp.Tournaments))
	}
}

func createPublishUser(t *testing.T, store *db.Store, email string, role sqlc.UserRole) sqlc.User {
	t.Helper()
	ctx := context.Background()
	hash, err := crypto.HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.Queries.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
		Email: textEmail(email), PasswordHash: hash, FirstName: "Pub", LastName: "Lish",
		Role: role, Status: sqlc.UserStatusApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queries.CreateProfile(ctx, rating.NewProfileParams(user.ID, clubs.DefaultUUID(), pgtype.Text{}, "challenger", "Pub", "Lish")); err != nil {
		t.Fatal(err)
	}
	return user
}
