package matches_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/matches"
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

func createPlayer(t *testing.T, store *db.Store, email string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	hash, err := crypto.HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.Queries.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
		Email:        email,
		PasswordHash: hash,
		FirstName:    "P",
		LastName:     email,
		Role:         sqlc.UserRolePlayer,
		Status:       sqlc.UserStatusApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queries.CreateProfile(ctx, rating.NewProfileParams(
		user.ID, pgtype.Text{}, "challenger", "P", email,
	)); err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func createOpenTournament(t *testing.T, store *db.Store, slug string, max int32) sqlc.Tournament {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	tour, err := store.Queries.CreateTournament(ctx, sqlc.CreateTournamentParams{
		Slug:                 slug,
		Title:                "Bracket Test " + slug,
		Description:          "integration",
		Type:                 sqlc.TournamentTypeSingles,
		Category:             "challenger",
		Surface:              sqlc.SurfaceTypeHard,
		Format:               sqlc.TournamentFormatOlympic,
		Status:               sqlc.TournamentStatusRegistrationOpen,
		StartsAt:             pgtype.Timestamptz{Time: now.Add(48 * time.Hour), Valid: true},
		EndsAt:               pgtype.Timestamptz{},
		RegistrationOpensAt:  pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true},
		RegistrationClosesAt: pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		VenueName:            "Test",
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
	return tour
}

func confirmPlayers(t *testing.T, store *db.Store, tourID pgtype.UUID, n int, prefix string) []pgtype.UUID {
	t.Helper()
	ids := make([]pgtype.UUID, 0, n)
	for i := 0; i < n; i++ {
		id := createPlayer(t, store, fmt.Sprintf("%s-%d-%d@test.local", prefix, time.Now().UnixNano(), i))
		ids = append(ids, id)
		_, err := tournaments.CreateRegistration(context.Background(), store, tournaments.CreateRegistrationInput{
			TournamentID: tourID,
			PlayerID:     id,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

func TestGenerateByeAndScoreAdvance(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	slug := fmt.Sprintf("br-%d", time.Now().UnixNano())
	tour := createOpenTournament(t, store, slug, 16)
	confirmPlayers(t, store, tour.ID, 5, slug)

	actor := createPlayer(t, store, fmt.Sprintf("org-%s@test.local", slug))
	tour, draw, err := matches.Generate(ctx, store, matches.GenerateInput{
		TournamentID: tour.ID,
		ThirdPlace:   true,
		Seeding:      "seed",
		ActorID:      actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if draw.Byes != 3 {
		t.Fatalf("byes=%d", draw.Byes)
	}
	if tour.BracketStatus != sqlc.BracketStatusDraft {
		t.Fatalf("status=%s", tour.BracketStatus)
	}

	if _, err := matches.Publish(ctx, store, tour.ID, actor); err != nil {
		t.Fatal(err)
	}

	all, err := store.Queries.ListMatchesByTournament(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	var playable *sqlc.Match
	for i := range all {
		m := all[i]
		if m.Status != sqlc.MatchStatusScheduled || m.IsBye {
			continue
		}
		sides, err := store.Queries.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{m.ID})
		if err != nil {
			t.Fatal(err)
		}
		ok := true
		for _, s := range sides {
			if !s.PlayerID.Valid {
				ok = false
			}
		}
		if ok && len(sides) == 2 {
			playable = &m
			break
		}
	}
	if playable == nil {
		t.Fatal("no playable first-round match")
	}

	updated, err := matches.SubmitScore(ctx, store, matches.ScoreInput{
		MatchID: playable.ID,
		Sets: []matches.SetScore{
			{Side1: 6, Side2: 4},
			{Side1: 6, Side2: 2},
		},
		Version: playable.Version,
		ActorID: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != sqlc.MatchStatusCompleted || !updated.WinnerSide.Valid {
		t.Fatalf("status=%s winner=%v", updated.Status, updated.WinnerSide)
	}

	if !playable.NextMatchID.Valid {
		t.Fatal("expected next match")
	}
	sides, err := store.Queries.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{playable.NextMatchID})
	if err != nil {
		t.Fatal(err)
	}
	advanced := false
	for _, s := range sides {
		if s.Side == playable.NextSlot.Int16 && s.PlayerID.Valid {
			advanced = true
		}
	}
	if !advanced {
		t.Fatal("winner was not advanced to next match")
	}

	// Regenerating after results must be locked.
	_, _, err = matches.Generate(ctx, store, matches.GenerateInput{
		TournamentID: tour.ID,
		ActorID:      actor,
	})
	if err != matches.ErrBracketLocked {
		t.Fatalf("expected lock, got %v", err)
	}
}

func TestParallelScoreConflict(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	slug := fmt.Sprintf("par-%d", time.Now().UnixNano())
	tour := createOpenTournament(t, store, slug, 8)
	confirmPlayers(t, store, tour.ID, 4, slug)
	actor := createPlayer(t, store, fmt.Sprintf("org-%s@test.local", slug))

	_, _, err := matches.Generate(ctx, store, matches.GenerateInput{
		TournamentID: tour.ID,
		Seeding:      "seed",
		ActorID:      actor,
	})
	if err != nil {
		t.Fatal(err)
	}

	all, err := store.Queries.ListMatchesByTournament(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	var target sqlc.Match
	found := false
	for _, m := range all {
		if m.Status != sqlc.MatchStatusScheduled || m.IsBye {
			continue
		}
		sides, _ := store.Queries.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{m.ID})
		if len(sides) == 2 && sides[0].PlayerID.Valid && sides[1].PlayerID.Valid {
			target = m
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no target match")
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := matches.SubmitScore(ctx, store, matches.ScoreInput{
				MatchID: target.ID,
				Sets: []matches.SetScore{
					{Side1: 6, Side2: 3},
					{Side1: 6, Side2: 4},
				},
				Version: target.Version,
				ActorID: actor,
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	var okCount, conflictCount int
	for err := range errs {
		switch {
		case err == nil:
			okCount++
		case err == matches.ErrConflict:
			conflictCount++
		default:
			t.Fatalf("unexpected err: %v", err)
		}
	}
	if okCount != 1 || conflictCount != 1 {
		t.Fatalf("ok=%d conflict=%d", okCount, conflictCount)
	}

	// Bracket integrity: next match should have exactly one advanced player for the slot.
	m, err := store.Queries.GetMatchByID(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !m.NextMatchID.Valid {
		return
	}
	sides, err := store.Queries.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{m.NextMatchID})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, s := range sides {
		if s.Side == m.NextSlot.Int16 && s.PlayerID.Valid {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("next slot player count=%d", count)
	}
}
