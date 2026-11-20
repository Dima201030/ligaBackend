package bracket_test

import (
	"testing"

	"github.com/Dima201030/ligaBackend/internal/bracket"
)

func entries(names ...string) []bracket.Entry {
	out := make([]bracket.Entry, len(names))
	for i, name := range names {
		seed := i + 1
		out[i] = bracket.Entry{
			PlayerID: name,
			Name:     name,
			Seed:     &seed,
		}
	}
	return out
}

func TestSeedOrder8(t *testing.T) {
	got := bracket.SeedOrder(8)
	want := []int{1, 8, 4, 5, 2, 7, 3, 6}
	if len(got) != len(want) {
		t.Fatalf("len=%d", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("seed order: got %v want %v", got, want)
		}
	}
}

func TestOlympicWithByes(t *testing.T) {
	// 5 players → bracket size 8 → 3 byes
	draw := bracket.BuildSingleElimination(entries("A", "B", "C", "D", "E"), bracket.Options{ThirdPlace: true})
	if draw.BracketSize != 8 {
		t.Fatalf("bracket size=%d", draw.BracketSize)
	}
	if draw.Byes != 3 {
		t.Fatalf("byes=%d", draw.Byes)
	}
	if draw.ThirdPlace == nil {
		t.Fatal("expected third place")
	}

	r1 := draw.Rounds[0]
	byeCount := 0
	for _, m := range r1.Matches {
		if m.AutomaticWinner != 0 {
			byeCount++
			if m.NextMatchID == "" {
				t.Fatalf("bye match %s missing next", m.ID)
			}
		}
	}
	if byeCount != 3 {
		t.Fatalf("first-round bye matches=%d", byeCount)
	}

	// Bye winners must already sit in round 2 slots.
	r2 := draw.Rounds[1]
	filled := 0
	for _, m := range r2.Matches {
		if m.Slot1.PlayerID != "" || m.Slot1.Name != "" {
			filled++
		}
		if m.Slot2.PlayerID != "" || m.Slot2.Name != "" {
			filled++
		}
	}
	if filled != 3 {
		t.Fatalf("expected 3 advanced bye slots in R2, got %d", filled)
	}

	// Semis wired to third place.
	semis := draw.Rounds[len(draw.Rounds)-2].Matches
	if len(semis) != 2 {
		t.Fatalf("semis=%d", len(semis))
	}
	if semis[0].LoserNextMatchID != "R3P" || semis[1].LoserNextMatchID != "R3P" {
		t.Fatalf("third-place loser links missing: %+v %+v", semis[0], semis[1])
	}
}

func TestOlympicPowerOfTwoNoByes(t *testing.T) {
	draw := bracket.BuildSingleElimination(entries("A", "B", "C", "D"), bracket.Options{})
	if draw.Byes != 0 || draw.BracketSize != 4 {
		t.Fatalf("size=%d byes=%d", draw.BracketSize, draw.Byes)
	}
	for _, m := range draw.Rounds[0].Matches {
		if m.AutomaticWinner != 0 {
			t.Fatalf("unexpected bye in %s", m.ID)
		}
	}
}

func TestGenerateDispatcher(t *testing.T) {
	e := entries("A", "B", "C", "D")
	rr := bracket.Generate(bracket.FormatRoundRobin, e, bracket.Options{})
	if rr.MatchCount == 0 {
		t.Fatal("round robin empty")
	}
	gp := bracket.Generate(bracket.FormatGroupsPlayoff, e, bracket.Options{GroupSize: 4, QualifiersPerGroup: 2})
	if len(gp.Groups) == 0 || len(gp.PlayoffRounds) == 0 {
		t.Fatal("groups playoff incomplete")
	}
	de := bracket.Generate(bracket.FormatDoubleElimination, e, bracket.Options{})
	if de.GrandFinal == nil || len(de.LowerRounds) == 0 {
		t.Fatal("double elim incomplete")
	}
}
