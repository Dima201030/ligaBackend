package bracket_test

import (
	"testing"

	"github.com/Dima201030/ligaBackend/internal/bracket"
)

func entriesN(n int) []bracket.Entry {
	out := make([]bracket.Entry, 0, n)
	for i := 0; i < n; i++ {
		name := string(rune('A' + i))
		seed := i + 1
		out = append(out, bracket.Entry{
			PlayerID: name,
			Name:     name,
			Seed:     &seed,
			Rating:   1200 - i,
		})
	}
	return out
}

func TestRoundRobinMatchCount(t *testing.T) {
	// Odd N pads a bye slot in the circle method; count only player-vs-player.
	cases := []struct {
		n, wantPairs int
	}{
		{3, 3},
		{4, 6},
		{5, 10},
		{8, 28},
	}
	for _, tc := range cases {
		draw := bracket.Generate(bracket.FormatRoundRobin, entriesN(tc.n), bracket.Options{})
		pairs := 0
		for _, round := range draw.Rounds {
			for _, m := range round.Matches {
				if m.AutomaticWinner != 0 || m.Slot1.IsBye || m.Slot2.IsBye {
					continue
				}
				if m.Slot1.PlayerID == "" || m.Slot2.PlayerID == "" {
					t.Fatalf("empty slot in RR: %+v", m)
				}
				pairs++
			}
		}
		if pairs != tc.wantPairs {
			t.Fatalf("n=%d pairs=%d want %d (raw MatchCount=%d)", tc.n, pairs, tc.wantPairs, draw.MatchCount)
		}
	}
}

func TestGroupsPlayoffStructure(t *testing.T) {
	draw := bracket.Generate(bracket.FormatGroupsPlayoff, entriesN(8), bracket.Options{
		GroupSize:          4,
		QualifiersPerGroup: 2,
	})
	if len(draw.Groups) != 2 {
		t.Fatalf("groups=%d want 2", len(draw.Groups))
	}
	for _, g := range draw.Groups {
		// C(4,2)=6 RR matches per group
		if len(g.Matches) != 6 {
			t.Fatalf("group %s matches=%d want 6", g.Name, len(g.Matches))
		}
	}
	if len(draw.PlayoffRounds) == 0 {
		t.Fatal("missing playoff rounds")
	}
	// 4 qualifiers → SF + F (+ optional 3rd)
	playoffMatches := 0
	for _, r := range draw.PlayoffRounds {
		playoffMatches += len(r.Matches)
	}
	if playoffMatches < 2 {
		t.Fatalf("playoff matches=%d", playoffMatches)
	}
}

func TestOlympicSeedsAndByesForSix(t *testing.T) {
	draw := bracket.Generate(bracket.FormatOlympic, entriesN(6), bracket.Options{ThirdPlace: true})
	if draw.BracketSize != 8 {
		t.Fatalf("bracket size=%d", draw.BracketSize)
	}
	if draw.Byes != 2 {
		t.Fatalf("byes=%d", draw.Byes)
	}
	byeAdvances := 0
	for _, m := range draw.Rounds[0].Matches {
		if m.AutomaticWinner != 0 {
			byeAdvances++
		}
	}
	if byeAdvances != 2 {
		t.Fatalf("auto winners=%d", byeAdvances)
	}
	if draw.ThirdPlace == nil {
		t.Fatal("expected third place match")
	}
}

func TestDoubleEliminationHasLowerAndGrandFinal(t *testing.T) {
	draw := bracket.Generate(bracket.FormatDoubleElimination, entriesN(8), bracket.Options{})
	if draw.GrandFinal == nil {
		t.Fatal("missing grand final")
	}
	if len(draw.LowerRounds) == 0 {
		t.Fatal("missing lower bracket")
	}
	if len(draw.Rounds) == 0 {
		t.Fatal("missing upper bracket")
	}
}
