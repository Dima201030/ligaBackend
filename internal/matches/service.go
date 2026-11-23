package matches

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"

	"github.com/Dima201030/ligaBackend/internal/bracket"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Policy (explicit):
//
//   - Generate uses only confirmed registrations (never waitlist/pending).
//   - Generate may replace an existing draft or published bracket only when
//     no match has a recorded result (completed/walkover). Cancelled/bye alone
//     do not lock the bracket.
//   - After any completed/walkover result, regenerate is refused (ErrBracketLocked).
//   - Publish moves draft → published; re-publish of already published is idempotent.
//   - Submit score / walkover / cancel use optimistic version locking; concurrent
//     writers get ErrConflict. Winner advance into next_match runs in the same TX.
//   - SwapSlots exchanges two seedable sides without regenerating the tree.
//     Allowed only while no match has a recorded result (same lock as Generate).
var (
	ErrTournamentNotFound = errors.New("tournament not found")
	ErrNotEnoughPlayers   = errors.New("not enough players")
	ErrBracketLocked      = errors.New("bracket locked")
	ErrBracketMissing     = errors.New("bracket missing")
	ErrMatchNotFound      = errors.New("match not found")
	ErrConflict           = errors.New("conflict")
	ErrInvalidWinner      = errors.New("invalid winner")
	ErrMatchNotPlayable   = errors.New("match not playable")
	ErrSideIncomplete     = errors.New("side incomplete")
	ErrInvalidSlot        = errors.New("invalid slot")
	ErrSlotNotSwappable   = errors.New("slot not swappable")
)

type GenerateInput struct {
	TournamentID       pgtype.UUID
	ThirdPlace         bool
	GroupSize          int
	QualifiersPerGroup int
	Seeding            string // seed | rating | random
	RandomSeed         int64
	ActorID            pgtype.UUID
}

type ScoreInput struct {
	MatchID pgtype.UUID
	Sets    []SetScore
	Version int32
	ActorID pgtype.UUID
}

type WalkoverInput struct {
	MatchID     pgtype.UUID
	WinnerSide  int16
	Reason      string
	Version     int32
	ActorID     pgtype.UUID
}

// RetireInput awards a win when the opponent retires mid-match.
// Match is completed (full tournament points); Elo uses K × retire factor.
type RetireInput struct {
	MatchID    pgtype.UUID
	WinnerSide int16 // side that did not retire
	Sets       []SetScore // optional partial score for display
	Reason     string
	Version    int32
	ActorID    pgtype.UUID
}

// DisqualifyInput awards the match to winner_side after opponent DQ.
// Elo: full K win/loss. DQ'd player gets 0 tournament points for the event.
type DisqualifyInput struct {
	MatchID    pgtype.UUID
	WinnerSide int16 // side that was not disqualified
	Reason     string
	Version    int32
	ActorID    pgtype.UUID
}

type CancelInput struct {
	MatchID pgtype.UUID
	Reason  string
	Version int32
	ActorID pgtype.UUID
}

type ScheduleInput struct {
	MatchID     pgtype.UUID
	ScheduledAt pgtype.Timestamptz
	Court       pgtype.Text
	MarkLive    *bool
	ActorID     pgtype.UUID
}

// SlotRef points at one side of a match in a tournament bracket.
type SlotRef struct {
	MatchID pgtype.UUID
	Side    int16 // 1 or 2
}

type SwapInput struct {
	TournamentID pgtype.UUID
	A            SlotRef
	B            SlotRef
	ActorID      pgtype.UUID
}

// Generate builds a draw from confirmed registrations and persists it as draft.
func Generate(ctx context.Context, store *db.Store, in GenerateInput) (sqlc.Tournament, bracket.Draw, error) {
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return sqlc.Tournament{}, bracket.Draw{}, err
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)

	tour, err := q.LockTournamentByID(ctx, in.TournamentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Tournament{}, bracket.Draw{}, ErrTournamentNotFound
		}
		return sqlc.Tournament{}, bracket.Draw{}, err
	}

	results, err := q.CountResultMatches(ctx, in.TournamentID)
	if err != nil {
		return sqlc.Tournament{}, bracket.Draw{}, err
	}
	if results > 0 {
		return sqlc.Tournament{}, bracket.Draw{}, ErrBracketLocked
	}

	regs, err := q.ListConfirmedRegistrationsForBracket(ctx, in.TournamentID)
	if err != nil {
		return sqlc.Tournament{}, bracket.Draw{}, err
	}
	if len(regs) < 2 {
		return sqlc.Tournament{}, bracket.Draw{}, ErrNotEnoughPlayers
	}

	entries := registrationsToEntries(regs, in.Seeding, in.RandomSeed)
	opts := bracket.Options{
		ThirdPlace:         in.ThirdPlace,
		GroupSize:          in.GroupSize,
		QualifiersPerGroup: in.QualifiersPerGroup,
	}
	draw := bracket.Generate(bracket.Format(tour.Format), entries, opts)
	if draw.MatchCount == 0 {
		return sqlc.Tournament{}, bracket.Draw{}, ErrNotEnoughPlayers
	}

	if err := q.DeleteMatchesByTournament(ctx, in.TournamentID); err != nil {
		return sqlc.Tournament{}, bracket.Draw{}, err
	}

	flat := bracket.FlattenMatches(draw)
	created := make(map[string]sqlc.Match, len(flat))

	for _, m := range flat {
		status := sqlc.MatchStatusScheduled
		var winner pgtype.Int2
		isBye := m.AutomaticWinner != 0
		if isBye {
			status = sqlc.MatchStatusBye
			winner = pgtype.Int2{Int16: int16(m.AutomaticWinner), Valid: true}
		}
		var nextSlot, loserSlot pgtype.Int2
		if m.NextSlot != 0 {
			nextSlot = pgtype.Int2{Int16: int16(m.NextSlot), Valid: true}
		}
		if m.LoserNextSlot != 0 {
			loserSlot = pgtype.Int2{Int16: int16(m.LoserNextSlot), Valid: true}
		}
		row, err := q.CreateMatch(ctx, sqlc.CreateMatchParams{
			TournamentID:  in.TournamentID,
			BracketKey:    m.ID,
			Stage:         m.Stage,
			RoundIndex:    int32(m.RoundIndex),
			Position:      int32(m.Position),
			RoundLabel:    roundLabelFor(draw, m),
			Status:        status,
			WinnerSide:    winner,
			IsBye:         isBye,
			NextSlot:      nextSlot,
			LoserNextSlot: loserSlot,
		})
		if err != nil {
			return sqlc.Tournament{}, bracket.Draw{}, err
		}
		created[m.ID] = row

		if err := insertSide(ctx, q, row.ID, 1, m.Slot1); err != nil {
			return sqlc.Tournament{}, bracket.Draw{}, err
		}
		if err := insertSide(ctx, q, row.ID, 2, m.Slot2); err != nil {
			return sqlc.Tournament{}, bracket.Draw{}, err
		}
	}

	// Resolve next/loser links by bracket_key → uuid.
	for _, m := range flat {
		row := created[m.ID]
		var nextID, loserID pgtype.UUID
		var nextSlot, loserSlot pgtype.Int2
		if m.NextMatchID != "" {
			if next, ok := created[m.NextMatchID]; ok {
				nextID = next.ID
			}
		}
		if m.NextSlot != 0 {
			nextSlot = pgtype.Int2{Int16: int16(m.NextSlot), Valid: true}
		}
		if m.LoserNextMatchID != "" {
			if next, ok := created[m.LoserNextMatchID]; ok {
				loserID = next.ID
			}
		}
		if m.LoserNextSlot != 0 {
			loserSlot = pgtype.Int2{Int16: int16(m.LoserNextSlot), Valid: true}
		}
		if _, err := q.UpdateMatchLinks(ctx, sqlc.UpdateMatchLinksParams{
			ID:              row.ID,
			NextMatchID:     nextID,
			NextSlot:        nextSlot,
			LoserNextMatchID: loserID,
			LoserNextSlot:   loserSlot,
		}); err != nil {
			return sqlc.Tournament{}, bracket.Draw{}, err
		}
	}

	tour, err = q.SetTournamentBracketDraft(ctx, sqlc.SetTournamentBracketDraftParams{
		ID:                in.TournamentID,
		BracketThirdPlace: in.ThirdPlace,
	})
	if err != nil {
		return sqlc.Tournament{}, bracket.Draw{}, err
	}

	diff, _ := json.Marshal(map[string]any{
		"format":      tour.Format,
		"match_count": draw.MatchCount,
		"byes":        draw.Byes,
		"third_place": in.ThirdPlace,
		"seeding":     in.Seeding,
	})
	_, _ = q.InsertAuditLog(ctx, sqlc.InsertAuditLogParams{
		ActorID:  in.ActorID,
		Action:   "bracket.generate",
		Entity:   "tournament",
		EntityID: pgtype.Text{String: uuidString(in.TournamentID), Valid: true},
		Diff:     diff,
	})

	if err := tx.Commit(ctx); err != nil {
		return sqlc.Tournament{}, bracket.Draw{}, err
	}
	return tour, draw, nil
}

func roundLabelFor(draw bracket.Draw, m bracket.Match) string {
	switch m.Stage {
	case "third_place":
		return "Матч за 3-е место"
	case "grand_final":
		return "Большой финал"
	case "group":
		return "Группа"
	case "lower":
		return fmt.Sprintf("Нижняя сетка · %d", m.RoundIndex+1)
	default:
		// Look up from rounds.
		for _, r := range draw.Rounds {
			for _, rm := range r.Matches {
				if rm.ID == m.ID {
					return r.Label
				}
			}
		}
		for _, r := range draw.PlayoffRounds {
			for _, rm := range r.Matches {
				if rm.ID == m.ID {
					return r.Label
				}
			}
		}
		return bracket.RoundLabel(1) // fallback
	}
}

func insertSide(ctx context.Context, q *sqlc.Queries, matchID pgtype.UUID, side int16, slot bracket.Slot) error {
	var playerID, partnerID, regID pgtype.UUID
	var seed pgtype.Int4
	var label pgtype.Text
	if slot.PlayerID != "" {
		playerID = parseUUID(slot.PlayerID)
	}
	if slot.PartnerID != "" {
		partnerID = parseUUID(slot.PartnerID)
	}
	if slot.RegistrationID != "" {
		regID = parseUUID(slot.RegistrationID)
	}
	if slot.Seed != nil {
		seed = pgtype.Int4{Int32: int32(*slot.Seed), Valid: true}
	}
	if slot.Placeholder != "" {
		label = pgtype.Text{String: slot.Placeholder, Valid: true}
	} else if slot.Name != "" {
		label = pgtype.Text{String: slot.Name, Valid: true}
	}
	_, err := q.CreateMatchSide(ctx, sqlc.CreateMatchSideParams{
		MatchID:        matchID,
		Side:           side,
		PlayerID:       playerID,
		PartnerID:      partnerID,
		RegistrationID: regID,
		Seed:           seed,
		Label:          label,
		IsBye:          slot.IsBye,
	})
	return err
}

func registrationsToEntries(regs []sqlc.ListConfirmedRegistrationsForBracketRow, seeding string, randomSeed int64) []bracket.Entry {
	entries := make([]bracket.Entry, 0, len(regs))
	for _, r := range regs {
		name := fmt.Sprintf("%s %s", r.PlayerLastName, r.PlayerFirstName)
		var seed *int
		if r.Seed.Valid {
			s := int(r.Seed.Int32)
			seed = &s
		}
		e := bracket.Entry{
			PlayerID:       uuidString(r.PlayerID),
			RegistrationID: uuidString(r.ID),
			Name:           name,
			Seed:           seed,
			Rating:         int(r.PlayerRating + 0.5),
		}
		if r.PartnerID.Valid {
			e.PartnerID = uuidString(r.PartnerID)
		}
		entries = append(entries, e)
	}

	switch seeding {
	case "rating":
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].Rating != entries[j].Rating {
				return entries[i].Rating > entries[j].Rating
			}
			return entries[i].Name < entries[j].Name
		})
		for i := range entries {
			s := i + 1
			entries[i].Seed = &s
		}
	case "random":
		rng := rand.New(rand.NewSource(randomSeed))
		rng.Shuffle(len(entries), func(i, j int) { entries[i], entries[j] = entries[j], entries[i] })
		for i := range entries {
			s := i + 1
			entries[i].Seed = &s
		}
	default: // seed
		sort.SliceStable(entries, func(i, j int) bool {
			li, lj := 9999, 9999
			if entries[i].Seed != nil {
				li = *entries[i].Seed
			}
			if entries[j].Seed != nil {
				lj = *entries[j].Seed
			}
			if li != lj {
				return li < lj
			}
			return entries[i].Name < entries[j].Name
		})
		for i := range entries {
			s := i + 1
			entries[i].Seed = &s
		}
	}
	return entries
}

// Publish marks the draft bracket as published.
func Publish(ctx context.Context, store *db.Store, tournamentID, actorID pgtype.UUID) (sqlc.Tournament, error) {
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return sqlc.Tournament{}, err
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)

	tour, err := q.LockTournamentByID(ctx, tournamentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Tournament{}, ErrTournamentNotFound
		}
		return sqlc.Tournament{}, err
	}
	if tour.BracketStatus == sqlc.BracketStatusNone {
		return sqlc.Tournament{}, ErrBracketMissing
	}

	tour, err = q.PublishTournamentBracket(ctx, tournamentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Tournament{}, ErrBracketMissing
		}
		return sqlc.Tournament{}, err
	}

	_, _ = q.InsertAuditLog(ctx, sqlc.InsertAuditLogParams{
		ActorID:  actorID,
		Action:   "bracket.publish",
		Entity:   "tournament",
		EntityID: pgtype.Text{String: uuidString(tournamentID), Valid: true},
		Diff:     []byte(`{}`),
	})

	if err := tx.Commit(ctx); err != nil {
		return sqlc.Tournament{}, err
	}
	return tour, nil
}

// FinishOutcome is the result of submitting a score or walkover.
type FinishOutcome struct {
	Match               sqlc.Match
	TournamentWentLive  bool
	TournamentCompleted bool
	RatingsApplied      int
}

// SubmitScore records a set score and advances the winner (and loser to 3rd place if wired).
// First finished match promotes pending tournaments to live. Championship final
// (or last round-robin match) sets completed and awards tournament points.
func SubmitScore(ctx context.Context, store *db.Store, in ScoreInput) (FinishOutcome, error) {
	winner, text, err := ValidateAndDecideWinner(in.Sets)
	if err != nil {
		return FinishOutcome{}, err
	}
	setsJSON, err := SetsToJSON(in.Sets)
	if err != nil {
		return FinishOutcome{}, err
	}
	return finishMatch(ctx, store, finishParams{
		MatchID:    in.MatchID,
		Status:     sqlc.MatchStatusCompleted,
		WinnerSide: int16(winner),
		ScoreSets:  setsJSON,
		ScoreText:  text,
		Version:    in.Version,
		ActorID:    in.ActorID,
		Action:     "match.score",
	})
}

// Walkover awards a technical win without set score.
func Walkover(ctx context.Context, store *db.Store, in WalkoverInput) (FinishOutcome, error) {
	if in.WinnerSide != 1 && in.WinnerSide != 2 {
		return FinishOutcome{}, ErrInvalidWinner
	}
	diff := map[string]any{"winner_side": in.WinnerSide}
	if in.Reason != "" {
		diff["reason"] = in.Reason
	}
	raw, _ := json.Marshal(diff)
	return finishMatch(ctx, store, finishParams{
		MatchID:    in.MatchID,
		Status:     sqlc.MatchStatusWalkover,
		WinnerSide: in.WinnerSide,
		ScoreSets:  nil,
		ScoreText:  "тех. победа",
		Version:    in.Version,
		ActorID:    in.ActorID,
		Action:     "match.walkover",
		AuditDiff:  raw,
	})
}

// Retire records a mid-match retirement: completed status (full points), Elo K×0.5.
func Retire(ctx context.Context, store *db.Store, in RetireInput) (FinishOutcome, error) {
	if in.WinnerSide != 1 && in.WinnerSide != 2 {
		return FinishOutcome{}, ErrInvalidWinner
	}
	text := "снятие"
	var setsJSON []byte
	if len(in.Sets) > 0 {
		var err error
		setsJSON, err = SetsToJSON(in.Sets)
		if err != nil {
			return FinishOutcome{}, err
		}
		text = FormatPartialScoreText(in.Sets) + " · снятие"
	}
	diff := map[string]any{"winner_side": in.WinnerSide, "retire": true}
	if in.Reason != "" {
		diff["reason"] = in.Reason
	}
	raw, _ := json.Marshal(diff)
	return finishMatch(ctx, store, finishParams{
		MatchID:    in.MatchID,
		Status:     sqlc.MatchStatusCompleted,
		WinnerSide: in.WinnerSide,
		ScoreSets:  setsJSON,
		ScoreText:  text,
		Version:    in.Version,
		ActorID:    in.ActorID,
		Action:     "match.retire",
		AuditDiff:  raw,
		Retire:     true,
	})
}

// Disqualify records a DQ: status disqualified, full Elo, zero points for the loser at award time.
func Disqualify(ctx context.Context, store *db.Store, in DisqualifyInput) (FinishOutcome, error) {
	if in.WinnerSide != 1 && in.WinnerSide != 2 {
		return FinishOutcome{}, ErrInvalidWinner
	}
	diff := map[string]any{"winner_side": in.WinnerSide, "disqualified": true}
	if in.Reason != "" {
		diff["reason"] = in.Reason
	}
	raw, _ := json.Marshal(diff)
	return finishMatch(ctx, store, finishParams{
		MatchID:    in.MatchID,
		Status:     sqlc.MatchStatusDisqualified,
		WinnerSide: in.WinnerSide,
		ScoreSets:  nil,
		ScoreText:  "дисквалификация",
		Version:    in.Version,
		ActorID:    in.ActorID,
		Action:     "match.disqualify",
		AuditDiff:  raw,
	})
}

type finishParams struct {
	MatchID    pgtype.UUID
	Status     sqlc.MatchStatus
	WinnerSide int16
	ScoreSets  []byte
	ScoreText  string
	Version    int32
	ActorID    pgtype.UUID
	Action     string
	AuditDiff  []byte
	Retire     bool
}

func finishMatch(ctx context.Context, store *db.Store, in finishParams) (FinishOutcome, error) {
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return FinishOutcome{}, err
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)

	match, err := q.LockMatchByID(ctx, in.MatchID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return FinishOutcome{}, ErrMatchNotFound
		}
		return FinishOutcome{}, err
	}
	switch match.Status {
	case sqlc.MatchStatusScheduled, sqlc.MatchStatusLive:
		// playable
	case sqlc.MatchStatusCompleted, sqlc.MatchStatusWalkover, sqlc.MatchStatusDisqualified:
		// Another writer already finished — treat as optimistic conflict.
		return FinishOutcome{}, ErrConflict
	default:
		return FinishOutcome{}, ErrMatchNotPlayable
	}
	if match.Version != in.Version {
		return FinishOutcome{}, ErrConflict
	}

	sides, err := q.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{match.ID})
	if err != nil {
		return FinishOutcome{}, err
	}
	side1, side2, ok := pairSides(sides)
	if !ok {
		return FinishOutcome{}, ErrSideIncomplete
	}
	if !side1.PlayerID.Valid || !side2.PlayerID.Valid {
		return FinishOutcome{}, ErrSideIncomplete
	}

	var scoreSets []byte = in.ScoreSets
	if scoreSets == nil {
		scoreSets = []byte("null")
	}
	updated, err := q.SubmitMatchResult(ctx, sqlc.SubmitMatchResultParams{
		ID:         match.ID,
		Status:     in.Status,
		ScoreSets:  scoreSets,
		ScoreText:  pgtype.Text{String: in.ScoreText, Valid: in.ScoreText != ""},
		WinnerSide: pgtype.Int2{Int16: in.WinnerSide, Valid: true},
		Version:    in.Version,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return FinishOutcome{}, ErrConflict
		}
		return FinishOutcome{}, err
	}

	winner := side1
	loser := side2
	if in.WinnerSide == 2 {
		winner = side2
		loser = side1
	}

	if err := advanceSide(ctx, q, match.NextMatchID, match.NextSlot, winner); err != nil {
		return FinishOutcome{}, err
	}
	if err := advanceSide(ctx, q, match.LoserNextMatchID, match.LoserNextSlot, loser); err != nil {
		return FinishOutcome{}, err
	}

	if err := applyMatchRating(ctx, q, match, side1, side2, in); err != nil {
		return FinishOutcome{}, err
	}

	out := FinishOutcome{Match: updated}
	tour, err := q.GetTournamentByID(ctx, match.TournamentID)
	if err != nil {
		return FinishOutcome{}, err
	}
	shouldComplete, err := shouldCompleteAfterFinish(ctx, q, match, tour.Format)
	if err != nil {
		return FinishOutcome{}, err
	}
	if shouldComplete {
		res, err := maybeCompleteAndAward(ctx, q, match.TournamentID, in.ActorID)
		if err != nil {
			return FinishOutcome{}, err
		}
		out.TournamentCompleted = res.completed
		out.RatingsApplied = res.ratingsApplied
	} else {
		live, err := maybeMarkLive(ctx, q, match.TournamentID)
		if err != nil {
			return FinishOutcome{}, err
		}
		out.TournamentWentLive = live
	}

	diff := in.AuditDiff
	if diff == nil {
		diff, _ = json.Marshal(map[string]any{
			"status":               in.Status,
			"winner_side":          in.WinnerSide,
			"score":                in.ScoreText,
			"version":              in.Version,
			"tournament_went_live":  out.TournamentWentLive,
			"tournament_completed": out.TournamentCompleted,
			"ratings_applied":      out.RatingsApplied,
		})
	}
	_, _ = q.InsertAuditLog(ctx, sqlc.InsertAuditLogParams{
		ActorID:  in.ActorID,
		Action:   in.Action,
		Entity:   "match",
		EntityID: pgtype.Text{String: uuidString(match.ID), Valid: true},
		Diff:     diff,
	})

	if err := tx.Commit(ctx); err != nil {
		return FinishOutcome{}, err
	}
	return out, nil
}

// isChampionshipFinal reports whether finishing this match crowns a knockout winner.
// Olympic/playoff final: main|playoff with no next match. Double-elim: grand_final.
// Third-place, lower-bracket, and group matches are never finals.
// Round-robin must not use this — every RR match has no next_match_id.
func isChampionshipFinal(m sqlc.Match) bool {
	switch m.Stage {
	case "grand_final":
		return true
	case "third_place", "lower", "group":
		return false
	case "main", "playoff", "":
		return !m.NextMatchID.Valid
	default:
		return false
	}
}

func shouldCompleteAfterFinish(ctx context.Context, q *sqlc.Queries, m sqlc.Match, format sqlc.TournamentFormat) (bool, error) {
	if format == sqlc.TournamentFormatRoundRobin {
		open, err := q.CountOpenPlayableMatches(ctx, m.TournamentID)
		if err != nil {
			return false, err
		}
		return open == 0, nil
	}
	return isChampionshipFinal(m), nil
}

type completeAwardResult struct {
	completed      bool
	ratingsApplied int
}

func maybeMarkLive(ctx context.Context, q *sqlc.Queries, tournamentID pgtype.UUID) (bool, error) {
	_, err := q.MarkTournamentLiveIfPending(ctx, tournamentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func maybeCompleteAndAward(ctx context.Context, q *sqlc.Queries, tournamentID, actorID pgtype.UUID) (completeAwardResult, error) {
	tour, err := q.CompleteTournamentIfActive(ctx, tournamentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return completeAwardResult{}, nil
		}
		return completeAwardResult{}, err
	}
	n, err := rating.ApplyTournamentPointsTx(ctx, q, rating.TournamentApplyInput{
		TournamentID: tour.ID,
		Season:       tour.Season,
		Category:     tour.Category,
		Type:         string(tour.Type),
		ActorID:      actorID,
	})
	if err != nil {
		if errors.Is(err, rating.ErrConfigNotFound) {
			// Match + completed status stay; organizer can apply ratings after config exists.
			return completeAwardResult{completed: true}, nil
		}
		return completeAwardResult{}, err
	}
	return completeAwardResult{completed: true, ratingsApplied: n}, nil
}

func applyMatchRating(ctx context.Context, q *sqlc.Queries, match sqlc.Match, side1, side2 sqlc.MatchSide, in finishParams) error {
	tour, err := q.GetTournamentByID(ctx, match.TournamentID)
	if err != nil {
		return err
	}
	sidePlayers := func(side sqlc.MatchSide) ([]rating.PlayerRating, error) {
		out := make([]rating.PlayerRating, 0, 2)
		ids := []pgtype.UUID{side.PlayerID}
		if side.PartnerID.Valid {
			ids = append(ids, side.PartnerID)
		}
		for _, id := range ids {
			if !id.Valid {
				continue
			}
			prof, err := q.GetProfileByUserID(ctx, id)
			if err != nil {
				return nil, err
			}
			out = append(out, rating.PlayerRating{
				ID:            id,
				Rating:        prof.Rating,
				MatchesPlayed: int(prof.MatchesPlayed),
				Points:        prof.Points,
			})
		}
		return out, nil
	}
	s1, err := sidePlayers(side1)
	if err != nil {
		return err
	}
	s2, err := sidePlayers(side2)
	if err != nil {
		return err
	}
	doubles := side1.PartnerID.Valid || side2.PartnerID.Valid ||
		tour.Type == sqlc.TournamentTypeDoubles || tour.Type == sqlc.TournamentTypeMixed
	return rating.ApplyMatchElo(ctx, q, rating.MatchEloInput{
		ClubID:       tour.ClubID,
		MatchID:      match.ID,
		TournamentID: match.TournamentID,
		Season:       tour.Season,
		WinnerSide:   in.WinnerSide,
		Side1:        s1,
		Side2:        s2,
		Doubles:      doubles,
		Walkover:     in.Status == sqlc.MatchStatusWalkover,
		Retire:       in.Retire,
		Disqualify:   in.Status == sqlc.MatchStatusDisqualified,
		ActorID:      in.ActorID,
	})
}

func advanceSide(ctx context.Context, q *sqlc.Queries, nextMatchID pgtype.UUID, nextSlot pgtype.Int2, from sqlc.MatchSide) error {
	if !nextMatchID.Valid || !nextSlot.Valid {
		return nil
	}
	// Lock destination match to serialise parallel advances into the same slot.
	dest, err := q.LockMatchByID(ctx, nextMatchID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if dest.Status == sqlc.MatchStatusCompleted || dest.Status == sqlc.MatchStatusWalkover ||
		dest.Status == sqlc.MatchStatusDisqualified || dest.Status == sqlc.MatchStatusCancelled {
		return ErrConflict
	}

	label := from.Label
	if from.PlayerID.Valid && (!label.Valid || label.String == "") {
		// keep empty — API layer can resolve names
	}
	_, err = q.UpdateMatchSidePlayers(ctx, sqlc.UpdateMatchSidePlayersParams{
		MatchID:        nextMatchID,
		Side:           nextSlot.Int16,
		PlayerID:       from.PlayerID,
		PartnerID:      from.PartnerID,
		RegistrationID: from.RegistrationID,
		Seed:           from.Seed,
		Label:          label,
		IsBye:          false,
	})
	return err
}

func pairSides(sides []sqlc.MatchSide) (s1, s2 sqlc.MatchSide, ok bool) {
	for _, s := range sides {
		switch s.Side {
		case 1:
			s1 = s
		case 2:
			s2 = s
		}
	}
	return s1, s2, s1.ID.Valid && s2.ID.Valid
}

// Cancel marks a match cancelled without advancing.
func Cancel(ctx context.Context, store *db.Store, in CancelInput) (sqlc.Match, error) {
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return sqlc.Match{}, err
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)

	match, err := q.LockMatchByID(ctx, in.MatchID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Match{}, ErrMatchNotFound
		}
		return sqlc.Match{}, err
	}
	if match.Version != in.Version {
		return sqlc.Match{}, ErrConflict
	}

	updated, err := q.CancelMatch(ctx, sqlc.CancelMatchParams{
		ID:      match.ID,
		Version: in.Version,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Match{}, ErrConflict
		}
		return sqlc.Match{}, err
	}

	diff, _ := json.Marshal(map[string]any{"reason": in.Reason})
	_, _ = q.InsertAuditLog(ctx, sqlc.InsertAuditLogParams{
		ActorID:  in.ActorID,
		Action:   "match.cancel",
		Entity:   "match",
		EntityID: pgtype.Text{String: uuidString(match.ID), Valid: true},
		Diff:     diff,
	})

	if err := tx.Commit(ctx); err != nil {
		return sqlc.Match{}, err
	}
	return updated, nil
}

// UpdateSchedule patches court/time and optional live flag.
func UpdateSchedule(ctx context.Context, store *db.Store, in ScheduleInput) (sqlc.Match, error) {
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return sqlc.Match{}, err
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)

	match, err := q.LockMatchByID(ctx, in.MatchID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Match{}, ErrMatchNotFound
		}
		return sqlc.Match{}, err
	}

	updated, err := q.UpdateMatchSchedule(ctx, sqlc.UpdateMatchScheduleParams{
		ID:          match.ID,
		ScheduledAt: in.ScheduledAt,
		Court:       in.Court,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Match{}, ErrMatchNotPlayable
		}
		return sqlc.Match{}, err
	}

	if in.MarkLive != nil {
		status := sqlc.MatchStatusScheduled
		if *in.MarkLive {
			status = sqlc.MatchStatusLive
		}
		updated, err = q.SetMatchStatus(ctx, sqlc.SetMatchStatusParams{
			ID:     match.ID,
			Status: status,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return sqlc.Match{}, ErrMatchNotPlayable
			}
			return sqlc.Match{}, err
		}
	}

	_, _ = q.InsertAuditLog(ctx, sqlc.InsertAuditLogParams{
		ActorID:  in.ActorID,
		Action:   "match.schedule",
		Entity:   "match",
		EntityID: pgtype.Text{String: uuidString(match.ID), Valid: true},
		Diff:     []byte(`{}`),
	})

	if err := tx.Commit(ctx); err != nil {
		return sqlc.Match{}, err
	}
	return updated, nil
}

// SwapSlots exchanges two seedable bracket sides (player ↔ player or player ↔ bye)
// without regenerating the draw. Refused once any match has a recorded result.
func SwapSlots(ctx context.Context, store *db.Store, in SwapInput) (sqlc.Tournament, error) {
	if in.A.Side != 1 && in.A.Side != 2 {
		return sqlc.Tournament{}, ErrInvalidSlot
	}
	if in.B.Side != 1 && in.B.Side != 2 {
		return sqlc.Tournament{}, ErrInvalidSlot
	}
	if uuidString(in.A.MatchID) == uuidString(in.B.MatchID) && in.A.Side == in.B.Side {
		return sqlc.Tournament{}, ErrInvalidSlot
	}

	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return sqlc.Tournament{}, err
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)

	tour, err := q.LockTournamentByID(ctx, in.TournamentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Tournament{}, ErrTournamentNotFound
		}
		return sqlc.Tournament{}, err
	}
	if tour.BracketStatus == sqlc.BracketStatusNone {
		return sqlc.Tournament{}, ErrBracketMissing
	}

	results, err := q.CountResultMatches(ctx, in.TournamentID)
	if err != nil {
		return sqlc.Tournament{}, err
	}
	if results > 0 {
		return sqlc.Tournament{}, ErrBracketLocked
	}

	matchA, sideA, err := loadSwappableSlot(ctx, q, in.TournamentID, tour.Format, in.A)
	if err != nil {
		return sqlc.Tournament{}, err
	}
	matchB, sideB, err := loadSwappableSlot(ctx, q, in.TournamentID, tour.Format, in.B)
	if err != nil {
		return sqlc.Tournament{}, err
	}

	// Exchange payloads.
	if err := writeSide(ctx, q, matchA.ID, in.A.Side, sideB); err != nil {
		return sqlc.Tournament{}, err
	}
	if err := writeSide(ctx, q, matchB.ID, in.B.Side, sideA); err != nil {
		return sqlc.Tournament{}, err
	}

	touched := uniqueMatches(matchA, matchB)
	for _, m := range touched {
		if err := recomputeByeAdvancement(ctx, q, m.ID); err != nil {
			return sqlc.Tournament{}, err
		}
	}

	diff, _ := json.Marshal(map[string]any{
		"a": map[string]any{"match_id": uuidString(in.A.MatchID), "side": in.A.Side},
		"b": map[string]any{"match_id": uuidString(in.B.MatchID), "side": in.B.Side},
	})
	_, _ = q.InsertAuditLog(ctx, sqlc.InsertAuditLogParams{
		ActorID:  in.ActorID,
		Action:   "bracket.swap",
		Entity:   "tournament",
		EntityID: pgtype.Text{String: uuidString(in.TournamentID), Valid: true},
		Diff:     diff,
	})

	if err := tx.Commit(ctx); err != nil {
		return sqlc.Tournament{}, err
	}
	return tour, nil
}

func loadSwappableSlot(ctx context.Context, q *sqlc.Queries, tournamentID pgtype.UUID, format sqlc.TournamentFormat, ref SlotRef) (sqlc.Match, sqlc.MatchSide, error) {
	match, err := q.LockMatchByID(ctx, ref.MatchID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Match{}, sqlc.MatchSide{}, ErrMatchNotFound
		}
		return sqlc.Match{}, sqlc.MatchSide{}, err
	}
	if uuidString(match.TournamentID) != uuidString(tournamentID) {
		return sqlc.Match{}, sqlc.MatchSide{}, ErrMatchNotFound
	}
	switch match.Status {
	case sqlc.MatchStatusScheduled, sqlc.MatchStatusBye, sqlc.MatchStatusLive:
		// ok
	default:
		return sqlc.Match{}, sqlc.MatchSide{}, ErrMatchNotPlayable
	}
	if !slotStageSwappable(match, format) {
		return sqlc.Match{}, sqlc.MatchSide{}, ErrSlotNotSwappable
	}

	sides, err := q.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{match.ID})
	if err != nil {
		return sqlc.Match{}, sqlc.MatchSide{}, err
	}
	s1, s2, ok := pairSides(sides)
	if !ok {
		return sqlc.Match{}, sqlc.MatchSide{}, ErrSideIncomplete
	}
	side := s1
	if ref.Side == 2 {
		side = s2
	}
	if !side.PlayerID.Valid && !side.IsBye {
		// Placeholder (winner of …) — not a seed slot.
		return sqlc.Match{}, sqlc.MatchSide{}, ErrSlotNotSwappable
	}
	return match, side, nil
}

func slotStageSwappable(match sqlc.Match, format sqlc.TournamentFormat) bool {
	switch match.Stage {
	case "group":
		return true
	case "main", "lower":
		if format == sqlc.TournamentFormatRoundRobin {
			return true
		}
		// Olympic / DE / groups-playoff playoff: only seed round (index 0).
		return match.RoundIndex == 0
	default:
		return false
	}
}

func writeSide(ctx context.Context, q *sqlc.Queries, matchID pgtype.UUID, side int16, src sqlc.MatchSide) error {
	_, err := q.UpdateMatchSidePlayers(ctx, sqlc.UpdateMatchSidePlayersParams{
		MatchID:        matchID,
		Side:           side,
		PlayerID:       src.PlayerID,
		PartnerID:      src.PartnerID,
		RegistrationID: src.RegistrationID,
		Seed:           src.Seed,
		Label:          src.Label,
		IsBye:          src.IsBye,
	})
	return err
}

func uniqueMatches(matches ...sqlc.Match) []sqlc.Match {
	seen := map[string]struct{}{}
	out := make([]sqlc.Match, 0, len(matches))
	for _, m := range matches {
		key := uuidString(m.ID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, m)
	}
	return out
}

// recomputeByeAdvancement refreshes match bye state and the next-round slot
// that this match feeds when it is (or was) an automatic bye.
func recomputeByeAdvancement(ctx context.Context, q *sqlc.Queries, matchID pgtype.UUID) error {
	match, err := q.LockMatchByID(ctx, matchID)
	if err != nil {
		return err
	}
	sides, err := q.ListMatchSidesByMatchIDs(ctx, []pgtype.UUID{match.ID})
	if err != nil {
		return err
	}
	s1, s2, ok := pairSides(sides)
	if !ok {
		return ErrSideIncomplete
	}

	has1 := s1.PlayerID.Valid
	has2 := s2.PlayerID.Valid
	var (
		status     sqlc.MatchStatus
		isBye      bool
		winnerSide pgtype.Int2
		winner     sqlc.MatchSide
	)
	switch {
	case has1 && has2:
		status = sqlc.MatchStatusScheduled
		isBye = false
		// Clear accidental bye flag on sides if both have players.
		if s1.IsBye {
			s1.IsBye = false
			if err := writeSide(ctx, q, match.ID, 1, s1); err != nil {
				return err
			}
		}
		if s2.IsBye {
			s2.IsBye = false
			if err := writeSide(ctx, q, match.ID, 2, s2); err != nil {
				return err
			}
		}
	case has1 && !has2:
		status = sqlc.MatchStatusBye
		isBye = true
		winnerSide = pgtype.Int2{Int16: 1, Valid: true}
		winner = s1
		if !s2.IsBye || (s2.Label.Valid && s2.Label.String != "Пропуск") {
			s2.IsBye = true
			s2.Label = pgtype.Text{String: "Пропуск", Valid: true}
			s2.PlayerID = pgtype.UUID{}
			s2.PartnerID = pgtype.UUID{}
			s2.RegistrationID = pgtype.UUID{}
			s2.Seed = pgtype.Int4{}
			if err := writeSide(ctx, q, match.ID, 2, s2); err != nil {
				return err
			}
		}
	case !has1 && has2:
		status = sqlc.MatchStatusBye
		isBye = true
		winnerSide = pgtype.Int2{Int16: 2, Valid: true}
		winner = s2
		if !s1.IsBye || (s1.Label.Valid && s1.Label.String != "Пропуск") {
			s1.IsBye = true
			s1.Label = pgtype.Text{String: "Пропуск", Valid: true}
			s1.PlayerID = pgtype.UUID{}
			s1.PartnerID = pgtype.UUID{}
			s1.RegistrationID = pgtype.UUID{}
			s1.Seed = pgtype.Int4{}
			if err := writeSide(ctx, q, match.ID, 1, s1); err != nil {
				return err
			}
		}
	default:
		// Two empties / two byes — keep scheduled empty; clear advancement.
		status = sqlc.MatchStatusScheduled
		isBye = false
	}

	if _, err := q.SetMatchBracketState(ctx, sqlc.SetMatchBracketStateParams{
		ID:         match.ID,
		Status:     status,
		IsBye:      isBye,
		WinnerSide: winnerSide,
	}); err != nil {
		return err
	}

	if !match.NextMatchID.Valid || !match.NextSlot.Valid {
		return nil
	}

	placeholder := pgtype.Text{String: "Победитель " + match.BracketKey, Valid: true}
	if isBye && winner.PlayerID.Valid {
		return advanceSide(ctx, q, match.NextMatchID, match.NextSlot, winner)
	}
	_, err = q.ClearMatchSidePlayers(ctx, sqlc.ClearMatchSidePlayersParams{
		MatchID: match.NextMatchID,
		Side:    match.NextSlot.Int16,
		Label:   placeholder,
	})
	return err
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	b := id.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func parseUUID(s string) pgtype.UUID {
	var id pgtype.UUID
	_ = id.Scan(s)
	return id
}
