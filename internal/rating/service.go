package rating

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrConfigNotFound = errors.New("rating config not found")
	ErrAlreadyApplied = errors.New("rating already applied")
	ErrInvalidManual  = errors.New("invalid manual rating adjustment")
)

// MatchEloInput applies Elo after a completed/walkover match.
type MatchEloInput struct {
	MatchID      pgtype.UUID
	TournamentID pgtype.UUID
	Season       int32
	WinnerSide   int16
	Side1        []PlayerRating // player + optional partner
	Side2        []PlayerRating
	Doubles      bool
	Walkover     bool
	ActorID      pgtype.UUID
}

// PlayerRating is a competitor on a match side.
type PlayerRating struct {
	ID            pgtype.UUID
	Rating        float64
	MatchesPlayed int
	Points        int32
}

// ApplyMatchElo writes match rating_events and updates profiles (idempotent).
func ApplyMatchElo(ctx context.Context, q *sqlc.Queries, in MatchEloInput) error {
	if len(in.Side1) == 0 || len(in.Side2) == 0 {
		return nil
	}
	cfg, coeffs, err := loadConfig(ctx, q, in.Season)
	if err != nil {
		return err
	}
	if in.Walkover && !coeffs.WalkoverElo {
		return nil
	}

	avg1 := SideAverage(extractRatings(in.Side1))
	avg2 := SideAverage(extractRatings(in.Side2))

	applySide := func(players []PlayerRating, opponentAvg float64, won bool) error {
		score := 0.0
		winInc, lossInc := int32(0), int32(1)
		if won {
			score = 1
			winInc, lossInc = 1, 0
		}
		for _, p := range players {
			exists, err := q.RatingEventExistsForMatch(ctx, sqlc.RatingEventExistsForMatchParams{
				MatchID:  in.MatchID,
				PlayerID: p.ID,
			})
			if err != nil {
				return err
			}
			if exists {
				continue
			}
			k := KFactor(p.MatchesPlayed, in.Doubles, coeffs)
			delta := EloDelta(p.Rating, opponentAvg, k, score)
			after := ApplyEloFloor(p.Rating+delta, coeffs.MinRating)
			disc := DisciplineSingles
			if in.Doubles {
				disc = DisciplineDoubles
			}
			if _, err := q.InsertRatingEvent(ctx, sqlc.InsertRatingEventParams{
				PlayerID:     p.ID,
				MatchID:      in.MatchID,
				TournamentID: in.TournamentID,
				Kind:         sqlc.RatingEventKindMatch,
				Discipline:   disc,
				PointsDelta:  0,
				RatingDelta:  delta,
				RatingAfter:  after,
				PointsAfter:  p.Points,
				ConfigID:     cfg.ID,
				Reason:       "match result",
				CreatedBy:    in.ActorID,
			}); err != nil {
				return err
			}
			if _, err := q.ApplyProfileRatingDelta(ctx, sqlc.ApplyProfileRatingDeltaParams{
				UserID:     p.ID,
				Rating:     after,
				Points:     p.Points,
				MatchesInc: 1,
				WinsInc:    winInc,
				LossesInc:  lossInc,
			}); err != nil {
				return err
			}
		}
		return nil
	}

	if err := applySide(in.Side1, avg2, in.WinnerSide == 1); err != nil {
		return err
	}
	return applySide(in.Side2, avg1, in.WinnerSide == 2)
}

// ManualInput is an organizer adjustment with mandatory reason.
type ManualInput struct {
	PlayerID    pgtype.UUID
	Season      int32
	PointsDelta int32
	RatingDelta float64
	Discipline  string // singles|doubles; default singles for points_delta
	Reason      string
	ActorID     pgtype.UUID
}

// ApplyManual adjusts points/rating with an audit event.
func ApplyManual(ctx context.Context, store *db.Store, in ManualInput) (sqlc.RatingEvent, error) {
	if in.Reason == "" {
		return sqlc.RatingEvent{}, ErrInvalidManual
	}
	if in.PointsDelta == 0 && in.RatingDelta == 0 {
		return sqlc.RatingEvent{}, ErrInvalidManual
	}
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return sqlc.RatingEvent{}, err
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)

	cfg, coeffs, err := loadConfig(ctx, q, in.Season)
	if err != nil {
		return sqlc.RatingEvent{}, err
	}
	prof, err := q.LockProfileForUpdate(ctx, in.PlayerID)
	if err != nil {
		return sqlc.RatingEvent{}, err
	}
	disc := NormalizeDiscipline(in.Discipline)
	singles := prof.SinglesPoints
	doubles := prof.DoublesPoints
	if in.PointsDelta != 0 {
		if disc == DisciplineDoubles {
			doubles += in.PointsDelta
			if doubles < 0 {
				doubles = 0
			}
		} else {
			singles += in.PointsDelta
			if singles < 0 {
				singles = 0
			}
		}
	}
	newPoints := singles + doubles
	newRating := ApplyEloFloor(prof.Rating+in.RatingDelta, coeffs.MinRating)
	ev, err := q.InsertRatingEvent(ctx, sqlc.InsertRatingEventParams{
		PlayerID:     in.PlayerID,
		MatchID:      pgtype.UUID{},
		TournamentID: pgtype.UUID{},
		Kind:         sqlc.RatingEventKindManual,
		Discipline:   disc,
		PointsDelta:  in.PointsDelta,
		RatingDelta:  in.RatingDelta,
		RatingAfter:  newRating,
		PointsAfter:  newPoints,
		ConfigID:     cfg.ID,
		Reason:       in.Reason,
		CreatedBy:    in.ActorID,
	})
	if err != nil {
		return sqlc.RatingEvent{}, err
	}
	if _, err := q.SetProfilePointsBreakdown(ctx, sqlc.SetProfilePointsBreakdownParams{
		UserID:        in.PlayerID,
		SinglesPoints: singles,
		DoublesPoints: doubles,
	}); err != nil {
		return sqlc.RatingEvent{}, err
	}
	if _, err := q.ApplyProfileRatingDelta(ctx, sqlc.ApplyProfileRatingDeltaParams{
		UserID:     in.PlayerID,
		Rating:     newRating,
		Points:     newPoints,
		MatchesInc: 0,
		WinsInc:    0,
		LossesInc:  0,
	}); err != nil {
		return sqlc.RatingEvent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.RatingEvent{}, err
	}
	return ev, nil
}

// TournamentApplyInput awards points for a finished tournament.
type TournamentApplyInput struct {
	TournamentID pgtype.UUID
	Season       int32
	Category     string
	Type         string
	ActorID      pgtype.UUID
}

// ApplyTournamentPoints awards points once per player (idempotent).
func ApplyTournamentPoints(ctx context.Context, store *db.Store, in TournamentApplyInput) (int, error) {
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)

	cfg, coeffs, err := loadConfig(ctx, q, in.Season)
	if err != nil {
		return 0, err
	}
	participants, err := q.CountConfirmedParticipants(ctx, in.TournamentID)
	if err != nil {
		return 0, err
	}
	players, err := q.ListConfirmedPlayerIDs(ctx, in.TournamentID)
	if err != nil {
		return 0, err
	}
	applied := 0
	for _, playerID := range players {
		exists, err := q.RatingEventExistsForTournament(ctx, sqlc.RatingEventExistsForTournamentParams{
			TournamentID: in.TournamentID,
			PlayerID:     playerID,
		})
		if err != nil {
			return 0, err
		}
		if exists {
			continue
		}
		wins, err := q.CountMainDrawWins(ctx, sqlc.CountMainDrawWinsParams{
			TournamentID: in.TournamentID,
			PlayerID:     playerID,
		})
		if err != nil {
			return 0, err
		}
		pts := TournamentPoints(TournamentPointsInput{
			Wins:         int(wins),
			Participants: int(participants),
			Category:     in.Category,
			Type:         in.Type,
		}, coeffs)
		disc := DisciplineFromTournamentType(in.Type)
		prof, err := q.LockProfileForUpdate(ctx, playerID)
		if err != nil {
			return 0, err
		}
		singles := prof.SinglesPoints
		doubles := prof.DoublesPoints
		if disc == DisciplineDoubles {
			doubles += int32(pts)
		} else {
			singles += int32(pts)
		}
		newPoints := singles + doubles
		if _, err := q.InsertRatingEvent(ctx, sqlc.InsertRatingEventParams{
			PlayerID:     playerID,
			MatchID:      pgtype.UUID{},
			TournamentID: in.TournamentID,
			Kind:         sqlc.RatingEventKindTournament,
			Discipline:   disc,
			PointsDelta:  int32(pts),
			RatingDelta:  0,
			RatingAfter:  prof.Rating,
			PointsAfter:  newPoints,
			ConfigID:     cfg.ID,
			Reason:       fmt.Sprintf("tournament wins=%d discipline=%s", wins, disc),
			CreatedBy:    in.ActorID,
		}); err != nil {
			return 0, err
		}
		if _, err := q.SetProfilePointsBreakdown(ctx, sqlc.SetProfilePointsBreakdownParams{
			UserID:        playerID,
			SinglesPoints: singles,
			DoublesPoints: doubles,
		}); err != nil {
			return 0, err
		}
		applied++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return applied, nil
}

// Snapshot builds a rankings snapshot for the season from current profile points.
func Snapshot(ctx context.Context, store *db.Store, season int32) (time.Time, int, error) {
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return time.Time{}, 0, err
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)

	calcAt := time.Now().UTC()
	prevAt, err := q.LatestRankingCalculatedAt(ctx, season)
	prevMap := map[string]int32{}
	if err == nil && prevAt.Valid {
		rows, err := q.ListPreviousPositions(ctx, sqlc.ListPreviousPositionsParams{
			Season:        season,
			CalculatedAt:  prevAt,
		})
		if err != nil {
			return time.Time{}, 0, err
		}
		for _, row := range rows {
			prevMap[uuidString(row.PlayerID)] = row.Position
		}
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, 0, err
	}

	players, err := q.ListPublicPlayers(ctx, sqlc.ListPublicPlayersParams{
		Category:    pgtype.Text{},
		Query:       pgtype.Text{},
		LimitCount:  10000,
		OffsetCount: 0,
	})
	if err != nil {
		return time.Time{}, 0, err
	}
	// already ordered by points DESC, rating DESC
	for i, p := range players {
		pos := int32(i + 1)
		var prev pgtype.Int4
		if v, ok := prevMap[uuidString(p.ID)]; ok {
			prev = pgtype.Int4{Int32: v, Valid: true}
		}
		if _, err := q.InsertRankingRow(ctx, sqlc.InsertRankingRowParams{
			Season:           season,
			PlayerID:         p.ID,
			Position:         pos,
			PreviousPosition: prev,
			Points:           p.Points,
			Rating:           p.Rating,
			SinglesPoints:    p.SinglesPoints,
			DoublesPoints:    p.DoublesPoints,
			Category:         p.Level,
			CalculatedAt:     pgtype.Timestamptz{Time: calcAt, Valid: true},
		}); err != nil {
			return time.Time{}, 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, 0, err
	}
	return calcAt, len(players), nil
}

func loadConfig(ctx context.Context, q *sqlc.Queries, season int32) (sqlc.RatingConfig, Coefficients, error) {
	cfg, err := q.GetLatestRatingConfig(ctx, season)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.RatingConfig{}, Coefficients{}, ErrConfigNotFound
		}
		return sqlc.RatingConfig{}, Coefficients{}, err
	}
	coeffs, err := ParseCoefficients(cfg.Coefficients)
	if err != nil {
		return sqlc.RatingConfig{}, Coefficients{}, err
	}
	return cfg, coeffs, nil
}

func extractRatings(players []PlayerRating) []float64 {
	out := make([]float64, len(players))
	for i, p := range players {
		out[i] = p.Rating
	}
	return out
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	b := id.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
