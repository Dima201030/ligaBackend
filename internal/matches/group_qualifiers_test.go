package matches

import (
	"testing"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestGroupLetterFromKey(t *testing.T) {
	if got := groupLetterFromKey("A-GAR1M1"); got != "A" {
		t.Fatalf("got %q", got)
	}
	if got := groupLetterFromKey("B-GBR1M1"); got != "B" {
		t.Fatalf("got %q", got)
	}
	if got := groupLetterFromKey("R1M1"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestRankGroupStandingsTwoPlayers(t *testing.T) {
	p1 := mustUUID(t, "11111111-1111-1111-1111-111111111111")
	p2 := mustUUID(t, "22222222-2222-2222-2222-222222222222")
	mID := mustUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")

	match := sqlc.Match{
		ID:         mID,
		Stage:      "group",
		BracketKey: "A-GAR1M1",
		Status:     sqlc.MatchStatusCompleted,
		WinnerSide: pgtype.Int2{Int16: 2, Valid: true},
		ScoreSets:  []byte(`[[3,6]]`),
	}
	sides := map[string][]sqlc.MatchSide{
		uuidString(mID): {
			{ID: mustUUID(t, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb1"), MatchID: mID, Side: 1, PlayerID: p1, Label: pgtype.Text{String: "Player One", Valid: true}},
			{ID: mustUUID(t, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb2"), MatchID: mID, Side: 2, PlayerID: p2, Label: pgtype.Text{String: "Player Two", Valid: true}},
		},
	}

	ranked := rankGroupStandings([]sqlc.Match{match}, sides)
	if len(ranked) != 2 {
		t.Fatalf("len=%d", len(ranked))
	}
	if uuidString(ranked[0].playerID) != uuidString(p2) {
		t.Fatalf("expected p2 first, got %s (%s)", ranked[0].label, uuidString(ranked[0].playerID))
	}
	if ranked[0].wins != 1 || ranked[1].wins != 0 {
		t.Fatalf("wins=%d/%d", ranked[0].wins, ranked[1].wins)
	}
}

func TestQualifierLabelRegex(t *testing.T) {
	parts := qualifierLabelRe.FindStringSubmatch("A1")
	if len(parts) != 3 || parts[1] != "A" || parts[2] != "1" {
		t.Fatalf("%v", parts)
	}
	if qualifierLabelRe.MatchString("Победитель R1M1") {
		t.Fatal("should not match russian placeholder")
	}
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		t.Fatal(err)
	}
	return id
}
