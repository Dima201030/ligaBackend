package matches_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/clubs"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/matches"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/Dima201030/ligaBackend/internal/tournaments"
	"github.com/Dima201030/ligaBackend/internal/uuidx"
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
		Email: pgtype.Text{String: email, Valid: true},
		PasswordHash: hash,
		FirstName:    "P",
		LastName:     email,
		Role:         sqlc.UserRolePlayer,
		Status:       sqlc.UserStatusApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Queries.CreateProfile(ctx, rating.NewProfileParams(user.ID, clubs.DefaultUUID(), pgtype.Text{}, "challenger", "P", email,
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
		ClubID: clubs.DefaultUUID(),
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

	outcome, err := matches.SubmitScore(ctx, store, matches.ScoreInput{
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
	updated := outcome.Match
	if updated.Status != sqlc.MatchStatusCompleted || !updated.WinnerSide.Valid {
		t.Fatalf("status=%s winner=%v", updated.Status, updated.WinnerSide)
	}
	if outcome.TournamentCompleted {
		t.Fatal("first-round match must not complete the tournament")
	}
	if !outcome.TournamentWentLive {
		t.Fatal("first finished match should promote tournament to live")
	}
	liveTour, err := store.Queries.GetTournamentByID(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if liveTour.Status != sqlc.TournamentStatusLive {
		t.Fatalf("status=%s want live", liveTour.Status)
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

func TestSwapSlotsPlayersAndBye(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	slug := fmt.Sprintf("sw-%d", time.Now().UnixNano())
	tour := createOpenTournament(t, store, slug, 16)
	confirmPlayers(t, store, tour.ID, 5, slug)
	actor := createPlayer(t, store, fmt.Sprintf("org-%s@test.local", slug))

	if _, _, err := matches.Generate(ctx, store, matches.GenerateInput{
		TournamentID: tour.ID,
		Seeding:      "seed",
		ActorID:      actor,
	}); err != nil {
		t.Fatal(err)
	}

	all, err := store.Queries.ListMatchesByTournament(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}

	var playable, byeMatch *sqlc.Match
	var playableSide, byeSide int16
	for i := range all {
		m := all[i]
		if m.RoundIndex != 0 || m.Stage != "main" {
			continue
		}
		sides, err := store.Queries.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{m.ID})
		if err != nil {
			t.Fatal(err)
		}
		s1, s2, ok := pairSidesForTest(sides)
		if !ok {
			continue
		}
		if playable == nil && m.Status == sqlc.MatchStatusScheduled && s1.PlayerID.Valid && s2.PlayerID.Valid {
			playable = &all[i]
			playableSide = 2
		}
		if byeMatch == nil && m.Status == sqlc.MatchStatusBye {
			byeMatch = &all[i]
			if s1.IsBye {
				byeSide = 1
			} else {
				byeSide = 2
			}
		}
	}
	if playable == nil || byeMatch == nil {
		t.Fatal("need playable and bye first-round matches")
	}

	beforePlayable, _ := store.Queries.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{playable.ID})
	beforeBye, _ := store.Queries.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{byeMatch.ID})
	pSide := sideByNum(beforePlayable, playableSide)
	bSide := sideByNum(beforeBye, byeSide)
	if !pSide.PlayerID.Valid || !bSide.IsBye {
		t.Fatalf("precondition failed: playable player=%v bye=%v", pSide.PlayerID.Valid, bSide.IsBye)
	}
	movedPlayer := pSide.PlayerID

	if _, err := matches.SwapSlots(ctx, store, matches.SwapInput{
		TournamentID: tour.ID,
		A:            matches.SlotRef{MatchID: playable.ID, Side: playableSide},
		B:            matches.SlotRef{MatchID: byeMatch.ID, Side: byeSide},
		ActorID:      actor,
	}); err != nil {
		t.Fatal(err)
	}

	afterPlayableMatch, err := store.Queries.GetMatchByID(ctx, playable.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterPlayableMatch.Status != sqlc.MatchStatusBye || !afterPlayableMatch.IsBye {
		t.Fatalf("playable should become bye: status=%s bye=%v", afterPlayableMatch.Status, afterPlayableMatch.IsBye)
	}
	afterByeMatch, err := store.Queries.GetMatchByID(ctx, byeMatch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterByeMatch.Status != sqlc.MatchStatusScheduled || afterByeMatch.IsBye {
		t.Fatalf("bye should become scheduled: status=%s bye=%v", afterByeMatch.Status, afterByeMatch.IsBye)
	}

	afterByeSides, _ := store.Queries.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{byeMatch.ID})
	foundMoved := false
	for _, s := range afterByeSides {
		if s.PlayerID.Valid && uuidEqual(s.PlayerID, movedPlayer) {
			foundMoved = true
		}
	}
	if !foundMoved {
		t.Fatal("player did not move into former bye match")
	}

	// Scoring still works after swap.
	sides, _ := store.Queries.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{byeMatch.ID})
	s1, s2, _ := pairSidesForTest(sides)
	if !s1.PlayerID.Valid || !s2.PlayerID.Valid {
		t.Fatal("former bye match must have two players to score")
	}
	outcome, err := matches.SubmitScore(ctx, store, matches.ScoreInput{
		MatchID: byeMatch.ID,
		Sets: []matches.SetScore{
			{Side1: 6, Side2: 3},
			{Side1: 6, Side2: 4},
		},
		Version: afterByeMatch.Version,
		ActorID: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Match.Status != sqlc.MatchStatusCompleted {
		t.Fatalf("status=%s", outcome.Match.Status)
	}

	// Further swaps locked after results.
	_, err = matches.SwapSlots(ctx, store, matches.SwapInput{
		TournamentID: tour.ID,
		A:            matches.SlotRef{MatchID: playable.ID, Side: 1},
		B:            matches.SlotRef{MatchID: byeMatch.ID, Side: 1},
		ActorID:      actor,
	})
	if err != matches.ErrBracketLocked {
		t.Fatalf("expected lock, got %v", err)
	}
}

func pairSidesForTest(sides []sqlc.MatchSide) (s1, s2 sqlc.MatchSide, ok bool) {
	for _, s := range sides {
		switch s.Side {
		case 1:
			s1 = s
		case 2:
			s2 = s
		}
	}
	return s1, s2, s1.ID.Valid && s2.ID.Valid
}

func sideByNum(sides []sqlc.MatchSide, side int16) sqlc.MatchSide {
	for _, s := range sides {
		if s.Side == side {
			return s
		}
	}
	return sqlc.MatchSide{}
}

func uuidEqual(a, b pgtype.UUID) bool {
	return uuidx.Equal(a, b)
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

func TestFinalScoreCompletesTournamentAndAwards(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	slug := fmt.Sprintf("final-%d", time.Now().UnixNano())
	tour := createOpenTournament(t, store, slug, 2)
	players := confirmPlayers(t, store, tour.ID, 2, slug)
	actor := createPlayer(t, store, fmt.Sprintf("org-%s@test.local", slug))

	// Mark live so complete-if-active is realistic.
	if _, err := store.Queries.UpdateTournament(ctx, sqlc.UpdateTournamentParams{
		ID:                   tour.ID,
		Title:                tour.Title,
		Description:          tour.Description,
		Type:                 tour.Type,
		Category:             tour.Category,
		Surface:              tour.Surface,
		Format:               tour.Format,
		Status:               sqlc.TournamentStatusLive,
		StartsAt:             tour.StartsAt,
		EndsAt:               tour.EndsAt,
		RegistrationOpensAt:  tour.RegistrationOpensAt,
		RegistrationClosesAt: tour.RegistrationClosesAt,
		VenueName:            tour.VenueName,
		VenueAddress:         tour.VenueAddress,
		VenueCourts:          tour.VenueCourts,
		VenueSurface:         tour.VenueSurface,
		EntryFee:             tour.EntryFee,
		MaxParticipants:      tour.MaxParticipants,
		CoverUrl:             tour.CoverUrl,
		Season:               tour.Season,
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := matches.Generate(ctx, store, matches.GenerateInput{
		TournamentID: tour.ID,
		Seeding:      "seed",
		ActorID:      actor,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := matches.Publish(ctx, store, tour.ID, actor); err != nil {
		t.Fatal(err)
	}

	all, err := store.Queries.ListMatchesByTournament(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	var final *sqlc.Match
	for i := range all {
		m := all[i]
		if m.IsBye || m.Status != sqlc.MatchStatusScheduled {
			continue
		}
		if !m.NextMatchID.Valid && m.Stage == "main" {
			final = &m
			break
		}
	}
	if final == nil {
		t.Fatal("expected championship final match")
	}

	outcome, err := matches.SubmitScore(ctx, store, matches.ScoreInput{
		MatchID: final.ID,
		Sets: []matches.SetScore{
			{Side1: 6, Side2: 1},
			{Side1: 6, Side2: 2},
		},
		Version: final.Version,
		ActorID: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.TournamentCompleted {
		t.Fatal("expected tournament_completed")
	}
	if outcome.RatingsApplied < 2 {
		t.Fatalf("ratings_applied=%d want >=2", outcome.RatingsApplied)
	}

	got, err := store.Queries.GetTournamentByID(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sqlc.TournamentStatusCompleted {
		t.Fatalf("status=%s", got.Status)
	}

	for _, pid := range players {
		prof, err := store.Queries.GetProfileByUserID(ctx, pid)
		if err != nil {
			t.Fatal(err)
		}
		if prof.Points <= 0 && prof.SinglesPoints <= 0 {
			t.Fatalf("player %v got no points", pid.Bytes)
		}
	}

	// Stale version on correction must still conflict.
	_, err = matches.Walkover(ctx, store, matches.WalkoverInput{
		MatchID:    final.ID,
		WinnerSide: 1,
		Version:    outcome.Match.Version - 1,
		ActorID:    actor,
	})
	if err != matches.ErrConflict {
		t.Fatalf("stale version expected conflict, got %v", err)
	}

	// Matching version may correct a finished match while the next round is still open
	// (final has no next match — correction is allowed).
	outcome2, err := matches.Walkover(ctx, store, matches.WalkoverInput{
		MatchID:    final.ID,
		WinnerSide: 2,
		Version:    outcome.Match.Version,
		ActorID:    actor,
	})
	if err != nil {
		t.Fatalf("correction walkover: %v", err)
	}
	if !outcome2.Match.WinnerSide.Valid || outcome2.Match.WinnerSide.Int16 != 2 {
		t.Fatalf("corrected winner=%v", outcome2.Match.WinnerSide)
	}
}

func TestDisqualifyFullEloAndZeroPoints(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	slug := fmt.Sprintf("dq-%d", time.Now().UnixNano())
	tour := createOpenTournament(t, store, slug, 2)
	players := confirmPlayers(t, store, tour.ID, 2, slug)
	actor := createPlayer(t, store, fmt.Sprintf("org-%s@test.local", slug))

	if _, _, err := matches.Generate(ctx, store, matches.GenerateInput{
		TournamentID: tour.ID, Seeding: "seed", ActorID: actor,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := matches.Publish(ctx, store, tour.ID, actor); err != nil {
		t.Fatal(err)
	}
	all, err := store.Queries.ListMatchesByTournament(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	var final sqlc.Match
	for _, m := range all {
		if !m.IsBye && m.Status == sqlc.MatchStatusScheduled && !m.NextMatchID.Valid {
			final = m
			break
		}
	}
	if !final.ID.Valid {
		t.Fatal("no final")
	}

	sides, err := store.Queries.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{final.ID})
	if err != nil || len(sides) != 2 {
		t.Fatalf("sides: %v len=%d", err, len(sides))
	}
	var winnerID, loserID pgtype.UUID
	for _, s := range sides {
		if s.Side == 1 {
			winnerID = s.PlayerID
		} else {
			loserID = s.PlayerID
		}
	}

	outcome, err := matches.Disqualify(ctx, store, matches.DisqualifyInput{
		MatchID:    final.ID,
		WinnerSide: 1,
		Reason:     "нарушение",
		Version:    final.Version,
		ActorID:    actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Match.Status != sqlc.MatchStatusDisqualified {
		t.Fatalf("status=%s", outcome.Match.Status)
	}

	// Full Elo (~±16 at equal 1200 provisional).
	var sawDQ bool
	for _, pid := range []pgtype.UUID{winnerID, loserID} {
		events, err := store.Queries.ListRatingEventsByPlayer(ctx, sqlc.ListRatingEventsByPlayerParams{
			PlayerID: pid, Limit: 5,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range events {
			if ev.Reason == "match dq" {
				sawDQ = true
				if absFloat(ev.RatingDelta) < 15 || absFloat(ev.RatingDelta) > 17 {
					t.Fatalf("dq delta=%v want ~16", ev.RatingDelta)
				}
			}
		}
	}
	if !sawDQ {
		t.Fatal("expected match dq elo events")
	}

	if !outcome.TournamentCompleted {
		// Final DQ should auto-complete + award.
		if _, err := rating.ApplyTournamentPoints(ctx, store, rating.TournamentApplyInput{
			TournamentID: tour.ID,
			Season:       tour.Season,
			Category:     tour.Category,
			Type:         string(tour.Type),
			ActorID:      actor,
		}); err != nil {
			t.Fatal(err)
		}
	}
	loserProf, err := store.Queries.GetProfileByUserID(ctx, loserID)
	if err != nil {
		t.Fatal(err)
	}
	if loserProf.Points != 0 || loserProf.SinglesPoints != 0 {
		t.Fatalf("DQ'd player points=%d singles=%d want 0", loserProf.Points, loserProf.SinglesPoints)
	}
	_ = players
}

func TestRetireAppliesHalfElo(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	slug := fmt.Sprintf("ret-%d", time.Now().UnixNano())
	tour := createOpenTournament(t, store, slug, 2)
	players := confirmPlayers(t, store, tour.ID, 2, slug)
	actor := createPlayer(t, store, fmt.Sprintf("org-%s@test.local", slug))

	if _, _, err := matches.Generate(ctx, store, matches.GenerateInput{
		TournamentID: tour.ID, Seeding: "seed", ActorID: actor,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := matches.Publish(ctx, store, tour.ID, actor); err != nil {
		t.Fatal(err)
	}
	all, err := store.Queries.ListMatchesByTournament(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	var final sqlc.Match
	for _, m := range all {
		if !m.IsBye && m.Status == sqlc.MatchStatusScheduled && !m.NextMatchID.Valid {
			final = m
			break
		}
	}
	if !final.ID.Valid {
		t.Fatal("no final")
	}

	before := make(map[string]float64, 2)
	for _, pid := range players {
		prof, err := store.Queries.GetProfileByUserID(ctx, pid)
		if err != nil {
			t.Fatal(err)
		}
		before[fmt.Sprintf("%x", pid.Bytes)] = prof.Rating
	}

	outcome, err := matches.Retire(ctx, store, matches.RetireInput{
		MatchID:    final.ID,
		WinnerSide: 1,
		Sets:       []matches.SetScore{{Side1: 4, Side2: 2}},
		Reason:     "травма",
		Version:    final.Version,
		ActorID:    actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Match.Status != sqlc.MatchStatusCompleted {
		t.Fatalf("status=%s want completed", outcome.Match.Status)
	}
	if !strings.Contains(outcome.Match.ScoreText.String, "снятие") {
		t.Fatalf("score_text=%q", outcome.Match.ScoreText.String)
	}

	events, err := store.Queries.ListRatingEventsByPlayer(ctx, sqlc.ListRatingEventsByPlayerParams{
		PlayerID: players[0], Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range events {
		if ev.MatchID.Valid && ev.Reason == "match retire" {
			found = true
			// Equal 1200 start, provisional K=32 → retire K=16 → |delta|=8
			if absFloat(ev.RatingDelta) < 7.5 || absFloat(ev.RatingDelta) > 8.5 {
				t.Fatalf("retire delta=%v want ~8", ev.RatingDelta)
			}
		}
	}
	if !found {
		// winner might be side1 which is players order from confirm — check both
		for _, pid := range players {
			events, _ = store.Queries.ListRatingEventsByPlayer(ctx, sqlc.ListRatingEventsByPlayerParams{
				PlayerID: pid, Limit: 5,
			})
			for _, ev := range events {
				if ev.Reason == "match retire" {
					found = true
					if absFloat(ev.RatingDelta) < 7.5 || absFloat(ev.RatingDelta) > 8.5 {
						t.Fatalf("retire delta=%v want ~8", ev.RatingDelta)
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("expected match retire rating event")
	}
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestRoundRobinCompletesWhenAllMatchesClosed(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	slug := fmt.Sprintf("rr-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	tour, err := store.Queries.CreateTournament(ctx, sqlc.CreateTournamentParams{
		ClubID: clubs.DefaultUUID(),
		Slug:                 slug,
		Title:                "RR " + slug,
		Description:          "rr",
		Type:                 sqlc.TournamentTypeSingles,
		Category:             "futures",
		Surface:              sqlc.SurfaceTypeHard,
		Format:               sqlc.TournamentFormatRoundRobin,
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
		MaxParticipants:      4,
		CoverUrl:             pgtype.Text{},
		Season:               int32(now.Year()),
		CreatedBy:            pgtype.UUID{},
	})
	if err != nil {
		t.Fatal(err)
	}
	confirmPlayers(t, store, tour.ID, 3, slug)
	actor := createPlayer(t, store, fmt.Sprintf("org-%s@test.local", slug))

	if _, _, err := matches.Generate(ctx, store, matches.GenerateInput{
		TournamentID: tour.ID,
		Seeding:      "seed",
		ActorID:      actor,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := matches.Publish(ctx, store, tour.ID, actor); err != nil {
		t.Fatal(err)
	}

	all, err := store.Queries.ListMatchesByTournament(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	playable := make([]sqlc.Match, 0, len(all))
	for _, m := range all {
		if m.IsBye || m.Status != sqlc.MatchStatusScheduled {
			continue
		}
		sides, err := store.Queries.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{m.ID})
		if err != nil {
			t.Fatal(err)
		}
		ok := len(sides) == 2
		for _, s := range sides {
			if !s.PlayerID.Valid {
				ok = false
			}
		}
		if ok {
			playable = append(playable, m)
		}
	}
	if len(playable) < 2 {
		t.Fatalf("expected several RR matches, got %d", len(playable))
	}

	var last matches.FinishOutcome
	for i, m := range playable {
		// Refresh version in case of earlier updates (shouldn't change until scored).
		cur, err := store.Queries.GetMatchByID(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		out, err := matches.SubmitScore(ctx, store, matches.ScoreInput{
			MatchID: cur.ID,
			Sets: []matches.SetScore{
				{Side1: 6, Side2: 3},
				{Side1: 6, Side2: 4},
			},
			Version: cur.Version,
			ActorID: actor,
		})
		if err != nil {
			t.Fatalf("match %d: %v", i, err)
		}
		last = out
		if i < len(playable)-1 {
			if out.TournamentCompleted {
				t.Fatalf("match %d/%d completed tournament early", i+1, len(playable))
			}
			got, err := store.Queries.GetTournamentByID(ctx, tour.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != sqlc.TournamentStatusLive {
				t.Fatalf("after match %d status=%s want live", i+1, got.Status)
			}
		}
	}
	if !last.TournamentCompleted {
		t.Fatal("last RR match should complete tournament")
	}
	if last.RatingsApplied < 1 {
		t.Fatalf("ratings_applied=%d", last.RatingsApplied)
	}
	got, err := store.Queries.GetTournamentByID(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sqlc.TournamentStatusCompleted {
		t.Fatalf("status=%s", got.Status)
	}
}
