package matches

import (
	"testing"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestIsChampionshipFinal(t *testing.T) {
	next := pgtype.UUID{Valid: true}
	cases := []struct {
		name string
		m    sqlc.Match
		want bool
	}{
		{"olympic final", sqlc.Match{Stage: "main"}, true},
		{"olympic SF", sqlc.Match{Stage: "main", NextMatchID: next}, false},
		{"grand final", sqlc.Match{Stage: "grand_final"}, true},
		{"grand final with next (weird)", sqlc.Match{Stage: "grand_final", NextMatchID: next}, true},
		{"third place", sqlc.Match{Stage: "third_place"}, false},
		{"lower", sqlc.Match{Stage: "lower"}, false},
		{"group", sqlc.Match{Stage: "group"}, false},
		{"playoff final", sqlc.Match{Stage: "playoff"}, true},
		{"playoff SF", sqlc.Match{Stage: "playoff", NextMatchID: next}, false},
		// RR matches look like finals by stage/next; format gate is separate.
		{"rr-looking main", sqlc.Match{Stage: "main"}, true},
	}
	for _, tc := range cases {
		if got := isChampionshipFinal(tc.m); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}
