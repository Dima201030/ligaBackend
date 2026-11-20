package rating

import (
	"sort"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// PlaceToEquivalentWins maps a finishing place to BasePoints wins for DE.
// RATING.md §6: champion=4, finalist=3, semifinalist=2, and so on.
func PlaceToEquivalentWins(place int) int {
	switch {
	case place <= 0:
		return 0
	case place == 1:
		return 4
	case place == 2:
		return 3
	case place <= 4:
		return 2
	case place <= 8:
		return 1
	default:
		return 0
	}
}

type finishedMatch struct {
	match   sqlc.Match
	side1   []pgtype.UUID // players on side 1 (player + optional partner)
	side2   []pgtype.UUID
	winner  int16
}

// ComputeDEPlaces assigns finishing places for a double-elimination draw.
// Grand final winner=1, loser=2; then lower-bracket losers from last round to first.
// Players with no elimination recorded get the next places (stable by UUID bytes).
func ComputeDEPlaces(matches []sqlc.Match, sides []sqlc.MatchSide, playerIDs []pgtype.UUID) map[string]int {
	place := map[string]int{}
	assign := func(ids []pgtype.UUID, p int) {
		for _, id := range ids {
			if !id.Valid {
				continue
			}
			key := uuidKey(id)
			if _, ok := place[key]; !ok {
				place[key] = p
			}
		}
	}

	byMatch := map[string][]sqlc.MatchSide{}
	for _, s := range sides {
		byMatch[uuidKey(s.MatchID)] = append(byMatch[uuidKey(s.MatchID)], s)
	}

	var lower []finishedMatch
	var gf *finishedMatch
	for _, m := range matches {
		if !isFinishedResult(m) || m.IsBye || !m.WinnerSide.Valid {
			continue
		}
		s1, s2 := sidesForMatch(byMatch[uuidKey(m.ID)])
		fm := finishedMatch{match: m, side1: s1, side2: s2, winner: m.WinnerSide.Int16}
		switch m.Stage {
		case "grand_final":
			cp := fm
			gf = &cp
		case "lower":
			lower = append(lower, fm)
		}
	}

	next := 1
	if gf != nil {
		assign(winnersOf(*gf), 1)
		assign(losersOf(*gf), 2)
		next = 3
	}

	sort.SliceStable(lower, func(i, j int) bool {
		if lower[i].match.RoundIndex != lower[j].match.RoundIndex {
			return lower[i].match.RoundIndex > lower[j].match.RoundIndex
		}
		return lower[i].match.Position < lower[j].match.Position
	})
	i := 0
	for i < len(lower) {
		round := lower[i].match.RoundIndex
		var losers []pgtype.UUID
		for i < len(lower) && lower[i].match.RoundIndex == round {
			losers = append(losers, losersOf(lower[i])...)
			i++
		}
		// Stable unique losers without a place yet.
		keys := uniqueValidKeys(losers)
		sort.Strings(keys)
		for _, key := range keys {
			if _, ok := place[key]; ok {
				continue
			}
			place[key] = next
			next++
		}
	}

	// Anyone not placed (e.g. incomplete draw) gets trailing places.
	rest := make([]string, 0, len(playerIDs))
	for _, id := range playerIDs {
		if !id.Valid {
			continue
		}
		key := uuidKey(id)
		if _, ok := place[key]; !ok {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	for _, key := range rest {
		place[key] = next
		next++
	}

	return place
}

func isFinishedResult(m sqlc.Match) bool {
	switch m.Status {
	case sqlc.MatchStatusCompleted, sqlc.MatchStatusWalkover, sqlc.MatchStatusDisqualified:
		return true
	default:
		return false
	}
}

func sidesForMatch(sides []sqlc.MatchSide) (side1, side2 []pgtype.UUID) {
	for _, s := range sides {
		ids := []pgtype.UUID{}
		if s.PlayerID.Valid {
			ids = append(ids, s.PlayerID)
		}
		if s.PartnerID.Valid {
			ids = append(ids, s.PartnerID)
		}
		if s.Side == 1 {
			side1 = append(side1, ids...)
		} else if s.Side == 2 {
			side2 = append(side2, ids...)
		}
	}
	return side1, side2
}

func winnersOf(fm finishedMatch) []pgtype.UUID {
	if fm.winner == 1 {
		return fm.side1
	}
	return fm.side2
}

func losersOf(fm finishedMatch) []pgtype.UUID {
	if fm.winner == 1 {
		return fm.side2
	}
	return fm.side1
}

func uniqueValidKeys(ids []pgtype.UUID) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !id.Valid {
			continue
		}
		key := uuidKey(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func uuidKey(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return string(id.Bytes[:])
}
