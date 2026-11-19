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
	}
	for _, tc := range cases {
		got := TournamentPoints(tc.in, c)
		if got != tc.want {
			t.Fatalf("%s: got %d want %d", tc.name, got, tc.want)
		}
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
