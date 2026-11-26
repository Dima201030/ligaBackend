package matches

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// Qualifier labels written at generate time: A1, B2, …
var qualifierLabelRe = regexp.MustCompile(`^([A-Z])([1-9]\d*)$`)

type qualifierSide struct {
	PlayerID       pgtype.UUID
	PartnerID      pgtype.UUID
	RegistrationID pgtype.UUID
	Label          pgtype.Text
	Seed           pgtype.Int4
}

type groupStandingRow struct {
	key            string
	playerID       pgtype.UUID
	partnerID      pgtype.UUID
	registrationID pgtype.UUID
	label          string
	seed           pgtype.Int4
	wins           int
	losses         int
	setsFor        int
	setsAgainst    int
	gamesFor       int
	gamesAgainst   int
}

// syncGroupPlayoffQualifiers fills playoff slots A1/B1/… from finished group standings.
// Group matches have no next_match_id — without this, Final stays on placeholders forever.
func syncGroupPlayoffQualifiers(ctx context.Context, q *sqlc.Queries, tournamentID pgtype.UUID) error {
	matches, err := q.ListMatchesByTournament(ctx, tournamentID)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return nil
	}
	sides, err := q.ListMatchSidesByTournament(ctx, tournamentID)
	if err != nil {
		return err
	}
	sidesByMatch := make(map[string][]sqlc.MatchSide, len(matches))
	matchByID := make(map[string]sqlc.Match, len(matches))
	for _, m := range matches {
		key := uuidString(m.ID)
		matchByID[key] = m
	}
	for _, s := range sides {
		key := uuidString(s.MatchID)
		sidesByMatch[key] = append(sidesByMatch[key], s)
	}

	groupLetters := collectGroupLetters(matches)
	if len(groupLetters) == 0 {
		return nil
	}

	codes := qualifierCodesFromBracket(matches, sidesByMatch, matchByID, groupLetters)
	if len(codes) == 0 {
		return nil
	}

	fillers := make(map[string]qualifierSide, len(codes))
	for _, letter := range groupLetters {
		groupMatches := matchesForGroup(matches, letter)
		if len(groupMatches) == 0 {
			continue
		}
		if !groupFullyFinished(groupMatches) {
			continue
		}
		ranked := rankGroupStandings(groupMatches, sidesByMatch)
		for place, row := range ranked {
			code := letter + itoa(place+1)
			if _, want := codes[code]; !want {
				continue
			}
			fillers[code] = qualifierSide{
				PlayerID:       row.playerID,
				PartnerID:      row.partnerID,
				RegistrationID: row.registrationID,
				Label:          pgtype.Text{String: row.label, Valid: row.label != ""},
				Seed:           row.seed,
			}
		}
	}
	if len(fillers) == 0 {
		return nil
	}

	// Target slots: first playoff/main round sides still open, keyed by qualifier code.
	targets := findQualifierTargets(matches, sidesByMatch, codes)
	for code, side := range targets {
		filler, ok := fillers[code]
		if !ok {
			continue
		}
		// Preserve the slot's qualifier seed (1…n) so later corrections can re-find it.
		seed := side.Seed
		if !seed.Valid {
			seed = codes[code]
		}
		label := filler.Label
		if !label.Valid || label.String == "" {
			label = pgtype.Text{String: code, Valid: true}
		}
		if _, err := q.UpdateMatchSidePlayers(ctx, sqlc.UpdateMatchSidePlayersParams{
			MatchID:        side.MatchID,
			Side:           side.Side,
			PlayerID:       filler.PlayerID,
			PartnerID:      filler.PartnerID,
			RegistrationID: filler.RegistrationID,
			Seed:           seed,
			Label:          label,
			IsBye:          false,
		}); err != nil {
			return err
		}
	}
	return nil
}

// SyncGroupPlayoffQualifiers repairs stuck brackets where group results exist
// but playoff slots still show A1/B1 placeholders.
func SyncGroupPlayoffQualifiers(ctx context.Context, store *db.Store, tournamentID pgtype.UUID) error {
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)
	if err := syncGroupPlayoffQualifiers(ctx, q, tournamentID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func collectGroupLetters(matches []sqlc.Match) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range matches {
		if m.Stage != "group" {
			continue
		}
		letter := groupLetterFromKey(m.BracketKey)
		if letter == "" {
			continue
		}
		if _, ok := seen[letter]; ok {
			continue
		}
		seen[letter] = struct{}{}
		out = append(out, letter)
	}
	sort.Strings(out)
	return out
}

func groupLetterFromKey(key string) string {
	if key == "" {
		return ""
	}
	// Bracket keys: "A-GAR1M1", "B-GBR1M1"
	if i := strings.IndexByte(key, '-'); i > 0 {
		letter := strings.ToUpper(key[:i])
		if len(letter) == 1 && letter[0] >= 'A' && letter[0] <= 'Z' {
			return letter
		}
	}
	return ""
}

func matchesForGroup(matches []sqlc.Match, letter string) []sqlc.Match {
	var out []sqlc.Match
	for _, m := range matches {
		if m.Stage == "group" && groupLetterFromKey(m.BracketKey) == letter {
			out = append(out, m)
		}
	}
	return out
}

func groupFullyFinished(groupMatches []sqlc.Match) bool {
	for _, m := range groupMatches {
		switch m.Status {
		case sqlc.MatchStatusCompleted, sqlc.MatchStatusWalkover, sqlc.MatchStatusDisqualified, sqlc.MatchStatusBye, sqlc.MatchStatusCancelled:
			continue
		default:
			return false
		}
	}
	return len(groupMatches) > 0
}

// qualifierCodesFromBracket returns code → preferred seed for first-round playoff slots.
func qualifierCodesFromBracket(
	matches []sqlc.Match,
	sidesByMatch map[string][]sqlc.MatchSide,
	matchByID map[string]sqlc.Match,
	groupLetters []string,
) map[string]pgtype.Int4 {
	// Prefer explicit leftover placeholder labels (A1, B1, …).
	fromLabels := map[string]pgtype.Int4{}
	for _, s := range flattenSides(sidesByMatch) {
		m, ok := matchByID[uuidString(s.MatchID)]
		if !ok || !isKnockoutSeedRound(m) {
			continue
		}
		if m.Status == sqlc.MatchStatusCompleted || m.Status == sqlc.MatchStatusWalkover ||
			m.Status == sqlc.MatchStatusDisqualified || m.Status == sqlc.MatchStatusCancelled {
			continue
		}
		if parts := qualifierLabelRe.FindStringSubmatch(strings.TrimSpace(s.Label.String)); len(parts) == 3 {
			fromLabels[parts[0]] = s.Seed
		}
	}
	if len(fromLabels) > 0 {
		return fromLabels
	}

	// After a prior fill labels are names — rebuild codes like Generate does.
	nSlots := 0
	for _, m := range matches {
		if !isKnockoutSeedRound(m) {
			continue
		}
		nSlots += len(sidesByMatch[uuidString(m.ID)])
	}
	if nSlots == 0 || len(groupLetters) == 0 || nSlots%len(groupLetters) != 0 {
		return nil
	}
	qpg := nSlots / len(groupLetters)
	out := make(map[string]pgtype.Int4, nSlots)
	seed := int32(1)
	for _, letter := range groupLetters {
		for place := 1; place <= qpg; place++ {
			code := letter + itoa(place)
			out[code] = pgtype.Int4{Int32: seed, Valid: true}
			seed++
		}
	}
	return out
}

func findQualifierTargets(
	matches []sqlc.Match,
	sidesByMatch map[string][]sqlc.MatchSide,
	codes map[string]pgtype.Int4,
) map[string]sqlc.MatchSide {
	// seed → code for round-0 lookup after labels were overwritten with names.
	seedToCode := map[int32]string{}
	for code, seed := range codes {
		if seed.Valid {
			seedToCode[seed.Int32] = code
		}
	}

	out := make(map[string]sqlc.MatchSide, len(codes))
	for _, m := range matches {
		if !isKnockoutSeedRound(m) {
			continue
		}
		switch m.Status {
		case sqlc.MatchStatusCompleted, sqlc.MatchStatusWalkover, sqlc.MatchStatusDisqualified, sqlc.MatchStatusCancelled:
			continue
		}
		for _, s := range sidesByMatch[uuidString(m.ID)] {
			label := strings.TrimSpace(s.Label.String)
			if parts := qualifierLabelRe.FindStringSubmatch(label); len(parts) == 3 {
				if _, ok := codes[parts[0]]; ok {
					out[parts[0]] = s
					continue
				}
			}
			if s.Seed.Valid {
				if code, ok := seedToCode[s.Seed.Int32]; ok {
					if _, taken := out[code]; !taken {
						out[code] = s
					}
				}
			}
		}
	}
	return out
}

func isKnockoutSeedRound(m sqlc.Match) bool {
	switch m.Stage {
	case "playoff", "main":
		return m.RoundIndex == 0
	default:
		return false
	}
}

func rankGroupStandings(groupMatches []sqlc.Match, sidesByMatch map[string][]sqlc.MatchSide) []groupStandingRow {
	rows := map[string]*groupStandingRow{}
	ensure := func(s sqlc.MatchSide) *groupStandingRow {
		if !s.PlayerID.Valid {
			return nil
		}
		key := uuidString(s.PlayerID)
		if s.PartnerID.Valid {
			key += ":" + uuidString(s.PartnerID)
		}
		if row, ok := rows[key]; ok {
			return row
		}
		row := &groupStandingRow{
			key:            key,
			playerID:       s.PlayerID,
			partnerID:      s.PartnerID,
			registrationID: s.RegistrationID,
			label:          strings.TrimSpace(s.Label.String),
			seed:           s.Seed,
		}
		rows[key] = row
		return row
	}

	for _, m := range groupMatches {
		sides := sidesByMatch[uuidString(m.ID)]
		s1, s2, ok := pairSides(sides)
		if !ok {
			continue
		}
		r1 := ensure(s1)
		r2 := ensure(s2)
		switch m.Status {
		case sqlc.MatchStatusCompleted, sqlc.MatchStatusWalkover, sqlc.MatchStatusDisqualified:
			if !m.WinnerSide.Valid || r1 == nil || r2 == nil {
				continue
			}
			var winner, loser *groupStandingRow
			if m.WinnerSide.Int16 == 1 {
				winner, loser = r1, r2
			} else if m.WinnerSide.Int16 == 2 {
				winner, loser = r2, r1
			} else {
				continue
			}
			winner.wins++
			loser.losses++
			applySetStats(m.ScoreSets, r1, r2)
		case sqlc.MatchStatusBye:
			if m.WinnerSide.Valid {
				if m.WinnerSide.Int16 == 1 && r1 != nil {
					r1.wins++
				}
				if m.WinnerSide.Int16 == 2 && r2 != nil {
					r2.wins++
				}
			}
		}
	}

	list := make([]groupStandingRow, 0, len(rows))
	for _, row := range rows {
		list = append(list, *row)
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.wins != b.wins {
			return a.wins > b.wins
		}
		if a.losses != b.losses {
			return a.losses < b.losses
		}
		setDiffA := a.setsFor - a.setsAgainst
		setDiffB := b.setsFor - b.setsAgainst
		if setDiffA != setDiffB {
			return setDiffA > setDiffB
		}
		gameDiffA := a.gamesFor - a.gamesAgainst
		gameDiffB := b.gamesFor - b.gamesAgainst
		if gameDiffA != gameDiffB {
			return gameDiffA > gameDiffB
		}
		return a.label < b.label
	})
	return list
}

func applySetStats(raw []byte, r1, r2 *groupStandingRow) {
	if len(raw) == 0 || string(raw) == "null" || r1 == nil || r2 == nil {
		return
	}
	sets, err := ParseSetsJSON(json.RawMessage(raw))
	if err != nil {
		return
	}
	for _, set := range sets {
		r1.gamesFor += set.Side1
		r1.gamesAgainst += set.Side2
		r2.gamesFor += set.Side2
		r2.gamesAgainst += set.Side1
		if set.Side1 > set.Side2 {
			r1.setsFor++
			r2.setsAgainst++
		} else if set.Side2 > set.Side1 {
			r2.setsFor++
			r1.setsAgainst++
		}
	}
}

func flattenSides(sidesByMatch map[string][]sqlc.MatchSide) []sqlc.MatchSide {
	var out []sqlc.MatchSide
	for _, list := range sidesByMatch {
		out = append(out, list...)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
