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
	"github.com/Dima201030/ligaBackend/internal/clubs"
	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestTournamentOrganizerScopedAccess(t *testing.T) {
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
	clubSlug := "acc-" + itoaMem(suffix)
	club, err := store.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		Slug:     clubSlug,
		Name:     "Access Club",
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

	owner := createAccessUser(t, store, "acc-owner-"+itoaMem(suffix)+"@example.com", sqlc.UserRolePlayer)
	org := createAccessUser(t, store, "acc-org-"+itoaMem(suffix)+"@example.com", sqlc.UserRolePlayer)
	player := createAccessUser(t, store, "acc-player-"+itoaMem(suffix)+"@example.com", sqlc.UserRolePlayer)
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM profiles WHERE user_id=$1 OR user_id=$2 OR user_id=$3`, owner.ID, org.ID, player.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1 OR id=$2 OR id=$3`, owner.ID, org.ID, player.ID)
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

	// plain player cannot open organizer tournaments
	playerReq := httptest.NewRequest(http.MethodGet, "/api/organizer/tournaments", nil)
	playerReq.Header.Set("X-Club-Slug", clubSlug)
	playerReq = playerReq.WithContext(auth.SetUser(playerReq.Context(), &auth.SessionUser{
		ID: player.ID, Email: emailString(player.Email), Role: player.Role, Status: player.Status,
	}))
	playerRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(playerRec, playerReq)
	if playerRec.Code != http.StatusForbidden {
		t.Fatalf("player tournaments want 403, got %d %s", playerRec.Code, playerRec.Body.String())
	}

	// tournament organizer can list tournaments
	orgTournReq := httptest.NewRequest(http.MethodGet, "/api/organizer/tournaments", nil)
	orgTournReq.Header.Set("X-Club-Slug", clubSlug)
	orgTournReq = orgTournReq.WithContext(auth.SetUser(orgTournReq.Context(), &auth.SessionUser{
		ID: org.ID, Email: emailString(org.Email), Role: org.Role, Status: org.Status,
	}))
	orgTournRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(orgTournRec, orgTournReq)
	if orgTournRec.Code != http.StatusOK {
		t.Fatalf("org tournaments want 200, got %d %s", orgTournRec.Code, orgTournRec.Body.String())
	}

	// tournament organizer cannot list members
	orgMemReq := httptest.NewRequest(http.MethodGet, "/api/organizer/members", nil)
	orgMemReq.Header.Set("X-Club-Slug", clubSlug)
	orgMemReq = orgMemReq.WithContext(auth.SetUser(orgMemReq.Context(), &auth.SessionUser{
		ID: org.ID, Email: emailString(org.Email), Role: org.Role, Status: org.Status,
	}))
	orgMemRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(orgMemRec, orgMemReq)
	if orgMemRec.Code != http.StatusForbidden {
		t.Fatalf("org members want 403, got %d %s", orgMemRec.Code, orgMemRec.Body.String())
	}

	// club owner can list members
	ownerMemReq := httptest.NewRequest(http.MethodGet, "/api/organizer/members", nil)
	ownerMemReq.Header.Set("X-Club-Slug", clubSlug)
	ownerMemReq = ownerMemReq.WithContext(auth.SetUser(ownerMemReq.Context(), &auth.SessionUser{
		ID: owner.ID, Email: emailString(owner.Email), Role: owner.Role, Status: owner.Status,
	}))
	ownerMemRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(ownerMemRec, ownerMemReq)
	if ownerMemRec.Code != http.StatusOK {
		t.Fatalf("owner members want 200, got %d %s", ownerMemRec.Code, ownerMemRec.Body.String())
	}

	// /me exposes club_role + organizer_access for tournament organizer
	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.Header.Set("X-Club-Slug", clubSlug)
	meReq = meReq.WithContext(auth.SetUser(meReq.Context(), &auth.SessionUser{
		ID: org.ID, Email: emailString(org.Email), Role: org.Role, Status: org.Status,
	}))
	meRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me want 200, got %d %s", meRec.Code, meRec.Body.String())
	}
	var me map[string]any
	if err := json.Unmarshal(meRec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me["club_role"] != "organizer" {
		t.Fatalf("club_role=%v", me["club_role"])
	}
	if me["organizer_access"] != organizerAccessTournament {
		t.Fatalf("organizer_access=%v", me["organizer_access"])
	}

	ownerMeReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	ownerMeReq.Header.Set("X-Club-Slug", clubSlug)
	ownerMeReq = ownerMeReq.WithContext(auth.SetUser(ownerMeReq.Context(), &auth.SessionUser{
		ID: owner.ID, Email: emailString(owner.Email), Role: owner.Role, Status: owner.Status,
	}))
	ownerMeRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(ownerMeRec, ownerMeReq)
	if ownerMeRec.Code != http.StatusOK {
		t.Fatalf("owner me want 200, got %d", ownerMeRec.Code)
	}
	var ownerMe map[string]any
	if err := json.Unmarshal(ownerMeRec.Body.Bytes(), &ownerMe); err != nil {
		t.Fatal(err)
	}
	if ownerMe["club_role"] != "owner" || ownerMe["organizer_access"] != organizerAccessFull {
		t.Fatalf("owner me=%v", ownerMe)
	}
}

func TestActorOrganizerAccessHelpers(t *testing.T) {
	if clubRoleRank(sqlc.ClubMemberRoleOrganizer) < clubRoleRank(sqlc.ClubMemberRoleOrganizer) {
		t.Fatal("rank identity")
	}
	if organizerAccessTournament != "tournament" || organizerAccessFull != "full" {
		t.Fatal("access constants")
	}
}

func createAccessUser(t *testing.T, store *db.Store, email string, role sqlc.UserRole) sqlc.User {
	t.Helper()
	ctx := context.Background()
	hash, err := crypto.HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.Queries.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
		Email: textEmail(email), PasswordHash: hash, FirstName: "Acc", LastName: "Ess",
		Role: role, Status: sqlc.UserStatusApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queries.CreateProfile(ctx, rating.NewProfileParams(user.ID, clubs.DefaultUUID(), pgtype.Text{}, "challenger", "Acc", "Ess")); err != nil {
		t.Fatal(err)
	}
	return user
}
