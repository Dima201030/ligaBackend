package rating

import (
	"testing"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestDerivePodiumRoundRobinEmpty(t *testing.T) {
	got := DerivePodiumPlaces(sqlc.TournamentFormatRoundRobin, nil, nil)
	if len(got) != 0 {
		t.Fatalf("round robin podium must be empty, got %#v", got)
	}
}

func TestDerivePodiumGroupsPlayoffUsesChampionshipFinal(t *testing.T) {
	p1 := mustScanUUID(t, "11111111-1111-1111-1111-111111111111")
	p2 := mustScanUUID(t, "22222222-2222-2222-2222-222222222222")
	p3 := mustScanUUID(t, "33333333-3333-3333-3333-333333333333")
	p4 := mustScanUUID(t, "44444444-4444-4444-4444-444444444444")
	sf1 := mustScanUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1")
	sf2 := mustScanUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2")
	finalID := mustScanUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa3")
	thirdID := mustScanUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa4")

	matches := []sqlc.Match{
		{
			ID: sf1, Stage: "playoff", RoundIndex: 1, Status: sqlc.MatchStatusCompleted,
			WinnerSide: pgtype.Int2{Int16: 1, Valid: true},
			NextMatchID: finalID, NextSlot: pgtype.Int2{Int16: 1, Valid: true},
		},
		{
			ID: sf2, Stage: "playoff", RoundIndex: 1, Status: sqlc.MatchStatusCompleted,
			WinnerSide: pgtype.Int2{Int16: 1, Valid: true},
			NextMatchID: finalID, NextSlot: pgtype.Int2{Int16: 2, Valid: true},
		},
		{
			ID: finalID, Stage: "playoff", RoundIndex: 2, Status: sqlc.MatchStatusCompleted,
			WinnerSide: pgtype.Int2{Int16: 1, Valid: true},
		},
		{
			ID: thirdID, Stage: "third_place", RoundIndex: 2, Status: sqlc.MatchStatusCompleted,
			WinnerSide: pgtype.Int2{Int16: 2, Valid: true},
		},
	}
	sides := []sqlc.MatchSide{
		{MatchID: sf1, Side: 1, PlayerID: p1},
		{MatchID: sf1, Side: 2, PlayerID: p3},
		{MatchID: sf2, Side: 1, PlayerID: p2},
		{MatchID: sf2, Side: 2, PlayerID: p4},
		{MatchID: finalID, Side: 1, PlayerID: p1},
		{MatchID: finalID, Side: 2, PlayerID: p2},
		{MatchID: thirdID, Side: 1, PlayerID: p3},
		{MatchID: thirdID, Side: 2, PlayerID: p4},
	}

	got := DerivePodiumPlaces(sqlc.TournamentFormatGroupsPlayoff, matches, sides)
	if len(got) != 3 {
		t.Fatalf("places=%d %#v", len(got), got)
	}
	if uuidKey(got[0].PlayerIDs[0]) != uuidKey(p1) {
		t.Fatalf("1st=%s", uuidKey(got[0].PlayerIDs[0]))
	}
	if uuidKey(got[1].PlayerIDs[0]) != uuidKey(p2) {
		t.Fatalf("2nd=%s", uuidKey(got[1].PlayerIDs[0]))
	}
	if uuidKey(got[2].PlayerIDs[0]) != uuidKey(p4) {
		t.Fatalf("3rd=%s", uuidKey(got[2].PlayerIDs[0]))
	}
}

func mustScanUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		t.Fatal(err)
	}
	return id
}
