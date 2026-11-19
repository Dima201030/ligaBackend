package rating_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/rating"
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

func TestApplyTournamentPointsIdempotentAndIncludesPartner(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	mkPlayer := func(email string) pgtype.UUID {
		t.Helper()
		hash, err := crypto.HashPassword("password123")
		if err != nil {
			t.Fatal(err)
		}
		user, err := store.Queries.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
			Email:        email,
			PasswordHash: hash,
			FirstName:    "A",
			LastName:     "B",
			Role:         sqlc.UserRolePlayer,
			Status:       sqlc.UserStatusApproved,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Queries.CreateProfile(ctx, rating.NewProfileParams(
			user.ID, pgtype.Text{}, "futures", "A", "B",
		)); err != nil {
			t.Fatal(err)
		}
		return user.ID
	}

	p1 := mkPlayer("award-p1-" + suffix + "@example.com")
	p2 := mkPlayer("award-p2-" + suffix + "@example.com")
	now := time.Now().UTC()
	tour, err := store.Queries.CreateTournament(ctx, sqlc.CreateTournamentParams{
		Slug:                 "award-" + suffix,
		Title:                "Award Test",
		Description:          "d",
		Type:                 sqlc.TournamentTypeDoubles,
		Category:             "futures",
		Surface:              sqlc.SurfaceTypeHard,
		Format:               sqlc.TournamentFormatOlympic,
		Status:               sqlc.TournamentStatusLive,
		StartsAt:             pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true},
		EndsAt:               pgtype.Timestamptz{},
		RegistrationOpensAt:  pgtype.Timestamptz{Time: now.Add(-48 * time.Hour), Valid: true},
		RegistrationClosesAt: pgtype.Timestamptz{Time: now.Add(-2 * time.Hour), Valid: true},
		VenueName:            "Court",
		VenueAddress:         "Addr",
		VenueCourts:          2,
		VenueSurface:         sqlc.SurfaceTypeHard,
		EntryFee:             0,
		MaxParticipants:      16,
		CoverUrl:             pgtype.Text{},
		Season:               int32(now.Year()),
		CreatedBy:            p1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queries.CreateRegistration(ctx, sqlc.CreateRegistrationParams{
		TournamentID:      tour.ID,
		PlayerID:          p1,
		PartnerID:         p2,
		LookingForPartner: false,
		Status:            sqlc.RegistrationStatusConfirmed,
		WaitlistPosition:  pgtype.Int4{},
		ConfirmedAt:       pgtype.Timestamptz{Time: now, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	in := rating.TournamentApplyInput{
		TournamentID: tour.ID,
		Season:       tour.Season,
		Category:     tour.Category,
		Type:         string(tour.Type),
		ActorID:      p1,
	}
	n, err := rating.ApplyTournamentPoints(ctx, store, in)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("first apply: got %d want 2 (player+partner)", n)
	}
	n2, err := rating.ApplyTournamentPoints(ctx, store, in)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Fatalf("second apply should be idempotent, got %d", n2)
	}

	for _, id := range []pgtype.UUID{p1, p2} {
		prof, err := store.Queries.GetProfileByUserID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if prof.DoublesPoints <= 0 || prof.Points != prof.DoublesPoints {
			t.Fatalf("player %v: points=%d singles=%d doubles=%d", id.Bytes, prof.Points, prof.SinglesPoints, prof.DoublesPoints)
		}
	}
}
