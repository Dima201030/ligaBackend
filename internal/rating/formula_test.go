package rating

import (
	"math"
	"testing"
)

func TestSizeFactor(t *testing.T) {
	cases := []struct {
		n    int
		want float64
	}{
		{4, 0.6},
		{8, math.Sqrt(8.0 / 16.0)},
		{16, 1.0},
		{32, math.Sqrt(32.0 / 16.0)},
		{100, 1.6},
	}
	for _, tc := range cases {
		got := SizeFactor(tc.n)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("n=%d got %v want %v", tc.n, got, tc.want)
		}
	}
}

func TestTournamentPointsExamples(t *testing.T) {
	c := DefaultCoeffs
	cases := []struct {
		name string
		in   TournamentPointsInput
		want int
	}{
		{"winner futures 16", TournamentPointsInput{Wins: 4, Participants: 16, Category: "futures", Type: "singles"}, 300},
		{"winner beginners 8", TournamentPointsInput{Wins: 3, Participants: 8, Category: "beginners", Type: "singles"}, 89},
		{"finalist masters 16", TournamentPointsInput{Wins: 3, Participants: 16, Category: "masters", Type: "singles"}, 336},
		{"winner challenger doubles 16", TournamentPointsInput{Wins: 4, Participants: 16, Category: "challenger", Type: "doubles"}, 312},
		{"winner masters final 8", TournamentPointsInput{Wins: 3, Participants: 8, Category: "masters", Type: "final"}, 356},
		{"semifinal rookies 8", TournamentPointsInput{Wins: 1, Participants: 8, Category: "rookies", Type: "singles"}, 42},
		{"consolation futures 16 2wins", TournamentPointsInput{Wins: 2, Participants: 16, Category: "futures", Type: "singles", Consolation: true}, 68},
		{"walkover-only winner futures 16", TournamentPointsInput{Wins: 4, Participants: 16, Category: "futures", Type: "singles", WalkoverOnly: true}, 150},
		{"consolation+walkover", TournamentPointsInput{Wins: 2, Participants: 16, Category: "futures", Type: "singles", Consolation: true, WalkoverOnly: true}, 34},
	}
	for _, tc := range cases {
		got := TournamentPoints(tc.in, c)
		if got != tc.want {
			t.Fatalf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}

func TestAwardFromWinStats(t *testing.T) {
	cases := []struct {
		name  string
		stats WinStats
		want  AwardResult
	}{
		{"primary preferred", WinStats{PrimaryWins: 2, ConsolationWins: 3}, AwardResult{Wins: 2}},
		{"primary all walkover", WinStats{PrimaryWins: 2, PrimaryWalkoverWins: 2}, AwardResult{Wins: 2, WalkoverOnly: true}},
		{"primary mixed walkover", WinStats{PrimaryWins: 2, PrimaryWalkoverWins: 1}, AwardResult{Wins: 2}},
		{"consolation only", WinStats{ConsolationWins: 3}, AwardResult{Wins: 3, Consolation: true}},
		{"consolation walkover only", WinStats{ConsolationWins: 2, ConsolationWalkoverWins: 2}, AwardResult{Wins: 2, Consolation: true, WalkoverOnly: true}},
		{"no wins", WinStats{}, AwardResult{}},
		{"dq zeros", WinStats{PrimaryWins: 3, Disqualified: true}, AwardResult{Disqualified: true}},
	}
	for _, tc := range cases {
		got := AwardFromWinStats(tc.stats)
		if got != tc.want {
			t.Fatalf("%s: got %+v want %+v", tc.name, got, tc.want)
		}
	}
}

func TestTournamentPointsDisqualifiedZero(t *testing.T) {
	got := TournamentPoints(TournamentPointsInput{
		Wins: 4, Participants: 16, Category: "futures", Type: "singles", Disqualified: true,
	}, DefaultCoeffs)
	if got != 0 {
		t.Fatalf("got %d", got)
	}
}

func TestEffectiveKRetireHalves(t *testing.T) {
	full := KFactor(12, false, DefaultCoeffs)
	if full != 24 {
		t.Fatalf("base k=%v", full)
	}
	got := EffectiveK(12, false, true, DefaultCoeffs)
	if got != 12 {
		t.Fatalf("retire k=%v want 12", got)
	}
	if EffectiveK(12, false, false, DefaultCoeffs) != full {
		t.Fatal("non-retire should keep full K")
	}
}

func TestEloUpset(t *testing.T) {
	// A=1400 beats B=1700, both 12 matches → K=24
	k := KFactor(12, false, DefaultCoeffs)
	if k != 24 {
		t.Fatalf("k=%v", k)
	}
	dA := EloDelta(1400, 1700, k, 1)
	dB := EloDelta(1700, 1400, k, 0)
	if math.Abs(dA-20.4) > 0.05 {
		t.Fatalf("dA=%v want ~20.4", dA)
	}
	if math.Abs(dB+20.4) > 0.05 {
		t.Fatalf("dB=%v want ~-20.4", dB)
	}
}

func TestEloEqualish(t *testing.T) {
	k := KFactor(12, false, DefaultCoeffs)
	dA := EloDelta(1600, 1700, k, 1)
	if math.Abs(dA-15.36) > 0.1 {
		t.Fatalf("dA=%v", dA)
	}
}

func TestDisciplineFromTournamentType(t *testing.T) {
	cases := map[string]string{
		"singles": DisciplineSingles,
		"cup":     DisciplineSingles,
		"final":   DisciplineSingles,
		"doubles": DisciplineDoubles,
		"mixed":   DisciplineDoubles,
		"teams":   DisciplineDoubles,
		"":        DisciplineSingles,
	}
	for in, want := range cases {
		if got := DisciplineFromTournamentType(in); got != want {
			t.Fatalf("%q: got %s want %s", in, got, want)
		}
	}
}

func TestSplitBestWindowSums(t *testing.T) {
	// 12 singles results → keep best 10; 7 doubles → keep best 5
	singles := []int{10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 5, 1}
	doubles := []int{11, 22, 33, 44, 55, 66, 1}
	br := SplitBestWindowSums(singles, doubles, DefaultCoeffs)
	if br.Singles != 10+20+30+40+50+60+70+80+90+100 {
		t.Fatalf("singles=%d", br.Singles)
	}
	if br.Doubles != 66+55+44+33+22 {
		t.Fatalf("doubles=%d", br.Doubles)
	}
	if br.Total() != br.Singles+br.Doubles {
		t.Fatalf("total mismatch")
	}
}

func TestSelectBestSums(t *testing.T) {
	vals := []int{10, 50, 30, 40, 5}
	if got := SelectBestSums(vals, 3); got != 120 {
		t.Fatalf("got %d", got)
	}
	if got := SelectBestSums(vals, 10); got != 135 {
		t.Fatalf("got %d", got)
	}
}

func TestSideAverage(t *testing.T) {
	if got := SideAverage([]float64{1400, 1600}); got != 1500 {
		t.Fatalf("got %v", got)
	}
}

func TestApplyEloFloor(t *testing.T) {
	if got := ApplyEloFloor(790, 800); got != 800 {
		t.Fatalf("got %v", got)
	}
}
