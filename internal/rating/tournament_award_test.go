package rating_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/clubs"
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
		if _, err := store.Queries.CreateProfile(ctx, rating.NewProfileParams(user.ID, clubs.DefaultUUID(), pgtype.Text{}, "futures", "A", "B",
		)); err != nil {
			t.Fatal(err)
		}
		return user.ID
	}

	p1 := mkPlayer("award-p1-" + suffix + "@example.com")
	p2 := mkPlayer("award-p2-" + suffix + "@example.com")
	now := time.Now().UTC()
	tour, err := store.Queries.CreateTournament(ctx, sqlc.CreateTournamentParams{
		ClubID: clubs.DefaultUUID(),
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

func TestApplyTournamentPointsWalkoverOnlyHalves(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	mk := func(email string) pgtype.UUID {
		t.Helper()
		hash, err := crypto.HashPassword("password123")
		if err != nil {
			t.Fatal(err)
		}
		user, err := store.Queries.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
			Email: email, PasswordHash: hash, FirstName: "W", LastName: "O",
			Role: sqlc.UserRolePlayer, Status: sqlc.UserStatusApproved,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Queries.CreateProfile(ctx, rating.NewProfileParams(user.ID, clubs.DefaultUUID(), pgtype.Text{}, "futures", "W", "O",
		)); err != nil {
			t.Fatal(err)
		}
		return user.ID
	}
	p1 := mk("wo-a-" + suffix + "@example.com")
	p2 := mk("wo-b-" + suffix + "@example.com")
	now := time.Now().UTC()
	tour, err := store.Queries.CreateTournament(ctx, sqlc.CreateTournamentParams{
		ClubID: clubs.DefaultUUID(),
		Slug: "wo-" + suffix, Title: "Walkover Award Test", Description: "walkover-only award integration",
		Type: sqlc.TournamentTypeSingles, Category: "futures",
		Surface: sqlc.SurfaceTypeHard, Format: sqlc.TournamentFormatOlympic,
		Status: sqlc.TournamentStatusLive,
		StartsAt: pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true},
		EndsAt:   pgtype.Timestamptz{},
		RegistrationOpensAt:  pgtype.Timestamptz{Time: now.Add(-48 * time.Hour), Valid: true},
		RegistrationClosesAt: pgtype.Timestamptz{Time: now.Add(-2 * time.Hour), Valid: true},
		VenueName: "C", VenueAddress: "A", VenueCourts: 1, VenueSurface: sqlc.SurfaceTypeHard,
		EntryFee: 0, MaxParticipants: 2, CoverUrl: pgtype.Text{}, Season: int32(now.Year()), CreatedBy: p1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, pid := range []pgtype.UUID{p1, p2} {
		if _, err := store.Queries.CreateRegistration(ctx, sqlc.CreateRegistrationParams{
			TournamentID: tour.ID, PlayerID: pid, PartnerID: pgtype.UUID{},
			LookingForPartner: false, Status: sqlc.RegistrationStatusConfirmed,
			WaitlistPosition: pgtype.Int4{}, ConfirmedAt: pgtype.Timestamptz{Time: now, Valid: true},
		}); err != nil {
			t.Fatal(err)
		}
	}

	match, err := store.Queries.CreateMatch(ctx, sqlc.CreateMatchParams{
		TournamentID: tour.ID, BracketKey: "R1M1", Stage: "main",
		RoundIndex: 0, Position: 0, RoundLabel: "Финал",
		Status: sqlc.MatchStatusWalkover,
		WinnerSide: pgtype.Int2{Int16: 1, Valid: true},
		IsBye: false, NextSlot: pgtype.Int2{}, LoserNextSlot: pgtype.Int2{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queries.CreateMatchSide(ctx, sqlc.CreateMatchSideParams{
		MatchID: match.ID, Side: 1, PlayerID: p1, PartnerID: pgtype.UUID{},
		RegistrationID: pgtype.UUID{}, Seed: pgtype.Int4{}, Label: pgtype.Text{}, IsBye: false,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queries.CreateMatchSide(ctx, sqlc.CreateMatchSideParams{
		MatchID: match.ID, Side: 2, PlayerID: p2, PartnerID: pgtype.UUID{},
		RegistrationID: pgtype.UUID{}, Seed: pgtype.Int4{}, Label: pgtype.Text{}, IsBye: false,
	}); err != nil {
		t.Fatal(err)
	}

	full := rating.TournamentPoints(rating.TournamentPointsInput{
		Wins: 1, Participants: 2, Category: "futures", Type: "singles",
	}, rating.DefaultCoeffs)
	half := rating.TournamentPoints(rating.TournamentPointsInput{
		Wins: 1, Participants: 2, Category: "futures", Type: "singles", WalkoverOnly: true,
	}, rating.DefaultCoeffs)
	if half*2 != full && half != full/2 && half != (full+1)/2 {
		// allow rounding: half should be ~50% of full
		if half < full/3 || half > full*2/3 {
			t.Fatalf("unexpected half=%d full=%d", half, full)
		}
	}

	n, err := rating.ApplyTournamentPoints(ctx, store, rating.TournamentApplyInput{
		TournamentID: tour.ID, Season: tour.Season, Category: tour.Category,
		Type: string(tour.Type), ActorID: p1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("applied=%d", n)
	}
	winner, err := store.Queries.GetProfileByUserID(ctx, p1)
	if err != nil {
		t.Fatal(err)
	}
	if int(winner.SinglesPoints) != half {
		t.Fatalf("winner points=%d want walkover-halved %d (full would be %d)", winner.SinglesPoints, half, full)
	}
}

func TestApplyTournamentPointsConsolation(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	mk := func(email string) pgtype.UUID {
		t.Helper()
		hash, _ := crypto.HashPassword("password123")
		user, err := store.Queries.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
			Email: email, PasswordHash: hash, FirstName: "C", LastName: "S",
			Role: sqlc.UserRolePlayer, Status: sqlc.UserStatusApproved,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Queries.CreateProfile(ctx, rating.NewProfileParams(user.ID, clubs.DefaultUUID(), pgtype.Text{}, "futures", "C", "S",
		)); err != nil {
			t.Fatal(err)
		}
		return user.ID
	}
	p1 := mk("cs-a-" + suffix + "@example.com")
	p2 := mk("cs-b-" + suffix + "@example.com")
	now := time.Now().UTC()
	tour, err := store.Queries.CreateTournament(ctx, sqlc.CreateTournamentParams{
		ClubID: clubs.DefaultUUID(),
		Slug: "cs-" + suffix, Title: "Consolation Award Test", Description: "consolation award integration",
		Type: sqlc.TournamentTypeSingles, Category: "futures",
		Surface: sqlc.SurfaceTypeHard, Format: sqlc.TournamentFormatOlympic,
		Status: sqlc.TournamentStatusCompleted,
		StartsAt: pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true},
		RegistrationOpensAt:  pgtype.Timestamptz{Time: now.Add(-48 * time.Hour), Valid: true},
		RegistrationClosesAt: pgtype.Timestamptz{Time: now.Add(-2 * time.Hour), Valid: true},
		VenueName: "C", VenueAddress: "A", VenueCourts: 1, VenueSurface: sqlc.SurfaceTypeHard,
		EntryFee: 0, MaxParticipants: 4, CoverUrl: pgtype.Text{}, Season: int32(now.Year()), CreatedBy: p1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, pid := range []pgtype.UUID{p1, p2} {
		if _, err := store.Queries.CreateRegistration(ctx, sqlc.CreateRegistrationParams{
			TournamentID: tour.ID, PlayerID: pid,
			LookingForPartner: false, Status: sqlc.RegistrationStatusConfirmed,
			ConfirmedAt: pgtype.Timestamptz{Time: now, Valid: true},
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Only consolation-stage wins for p1 (no primary) → consolation factor.
	match, err := store.Queries.CreateMatch(ctx, sqlc.CreateMatchParams{
		TournamentID: tour.ID, BracketKey: "CS1M1", Stage: "consolation",
		RoundIndex: 0, Position: 0, RoundLabel: "Утешительная",
		Status: sqlc.MatchStatusCompleted,
		WinnerSide: pgtype.Int2{Int16: 1, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	for side, pid := range map[int16]pgtype.UUID{1: p1, 2: p2} {
		if _, err := store.Queries.CreateMatchSide(ctx, sqlc.CreateMatchSideParams{
			MatchID: match.ID, Side: side, PlayerID: pid,
		}); err != nil {
			t.Fatal(err)
		}
	}

	want := rating.TournamentPoints(rating.TournamentPointsInput{
		Wins: 1, Participants: 2, Category: "futures", Type: "singles", Consolation: true,
	}, rating.DefaultCoeffs)

	if _, err := rating.ApplyTournamentPoints(ctx, store, rating.TournamentApplyInput{
		TournamentID: tour.ID, Season: tour.Season, Category: tour.Category,
		Type: string(tour.Type), ActorID: p1,
	}); err != nil {
		t.Fatal(err)
	}
	winner, err := store.Queries.GetProfileByUserID(ctx, p1)
	if err != nil {
		t.Fatal(err)
	}
	if int(winner.SinglesPoints) != want {
		t.Fatalf("points=%d want consolation %d", winner.SinglesPoints, want)
	}
}

func TestApplyTournamentPointsDEPlaces(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	mk := func(email string) pgtype.UUID {
		t.Helper()
		hash, _ := crypto.HashPassword("password123")
		user, err := store.Queries.CreateUserVerified(ctx, sqlc.CreateUserVerifiedParams{
			Email: email, PasswordHash: hash, FirstName: "D", LastName: "E",
			Role: sqlc.UserRolePlayer, Status: sqlc.UserStatusApproved,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Queries.CreateProfile(ctx, rating.NewProfileParams(user.ID, clubs.DefaultUUID(), pgtype.Text{}, "futures", "D", "E",
		)); err != nil {
			t.Fatal(err)
		}
		return user.ID
	}
	champ := mk("de-c-" + suffix + "@example.com")
	finalist := mk("de-f-" + suffix + "@example.com")
	third := mk("de-3-" + suffix + "@example.com")
	fourth := mk("de-4-" + suffix + "@example.com")
	now := time.Now().UTC()
	tour, err := store.Queries.CreateTournament(ctx, sqlc.CreateTournamentParams{
		ClubID: clubs.DefaultUUID(),
		Slug: "de-" + suffix, Title: "DE Place Award", Description: "de place-based awards",
		Type: sqlc.TournamentTypeSingles, Category: "futures",
		Surface: sqlc.SurfaceTypeHard, Format: sqlc.TournamentFormatDoubleElimination,
		Status: sqlc.TournamentStatusCompleted,
		StartsAt: pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true},
		RegistrationOpensAt:  pgtype.Timestamptz{Time: now.Add(-48 * time.Hour), Valid: true},
		RegistrationClosesAt: pgtype.Timestamptz{Time: now.Add(-2 * time.Hour), Valid: true},
		VenueName: "C", VenueAddress: "A", VenueCourts: 1, VenueSurface: sqlc.SurfaceTypeHard,
		EntryFee: 0, MaxParticipants: 4, CoverUrl: pgtype.Text{}, Season: int32(now.Year()), CreatedBy: champ,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, pid := range []pgtype.UUID{champ, finalist, third, fourth} {
		if _, err := store.Queries.CreateRegistration(ctx, sqlc.CreateRegistrationParams{
			TournamentID: tour.ID, PlayerID: pid,
			LookingForPartner: false, Status: sqlc.RegistrationStatusConfirmed,
			ConfirmedAt: pgtype.Timestamptz{Time: now, Valid: true},
		}); err != nil {
			t.Fatal(err)
		}
	}
	mkMatch := func(key, stage string, round, pos int32, wSide int16, a, b pgtype.UUID) {
		t.Helper()
		m, err := store.Queries.CreateMatch(ctx, sqlc.CreateMatchParams{
			TournamentID: tour.ID, BracketKey: key, Stage: stage,
			RoundIndex: round, Position: pos, RoundLabel: stage,
			Status: sqlc.MatchStatusCompleted,
			WinnerSide: pgtype.Int2{Int16: wSide, Valid: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		for side, pid := range map[int16]pgtype.UUID{1: a, 2: b} {
			if _, err := store.Queries.CreateMatchSide(ctx, sqlc.CreateMatchSideParams{
				MatchID: m.ID, Side: side, PlayerID: pid,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// LB final: finalist beats third → third = place 3
	mkMatch("LB1", "lower", 1, 0, 1, finalist, third)
	// GF: champ beats finalist → places 1 / 2; fourth unplaced → 4
	mkMatch("GF", "grand_final", 0, 0, 1, champ, finalist)

	want := map[string]int{
		"champ": rating.TournamentPoints(rating.TournamentPointsInput{
			Wins: 4, Participants: 4, Category: "futures", Type: "singles",
		}, rating.DefaultCoeffs),
		"finalist": rating.TournamentPoints(rating.TournamentPointsInput{
			Wins: 3, Participants: 4, Category: "futures", Type: "singles",
		}, rating.DefaultCoeffs),
		"third": rating.TournamentPoints(rating.TournamentPointsInput{
			Wins: 2, Participants: 4, Category: "futures", Type: "singles",
		}, rating.DefaultCoeffs),
		"fourth": rating.TournamentPoints(rating.TournamentPointsInput{
			Wins: 2, Participants: 4, Category: "futures", Type: "singles",
		}, rating.DefaultCoeffs),
	}

	if _, err := rating.ApplyTournamentPoints(ctx, store, rating.TournamentApplyInput{
		TournamentID: tour.ID, ActorID: champ,
	}); err != nil {
		t.Fatal(err)
	}
	check := func(label string, id pgtype.UUID, expect int) {
		t.Helper()
		p, err := store.Queries.GetProfileByUserID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if int(p.SinglesPoints) != expect {
			t.Fatalf("%s points=%d want %d", label, p.SinglesPoints, expect)
		}
	}
	check("champ", champ, want["champ"])
	check("finalist", finalist, want["finalist"])
	check("third", third, want["third"])
	check("fourth", fourth, want["fourth"])
}
