package matches_test

import (
	"testing"

	"github.com/Dima201030/ligaBackend/internal/matches"
)

func TestValidateScoreTwoNil(t *testing.T) {
	winner, text, err := matches.ValidateAndDecideWinner([]matches.SetScore{
		{Side1: 6, Side2: 4},
		{Side1: 6, Side2: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if winner != 1 || text != "6:4, 6:3" {
		t.Fatalf("winner=%d text=%q", winner, text)
	}
}

func TestValidateScoreThreeSets(t *testing.T) {
	winner, _, err := matches.ValidateAndDecideWinner([]matches.SetScore{
		{Side1: 6, Side2: 4},
		{Side1: 3, Side2: 6},
		{Side1: 10, Side2: 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	if winner != 1 {
		t.Fatalf("winner=%d", winner)
	}
}

func TestValidateScoreBestOfOne(t *testing.T) {
	winner, text, err := matches.ValidateAndDecideWinner([]matches.SetScore{
		{Side1: 6, Side2: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if winner != 1 || text != "6:4" {
		t.Fatalf("winner=%d text=%q", winner, text)
	}
}

func TestValidateScoreRejectsOneSetEach(t *testing.T) {
	_, _, err := matches.ValidateAndDecideWinner([]matches.SetScore{
		{Side1: 6, Side2: 4},
		{Side1: 3, Side2: 6},
	})
	if err == nil {
		t.Fatal("expected error for unfinished best-of-3")
	}
}

func TestValidateScoreRejectsTieSet(t *testing.T) {
	_, _, err := matches.ValidateAndDecideWinner([]matches.SetScore{
		{Side1: 6, Side2: 6},
		{Side1: 6, Side2: 4},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateScoreTiebreakSet(t *testing.T) {
	winner, text, err := matches.ValidateAndDecideWinner([]matches.SetScore{
		{Side1: 7, Side2: 6},
		{Side1: 6, Side2: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if winner != 1 || text != "7:6, 6:4" {
		t.Fatalf("winner=%d text=%q", winner, text)
	}
}

func TestValidateScoreSide2WinsBestOfThree(t *testing.T) {
	winner, text, err := matches.ValidateAndDecideWinner([]matches.SetScore{
		{Side1: 4, Side2: 6},
		{Side1: 7, Side2: 5},
		{Side1: 3, Side2: 6},
	})
	if err != nil {
		t.Fatal(err)
	}
	if winner != 2 {
		t.Fatalf("winner=%d text=%q", winner, text)
	}
}
