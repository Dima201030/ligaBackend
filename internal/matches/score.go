package matches

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidScore = errors.New("invalid score")
	ErrEmptyScore   = errors.New("empty score")
)

// SetScore is games won by side1 and side2 in one set.
type SetScore struct {
	Side1 int `json:"side1"`
	Side2 int `json:"side2"`
}

// ValidateAndDecideWinner checks best-of-3 set scores and returns winner side (1|2).
// Accepts regular sets (6-x with margin 2, or 7-5/7-6) and a deciding match TB (to 10).
func ValidateAndDecideWinner(sets []SetScore) (winner int, text string, err error) {
	if len(sets) == 0 {
		return 0, "", ErrEmptyScore
	}
	if len(sets) > 3 {
		return 0, "", fmt.Errorf("%w: максимум 3 сета", ErrInvalidScore)
	}

	wins1, wins2 := 0, 0
	parts := make([]string, 0, len(sets))

	for i, set := range sets {
		if set.Side1 < 0 || set.Side2 < 0 {
			return 0, "", fmt.Errorf("%w: отрицательный счёт в сете %d", ErrInvalidScore, i+1)
		}
		if set.Side1 == set.Side2 {
			return 0, "", fmt.Errorf("%w: ничья в сете %d недопустима", ErrInvalidScore, i+1)
		}
		allowMatchTB := i == 2 || (len(sets) == 1 && (set.Side1 >= 10 || set.Side2 >= 10))
		if !validSet(set, allowMatchTB) {
			return 0, "", fmt.Errorf("%w: некорректный сет %d (%d:%d)", ErrInvalidScore, i+1, set.Side1, set.Side2)
		}
		if set.Side1 > set.Side2 {
			wins1++
		} else {
			wins2++
		}
		parts = append(parts, fmt.Sprintf("%d:%d", set.Side1, set.Side2))

		// Match already decided — no extra sets.
		if (wins1 >= 2 || wins2 >= 2) && i < len(sets)-1 {
			return 0, "", fmt.Errorf("%w: лишние сеты после победы", ErrInvalidScore)
		}
	}

	if wins1 == wins2 {
		return 0, "", fmt.Errorf("%w: матч не завершён — равное число сетов", ErrInvalidScore)
	}
	if wins1 < 2 && wins2 < 2 && len(sets) < 3 {
		// Allow 2-0, or incomplete only if someone already has 2 — covered above.
		// Best of 3 can end 2-0 or 2-1.
		if wins1+wins2 < 2 {
			return 0, "", fmt.Errorf("%w: недостаточно сетов", ErrInvalidScore)
		}
	}
	if wins1 < 2 && wins2 < 2 {
		return 0, "", fmt.Errorf("%w: для победы нужно выиграть 2 сета", ErrInvalidScore)
	}

	winner = 1
	if wins2 > wins1 {
		winner = 2
	}
	return winner, strings.Join(parts, ", "), nil
}

func validSet(set SetScore, allowMatchTB bool) bool {
	a, b := set.Side1, set.Side2
	if a < b {
		a, b = b, a
	}
	// Match tiebreak (deciding): first to 10, win by 2.
	if allowMatchTB && a >= 10 {
		return a-b >= 2
	}
	// Standard set: 6 games with margin 2, or 7-5 / 7-6.
	if a == 6 && b <= 4 {
		return true
	}
	if a == 7 && (b == 5 || b == 6) {
		return true
	}
	return false
}

// ParseSetsJSON unmarshals [[6,4],[6,3]] or [{"side1":6,"side2":4}].
func ParseSetsJSON(raw json.RawMessage) ([]SetScore, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, ErrEmptyScore
	}
	var pairs [][]int
	if err := json.Unmarshal(raw, &pairs); err == nil && len(pairs) > 0 {
		out := make([]SetScore, 0, len(pairs))
		for i, p := range pairs {
			if len(p) != 2 {
				return nil, fmt.Errorf("%w: сет %d должен содержать два числа", ErrInvalidScore, i+1)
			}
			out = append(out, SetScore{Side1: p[0], Side2: p[1]})
		}
		return out, nil
	}
	var objs []SetScore
	if err := json.Unmarshal(raw, &objs); err != nil {
		return nil, fmt.Errorf("%w: ожидается массив сетов", ErrInvalidScore)
	}
	return objs, nil
}

// SetsToJSON stores sets as [[6,4],...].
func SetsToJSON(sets []SetScore) (json.RawMessage, error) {
	pairs := make([][]int, len(sets))
	for i, s := range sets {
		pairs[i] = []int{s.Side1, s.Side2}
	}
	return json.Marshal(pairs)
}
