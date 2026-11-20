package rating

import (
	"testing"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPlaceToEquivalentWins(t *testing.T) {
	cases := map[int]int{0: 0, 1: 4, 2: 3, 3: 2, 4: 2, 5: 1, 8: 1, 9: 0, 16: 0}
	for place, want := range cases {
		if got := PlaceToEquivalentWins(place); got != want {
			t.Fatalf("place %d: got %d want %d", place, got, want)
		}
	}
}

func TestComputeDEPlacesGrandFinalAndLower(t *testing.T) {
	p := func(n byte) pgtype.UUID {
		var b [16]byte
		b[15] = n
		return pgtype.UUID{Bytes: b, Valid: true}
	}
	a, b, c, d := p(1), p(2), p(3), p(4)
	mk := func(id byte, stage string, round int32, pos int32, winner int16) sqlc.Match {
		var mid [16]byte
		mid[0] = id
		return sqlc.Match{
			ID: pgtype.UUID{Bytes: mid, Valid: true}, Stage: stage,
			RoundIndex: round, Position: pos,
			Status: sqlc.MatchStatusCompleted,
			WinnerSide: pgtype.Int2{Int16: winner, Valid: true},
		}
	}
	side := func(matchID byte, side int16, player pgtype.UUID) sqlc.MatchSide {
		var mid [16]byte
		mid[0] = matchID
		return sqlc.MatchSide{
			MatchID: pgtype.UUID{Bytes: mid, Valid: true},
			Side:    side, PlayerID: player,
		}
	}

	// LB last round: C beats D → D eliminated place 3
	// GF: A beats C → A=1, C=2
	matches := []sqlc.Match{
		mk(10, "lower", 1, 0, 1),
		mk(20, "grand_final", 0, 0, 1),
	}
	sides := []sqlc.MatchSide{
		side(10, 1, c), side(10, 2, d),
		side(20, 1, a), side(20, 2, c),
	}
	places := ComputeDEPlaces(matches, sides, []pgtype.UUID{a, b, c, d})
	if places[uuidKey(a)] != 1 {
		t.Fatalf("champ place=%d", places[uuidKey(a)])
	}
	if places[uuidKey(c)] != 2 {
		t.Fatalf("finalist place=%d", places[uuidKey(c)])
	}
	if places[uuidKey(d)] != 3 {
		t.Fatalf("LB loser place=%d", places[uuidKey(d)])
	}
	if places[uuidKey(b)] != 4 {
		t.Fatalf("unplaced trailing place=%d", places[uuidKey(b)])
	}
	if PlaceToEquivalentWins(places[uuidKey(a)]) != 4 {
		t.Fatal("champ wins bucket")
	}
	if PlaceToEquivalentWins(places[uuidKey(c)]) != 3 {
		t.Fatal("finalist wins bucket")
	}
	if PlaceToEquivalentWins(places[uuidKey(d)]) != 2 {
		t.Fatal("3rd wins bucket")
	}
}
