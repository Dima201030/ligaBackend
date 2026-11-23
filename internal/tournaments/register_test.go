package tournaments_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Dima201030/ligaBackend/internal/clubs"
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

func createApprovedPlayer(t *testing.T, store *db.Store, email string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	hash, err := crypto.HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.Queries.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
		Email: pgtype.Text{String: email, Valid: true},
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
		Title:                "Capacity Test " + slug,
		Description:          "Integration capacity test tournament",
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
		TextMarkdown: "Тестовый регламент для проверки вместимости турнира.",
		CreatedBy:    pgtype.UUID{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tour
}

func TestCreateRegistration_CapacityAndWaitlist(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	tour := createOpenTournament(t, store, "cap-test-"+itoa(suffix), 2)

	p1 := createApprovedPlayer(t, store, "cap1-"+itoa(suffix)+"@example.com")
	p2 := createApprovedPlayer(t, store, "cap2-"+itoa(suffix)+"@example.com")
	p3 := createApprovedPlayer(t, store, "cap3-"+itoa(suffix)+"@example.com")

	r1, err := tournaments.CreateRegistration(ctx, store, tournaments.CreateRegistrationInput{
		TournamentID: tour.ID, PlayerID: p1,
	})
	if err != nil || r1.Status != sqlc.RegistrationStatusConfirmed {
		t.Fatalf("r1: status=%s err=%v", r1.Status, err)
	}
	r2, err := tournaments.CreateRegistration(ctx, store, tournaments.CreateRegistrationInput{
		TournamentID: tour.ID, PlayerID: p2,
	})
	if err != nil || r2.Status != sqlc.RegistrationStatusConfirmed {
		t.Fatalf("r2: status=%s err=%v", r2.Status, err)
	}
	r3, err := tournaments.CreateRegistration(ctx, store, tournaments.CreateRegistrationInput{
		TournamentID: tour.ID, PlayerID: p3,
	})
	if err != nil || r3.Status != sqlc.RegistrationStatusWaitlist {
		t.Fatalf("r3 waitlist: status=%s err=%v", r3.Status, err)
	}

	if _, err := tournaments.CancelRegistration(ctx, store, tour.ID, p1); err != nil {
		t.Fatal(err)
	}
	promoted, err := store.Queries.GetRegistrationByTournamentPlayer(ctx, sqlc.GetRegistrationByTournamentPlayerParams{
		TournamentID: tour.ID,
		PlayerID:     p3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Status != sqlc.RegistrationStatusConfirmed {
		t.Fatalf("expected promote to confirmed, got %s", promoted.Status)
	}
}

func TestUpdateRegistrationStatus_ConfirmRejectAndDemote(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	tour := createOpenTournament(t, store, "status-test-"+itoa(suffix), 1)

	p1 := createApprovedPlayer(t, store, "st1-"+itoa(suffix)+"@example.com")
	p2 := createApprovedPlayer(t, store, "st2-"+itoa(suffix)+"@example.com")

	r1, err := tournaments.CreateRegistration(ctx, store, tournaments.CreateRegistrationInput{
		TournamentID: tour.ID, PlayerID: p1,
	})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := tournaments.CreateRegistration(ctx, store, tournaments.CreateRegistrationInput{
		TournamentID: tour.ID, PlayerID: p2,
	})
	if err != nil || r2.Status != sqlc.RegistrationStatusWaitlist {
		t.Fatalf("r2 waitlist: status=%s err=%v", r2.Status, err)
	}

	rejected, err := tournaments.UpdateRegistrationStatus(ctx, store, tour.ID, r1.ID, "rejected")
	if err != nil || rejected.Status != sqlc.RegistrationStatusRejected {
		t.Fatalf("reject: status=%s err=%v", rejected.Status, err)
	}
	promoted, err := store.Queries.GetRegistrationByID(ctx, r2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Status != sqlc.RegistrationStatusConfirmed {
		t.Fatalf("expected waitlist promote after reject, got %s", promoted.Status)
	}

	demoted, err := tournaments.UpdateRegistrationStatus(ctx, store, tour.ID, promoted.ID, "waitlist")
	if err != nil || demoted.Status != sqlc.RegistrationStatusWaitlist {
		t.Fatalf("demote: status=%s err=%v", demoted.Status, err)
	}
	still, err := store.Queries.GetRegistrationByID(ctx, demoted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if still.Status != sqlc.RegistrationStatusWaitlist {
		t.Fatalf("demote must not self-promote, got %s", still.Status)
	}

	restored, err := tournaments.UpdateRegistrationStatus(ctx, store, tour.ID, rejected.ID, "confirmed")
	if err != nil || restored.Status != sqlc.RegistrationStatusConfirmed {
		t.Fatalf("restore confirmed: status=%s err=%v", restored.Status, err)
	}

	_, err = tournaments.UpdateRegistrationStatus(ctx, store, tour.ID, demoted.ID, "confirmed")
	if !errors.Is(err, tournaments.ErrCapacityFull) {
		t.Fatalf("expected capacity_full, got %v", err)
	}
}

func TestCreateRegistration_ParallelDoesNotExceedMax(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	const max = 5
	const n = 20
	tour := createOpenTournament(t, store, "par-test-"+itoa(suffix), max)

	players := make([]pgtype.UUID, n)
	for i := 0; i < n; i++ {
		players[i] = createApprovedPlayer(t, store, "par"+itoa(suffix)+"-"+itoa(int64(i))+"@example.com")
	}

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(pid pgtype.UUID) {
			defer wg.Done()
			_, _ = tournaments.CreateRegistration(ctx, store, tournaments.CreateRegistrationInput{
				TournamentID: tour.ID,
				PlayerID:     pid,
			})
		}(players[i])
	}
	wg.Wait()

	occupied, err := store.Queries.CountOccupyingRegistrations(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if occupied > max {
		t.Fatalf("occupied=%d exceeds max=%d", occupied, max)
	}
	if occupied != max {
		t.Fatalf("occupied=%d want %d", occupied, max)
	}
}

func itoa(n int64) string {
	return time.Unix(0, n).Format("150405.000000000")
}
