package rating

import (
	"sort"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// PodiumPlace is a finishing slot for the public/organizer podium (places 1..3).
type PodiumPlace struct {
	Place     int
	PlayerIDs []pgtype.UUID
}

// DerivePodiumPlaces computes places 1..3 from finished bracket matches.
//
//   - olympic / groups_playoff: championship final → 1/2; third_place winner → 3;
//     without a third-place match, singles SF losers share place 3 (≤2 players).
//   - double_elimination: first three places from ComputeDEPlaces.
//   - round_robin: empty (standings podium not derived here).
//
// Never assigns the same player to two places. Incomplete draws yield partial results.
func DerivePodiumPlaces(
	format sqlc.TournamentFormat,
	matches []sqlc.Match,
	sides []sqlc.MatchSide,
) []PodiumPlace {
	switch format {
	case sqlc.TournamentFormatDoubleElimination:
		return podiumFromDE(matches, sides)
	case sqlc.TournamentFormatOlympic, sqlc.TournamentFormatGroupsPlayoff:
		return podiumFromKnockout(matches, sides)
	default:
		return nil
	}
}

func podiumFromDE(matches []sqlc.Match, sides []sqlc.MatchSide) []PodiumPlace {
	players := uniqueSidePlayers(sides)
	raw := ComputeDEPlaces(matches, sides, players)
	byPlace := map[int][]pgtype.UUID{}
	for _, id := range players {
		p := raw[uuidKey(id)]
		if p < 1 || p > 3 {
			continue
		}
		byPlace[p] = append(byPlace[p], id)
	}
	return podiumSlots(byPlace)
}

func podiumFromKnockout(matches []sqlc.Match, sides []sqlc.MatchSide) []PodiumPlace {
	byMatch := map[string][]sqlc.MatchSide{}
	for _, s := range sides {
		byMatch[uuidKey(s.MatchID)] = append(byMatch[uuidKey(s.MatchID)], s)
	}

	final := findChampionshipFinal(matches)
	byPlace := map[int][]pgtype.UUID{}
	used := map[string]struct{}{}
	assign := func(place int, ids []pgtype.UUID) {
		for _, id := range ids {
			if !id.Valid {
				continue
			}
			key := uuidKey(id)
			if _, ok := used[key]; ok {
				continue
			}
			if len(byPlace[place]) >= 2 {
				return
			}
			used[key] = struct{}{}
			byPlace[place] = append(byPlace[place], id)
		}
	}

	if final != nil && isFinishedResult(*final) && final.WinnerSide.Valid {
		s1, s2 := sidesForMatch(byMatch[uuidKey(final.ID)])
		fm := finishedMatch{match: *final, side1: s1, side2: s2, winner: final.WinnerSide.Int16}
		assign(1, winnersOf(fm))
		assign(2, losersOf(fm))
	}

	if tp := findThirdPlaceMatch(matches); tp != nil && isFinishedResult(*tp) && tp.WinnerSide.Valid {
		s1, s2 := sidesForMatch(byMatch[uuidKey(tp.ID)])
		fm := finishedMatch{match: *tp, side1: s1, side2: s2, winner: tp.WinnerSide.Int16}
		assign(3, winnersOf(fm))
	} else if final != nil && byPlace[3] == nil {
		// No bronze match: shared 3rd for singles SF losers (one player per side).
		var losers []pgtype.UUID
		singlesOK := true
		for _, m := range matches {
			if !m.NextMatchID.Valid || uuidKey(m.NextMatchID) != uuidKey(final.ID) {
				continue
			}
			if m.Stage == "third_place" || m.Stage == "group" || m.Stage == "lower" {
				continue
			}
			if !isFinishedResult(m) || !m.WinnerSide.Valid {
				singlesOK = false
				break
			}
			s1, s2 := sidesForMatch(byMatch[uuidKey(m.ID)])
			fm := finishedMatch{match: m, side1: s1, side2: s2, winner: m.WinnerSide.Int16}
			sideLosers := losersOf(fm)
			if len(sideLosers) != 1 {
				singlesOK = false
				break
			}
			losers = append(losers, sideLosers...)
		}
		if singlesOK && len(losers) > 0 && len(losers) <= 2 {
			assign(3, losers)
		}
	}

	return podiumSlots(byPlace)
}

func podiumSlots(byPlace map[int][]pgtype.UUID) []PodiumPlace {
	out := make([]PodiumPlace, 0, 3)
	for place := 1; place <= 3; place++ {
		ids := byPlace[place]
		if len(ids) == 0 {
			continue
		}
		// Stable order by UUID bytes.
		sort.SliceStable(ids, func(i, j int) bool {
			return uuidKey(ids[i]) < uuidKey(ids[j])
		})
		if len(ids) > 2 {
			ids = ids[:2]
		}
		out = append(out, PodiumPlace{Place: place, PlayerIDs: ids})
	}
	return out
}

func findChampionshipFinal(matches []sqlc.Match) *sqlc.Match {
	var best *sqlc.Match
	for i := range matches {
		m := &matches[i]
		switch m.Stage {
		case "grand_final":
			return m
		case "third_place", "lower", "group":
			continue
		case "main", "playoff", "":
			if !m.NextMatchID.Valid {
				if best == nil || m.RoundIndex > best.RoundIndex ||
					(m.RoundIndex == best.RoundIndex && m.Position < best.Position) {
					best = m
				}
			}
		}
	}
	return best
}

func findThirdPlaceMatch(matches []sqlc.Match) *sqlc.Match {
	for i := range matches {
		if matches[i].Stage == "third_place" {
			return &matches[i]
		}
	}
	return nil
}

func uniqueSidePlayers(sides []sqlc.MatchSide) []pgtype.UUID {
	seen := map[string]struct{}{}
	out := make([]pgtype.UUID, 0, len(sides))
	for _, s := range sides {
		for _, id := range []pgtype.UUID{s.PlayerID, s.PartnerID} {
			if !id.Valid {
				continue
			}
			key := uuidKey(id)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

// PodiumMismatch reports whether saved places disagree with the algorithm suggestion.
// Empty saved places are not a mismatch (nothing published yet).
func PodiumMismatch(saved, suggested []PodiumPlace) bool {
	if len(saved) == 0 || len(suggested) == 0 {
		return false
	}
	norm := func(list []PodiumPlace) map[int]string {
		out := map[int]string{}
		for _, p := range list {
			keys := uniqueValidKeys(p.PlayerIDs)
			sort.Strings(keys)
			joined := ""
			for i, k := range keys {
				if i > 0 {
					joined += ","
				}
				joined += k
			}
			out[p.Place] = joined
		}
		return out
	}
	a, b := norm(saved), norm(suggested)
	places := map[int]struct{}{}
	for p := range a {
		places[p] = struct{}{}
	}
	for p := range b {
		places[p] = struct{}{}
	}
	for p := range places {
		if a[p] != b[p] {
			return true
		}
	}
	return false
}
