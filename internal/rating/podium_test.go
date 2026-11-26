package rating

import (
	"testing"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestDerivePodiumKnockoutFinalAndThird(t *testing.T) {
	p := func(n byte) pgtype.UUID {
		var b [16]byte
		b[15] = n
		return pgtype.UUID{Bytes: b, Valid: true}
	}
	a, b, c, d := p(1), p(2), p(3), p(4)
	mk := func(id byte, stage string, round int32, next *byte, winner int16) sqlc.Match {
		var mid [16]byte
		mid[0] = id
		m := sqlc.Match{
			ID: pgtype.UUID{Bytes: mid, Valid: true}, Stage: stage,
			RoundIndex: round, Position: 0,
			Status: sqlc.MatchStatusCompleted,
			WinnerSide: pgtype.Int2{Int16: winner, Valid: true},
		}
		if next != nil {
			var nid [16]byte
			nid[0] = *next
			m.NextMatchID = pgtype.UUID{Bytes: nid, Valid: true}
		}
		return m
	}
	side := func(matchID byte, side int16, player pgtype.UUID) sqlc.MatchSide {
		var mid [16]byte
		mid[0] = matchID
		return sqlc.MatchSide{
			MatchID: pgtype.UUID{Bytes: mid, Valid: true},
			Side:    side, PlayerID: player,
		}
	}
	finalID := byte(30)
	matches := []sqlc.Match{
		mk(10, "playoff", 0, &finalID, 1), // A beats C → C bronze candidate
		mk(20, "playoff", 0, &finalID, 2), // D beats B → B bronze candidate
		mk(30, "playoff", 1, nil, 2),      // B? wait final: side1 A vs side2 D, D wins
		mk(40, "third_place", 0, nil, 1),  // C beats B → C = 3
	}
	sides := []sqlc.MatchSide{
		side(10, 1, a), side(10, 2, c),
		side(20, 1, b), side(20, 2, d),
		side(30, 1, a), side(30, 2, d),
		side(40, 1, c), side(40, 2, b),
	}
	got := DerivePodiumPlaces(sqlc.TournamentFormatGroupsPlayoff, matches, sides)
	want := map[int][]pgtype.UUID{1: {d}, 2: {a}, 3: {c}}
	assertPodium(t, got, want)
}

func TestDerivePodiumSharedBronzeWithoutThirdMatch(t *testing.T) {
	p := func(n byte) pgtype.UUID {
		var b [16]byte
		b[15] = n
		return pgtype.UUID{Bytes: b, Valid: true}
	}
	a, b, c, d := p(1), p(2), p(3), p(4)
	mk := func(id byte, stage string, round int32, next *byte, winner int16) sqlc.Match {
		var mid [16]byte
		mid[0] = id
		m := sqlc.Match{
			ID: pgtype.UUID{Bytes: mid, Valid: true}, Stage: stage,
			RoundIndex: round,
			Status:     sqlc.MatchStatusCompleted,
			WinnerSide: pgtype.Int2{Int16: winner, Valid: true},
		}
		if next != nil {
			var nid [16]byte
			nid[0] = *next
			m.NextMatchID = pgtype.UUID{Bytes: nid, Valid: true}
		}
		return m
	}
	side := func(matchID byte, side int16, player pgtype.UUID) sqlc.MatchSide {
		var mid [16]byte
		mid[0] = matchID
		return sqlc.MatchSide{
			MatchID: pgtype.UUID{Bytes: mid, Valid: true},
			Side:    side, PlayerID: player,
		}
	}
	finalID := byte(30)
	// Final: A beats D → A=1 D=2; SF losers C and B share 3.
	matches := []sqlc.Match{
		mk(10, "main", 0, &finalID, 1),
		mk(20, "main", 0, &finalID, 1),
		mk(30, "main", 1, nil, 1),
	}
	sides := []sqlc.MatchSide{
		side(10, 1, a), side(10, 2, c),
		side(20, 1, d), side(20, 2, b),
		side(30, 1, a), side(30, 2, d),
	}
	got := DerivePodiumPlaces(sqlc.TournamentFormatOlympic, matches, sides)
	assertPodium(t, got, map[int][]pgtype.UUID{1: {a}, 2: {d}, 3: {b, c}})
}

func TestDerivePodiumNoDuplicatePlayers(t *testing.T) {
	// Degenerate: same player somehow on both sides should still appear once.
	p := func(n byte) pgtype.UUID {
		var b [16]byte
		b[15] = n
		return pgtype.UUID{Bytes: b, Valid: true}
	}
	a := p(1)
	var mid [16]byte
	mid[0] = 1
	m := sqlc.Match{
		ID: pgtype.UUID{Bytes: mid, Valid: true}, Stage: "main",
		Status: sqlc.MatchStatusCompleted, WinnerSide: pgtype.Int2{Int16: 1, Valid: true},
	}
	sides := []sqlc.MatchSide{
		{MatchID: m.ID, Side: 1, PlayerID: a},
		{MatchID: m.ID, Side: 2, PlayerID: a},
	}
	got := DerivePodiumPlaces(sqlc.TournamentFormatOlympic, []sqlc.Match{m}, sides)
	seen := map[string]int{}
	for _, slot := range got {
		for _, id := range slot.PlayerIDs {
			seen[uuidKey(id)]++
		}
	}
	for key, n := range seen {
		if n != 1 {
			t.Fatalf("player %v appears %d times", key, n)
		}
	}
}

func TestPodiumMismatch(t *testing.T) {
	p := func(n byte) pgtype.UUID {
		var b [16]byte
		b[15] = n
		return pgtype.UUID{Bytes: b, Valid: true}
	}
	a, b := p(1), p(2)
	saved := []PodiumPlace{{Place: 1, PlayerIDs: []pgtype.UUID{a}}, {Place: 2, PlayerIDs: []pgtype.UUID{b}}}
	suggested := []PodiumPlace{{Place: 1, PlayerIDs: []pgtype.UUID{b}}, {Place: 2, PlayerIDs: []pgtype.UUID{a}}}
	if !PodiumMismatch(saved, suggested) {
		t.Fatal("expected mismatch")
	}
	if PodiumMismatch(nil, suggested) {
		t.Fatal("empty saved is not mismatch")
	}
	if PodiumMismatch(saved, saved) {
		t.Fatal("identical should match")
	}
}

func assertPodium(t *testing.T, got []PodiumPlace, want map[int][]pgtype.UUID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d: %+v", len(got), len(want), got)
	}
	for _, slot := range got {
		ids, ok := want[slot.Place]
		if !ok {
			t.Fatalf("unexpected place %d", slot.Place)
		}
		if len(ids) != len(slot.PlayerIDs) {
			t.Fatalf("place %d: got %d players want %d", slot.Place, len(slot.PlayerIDs), len(ids))
		}
		gotKeys := map[string]struct{}{}
		for _, id := range slot.PlayerIDs {
			gotKeys[uuidKey(id)] = struct{}{}
		}
		for _, id := range ids {
			if _, ok := gotKeys[uuidKey(id)]; !ok {
				t.Fatalf("place %d missing player", slot.Place)
			}
		}
	}
}
