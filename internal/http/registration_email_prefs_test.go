package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Dima201030/ligaBackend/internal/clubs"
	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/Dima201030/ligaBackend/internal/tournaments"
	"github.com/jackc/pgx/v5/pgtype"
)

func testStore(t *testing.T) *db.Store {
	t.Helper()
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
	return store
}

func countPendingTournamentEmails(t *testing.T, store *db.Store, to string) int {
	t.Helper()
	ctx := context.Background()
	var n int
	err := store.Pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM email_outbox
		WHERE "to" = $1
		  AND template LIKE 'tournament_registration_%'
		  AND status = 'pending'
	`, to).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEnqueueTournamentRegistrationEmailRespectsPrefs(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	emailOff := "prefs-off-" + itoa(suffix) + "@example.com"
	emailOn := "prefs-on-" + itoa(suffix) + "@example.com"

	offID := createApprovedPlayer(t, store, emailOff)
	onID := createApprovedPlayer(t, store, emailOn)

	offRaw, err := encodeNotificationPrefs(notificationPrefsJSON{
		Tournaments: false,
		Reminders:   true,
		Results:     true,
		News:        false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queries.UpdateNotificationPrefs(ctx, sqlc.UpdateNotificationPrefsParams{
		UserID:            offID,
		NotificationPrefs: offRaw,
	}); err != nil {
		t.Fatal(err)
	}

	tour := createOpenTournament(t, store, "prefs-mail-"+itoa(suffix), 4)
	s := NewWithOptions(Options{Store: store, Cfg: config.Config{AppEnv: "test"}})

	regOff, err := tournaments.CreateRegistration(ctx, store, tournaments.CreateRegistrationInput{
		TournamentID: tour.ID, PlayerID: offID,
	})
	if err != nil {
		t.Fatal(err)
	}
	regOn, err := tournaments.CreateRegistration(ctx, store, tournaments.CreateRegistrationInput{
		TournamentID: tour.ID, PlayerID: onID,
	})
	if err != nil {
		t.Fatal(err)
	}

	beforeOff := countPendingTournamentEmails(t, store, emailOff)
	beforeOn := countPendingTournamentEmails(t, store, emailOn)

	confirmedOff, _, err := tournaments.UpdateRegistrationStatus(ctx, store, tour.ID, regOff.ID, "confirmed")
	if err != nil {
		t.Fatal(err)
	}
	confirmedOn, _, err := tournaments.UpdateRegistrationStatus(ctx, store, tour.ID, regOn.ID, "confirmed")
	if err != nil {
		t.Fatal(err)
	}

	s.enqueueTournamentRegistrationEmail(ctx, tour, confirmedOff)
	s.enqueueTournamentRegistrationEmail(ctx, tour, confirmedOn)

	afterOff := countPendingTournamentEmails(t, store, emailOff)
	afterOn := countPendingTournamentEmails(t, store, emailOn)
	if afterOff != beforeOff {
		t.Fatalf("prefs off must skip outbox: before=%d after=%d", beforeOff, afterOff)
	}
	if afterOn != beforeOn+1 {
		t.Fatalf("prefs on must enqueue: before=%d after=%d", beforeOn, afterOn)
	}
}

func createApprovedPlayer(t *testing.T, store *db.Store, email string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	hash, err := crypto.HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.Queries.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
		Email:        textEmail(email),
		PasswordHash: hash,
		FirstName:    "Test",
		LastName:     "Player",
		Role:         sqlc.UserRolePlayer,
		Status:       sqlc.UserStatusApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queries.CreateProfile(ctx, rating.NewProfileParams(user.ID, clubs.DefaultUUID(), pgtype.Text{}, "challenger", "Test", "Player")); err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func createOpenTournament(t *testing.T, store *db.Store, slug string, max int32) sqlc.Tournament {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	tour, err := store.Queries.CreateTournament(ctx, sqlc.CreateTournamentParams{
		ClubID:               clubs.DefaultUUID(),
		Slug:                 slug,
		Title:                "Prefs Mail " + slug,
		Description:          "Prefs gate integration tournament",
		Type:                 sqlc.TournamentTypeSingles,
		Category:             "challenger",
		Surface:              sqlc.SurfaceTypeHard,
		Format:               sqlc.TournamentFormatOlympic,
		Status:               sqlc.TournamentStatusRegistrationOpen,
		StartsAt:             pgtype.Timestamptz{Time: now.Add(48 * time.Hour), Valid: true},
		EndsAt:               pgtype.Timestamptz{},
		RegistrationOpensAt:  pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true},
		RegistrationClosesAt: pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		VenueName:            "Test Court",
		VenueAddress:         "Test",
		VenueCourts:          2,
		VenueSurface:         sqlc.SurfaceTypeHard,
		EntryFee:             0,
		MaxParticipants:      max,
		CoverUrl:             pgtype.Text{},
		Season:               int32(now.Year()),
		CreatedBy:            pgtype.UUID{},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Queries.CreateTournamentRuleVersion(ctx, sqlc.CreateTournamentRuleVersionParams{
		TournamentID: tour.ID,
		Version:      1,
		Rules:        []byte(`{}`),
		TextMarkdown: "Тестовый регламент prefs mail.",
		CreatedBy:    pgtype.UUID{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tour
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
