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

func TestPlatformTournamentsAndCrossClubRegister(t *testing.T) {
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
	clubASlug := "cca-" + itoaCross(suffix)
	clubBSlug := "ccb-" + itoaCross(suffix)

	clubA, err := store.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		Slug: clubASlug, Name: "Club A Cross", City: "A", Status: sqlc.ClubStatusActive,
		Settings: []byte(`{"timezone":"Europe/Moscow"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	clubB, err := store.Queries.CreateClub(ctx, sqlc.CreateClubParams{
		Slug: clubBSlug, Name: "Club B Cross", City: "B", Status: sqlc.ClubStatusActive,
		Settings: []byte(`{"timezone":"Europe/Moscow"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM registrations WHERE tournament_id IN (SELECT id FROM tournaments WHERE club_id=$1 OR club_id=$2)`, clubA.ID, clubB.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM tournaments WHERE club_id=$1 OR club_id=$2`, clubA.ID, clubB.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM club_memberships WHERE club_id=$1 OR club_id=$2`, clubA.ID, clubB.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM clubs WHERE id=$1 OR id=$2`, clubA.ID, clubB.ID)
	})

	player := createCrossClubPlayer(t, store, "cross-"+itoaCross(suffix)+"@example.com", clubA.ID)
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM profiles WHERE user_id=$1`, player.ID)
		_, _ = store.Pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, player.ID)
	})
	if _, err := store.Queries.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
		ClubID: clubA.ID, UserID: player.ID, Role: sqlc.ClubMemberRolePlayer, Status: sqlc.ClubMemberStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	tourB, err := store.Queries.CreateTournament(ctx, sqlc.CreateTournamentParams{
		ClubID: clubB.ID, Slug: "cross-open-" + itoaCross(suffix),
		Title: "Cross Club Open Event", Description: "Platform catalog test tournament",
		Type: sqlc.TournamentTypeSingles, Category: "challenger", Surface: sqlc.SurfaceTypeHard,
		Format: sqlc.TournamentFormatOlympic, Status: sqlc.TournamentStatusRegistrationOpen,
		StartsAt:             pgtype.Timestamptz{Time: now.Add(48 * time.Hour), Valid: true},
		RegistrationOpensAt:  pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true},
		RegistrationClosesAt: pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		VenueName: "Court B", VenueCourts: 2, VenueSurface: sqlc.SurfaceTypeHard,
		EntryFee: 0, MaxParticipants: 16, Season: int32(now.Year()),
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := NewWithOptions(Options{Store: store, Cfg: config.Config{AppEnv: "test", DefaultClubSlug: clubASlug}})

	listReq := httptest.NewRequest(http.MethodGet, "/api/tournaments?scope=platform&status=upcoming", nil)
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("platform list=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listBody struct {
		Scope       string `json:"scope"`
		Tournaments []struct {
			Slug     string `json:"slug"`
			ClubSlug string `json:"club_slug"`
			ClubName string `json:"club_name"`
		} `json:"tournaments"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if listBody.Scope != "platform" {
		t.Fatalf("scope=%q", listBody.Scope)
	}
	found := false
	for _, item := range listBody.Tournaments {
		if item.Slug == tourB.Slug && item.ClubSlug == clubBSlug {
			found = true
			if item.ClubName == "" {
				t.Fatal("club_name empty")
			}
			break
		}
	}
	if !found {
		t.Fatalf("club B tournament missing from platform list: %s", listRec.Body.String())
	}

	body, _ := json.Marshal(map[string]any{"looking_for_partner": false})
	regReq := httptest.NewRequest(http.MethodPost, "/api/tournaments/"+tourB.Slug+"/registrations", bytes.NewReader(body))
	regReq.Header.Set("Content-Type", "application/json")
	regReq.Header.Set("X-Club-Slug", clubBSlug)
	regReq = regReq.WithContext(auth.SetUser(regReq.Context(), &auth.SessionUser{
		ID: player.ID, Email: emailString(player.Email), Role: player.Role, Status: player.Status,
	}))
	regRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(regRec, regReq)
	if regRec.Code != http.StatusCreated {
		t.Fatalf("register=%d body=%s", regRec.Code, regRec.Body.String())
	}

	mem, err := store.Queries.GetClubMembership(ctx, sqlc.GetClubMembershipParams{ClubID: clubB.ID, UserID: player.ID})
	if err != nil {
		t.Fatalf("membership after register: %v", err)
	}
	if mem.Status != sqlc.ClubMemberStatusActive || mem.Role != sqlc.ClubMemberRolePlayer {
		t.Fatalf("membership=%s/%s", mem.Role, mem.Status)
	}

	// Default club list must not require membership in club A for discovery of club A only.
	_ = clubs.DefaultSlug
}

func createCrossClubPlayer(t *testing.T, store *db.Store, email string, homeClub pgtype.UUID) sqlc.User {
	t.Helper()
	ctx := context.Background()
	hash, err := crypto.HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.Queries.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
		Email: textEmail(email), PasswordHash: hash, FirstName: "Cross", LastName: "Player",
		Role: sqlc.UserRolePlayer, Status: sqlc.UserStatusApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queries.CreateProfile(ctx, rating.NewProfileParams(user.ID, homeClub, pgtype.Text{}, "challenger", "Cross", "Player")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pool.Exec(ctx, `UPDATE profiles SET verification_status='approved', verified_at=now() WHERE user_id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	return user
}

func itoaCross(n int64) string {
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
