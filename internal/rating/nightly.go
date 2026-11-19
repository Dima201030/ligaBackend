package rating

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// NightlyResult is the outcome of one nightly pass.
type NightlyResult struct {
	Season       int32
	Skipped      bool
	SkipReason   string
	PointsUpdated int
	SnapshotAt   time.Time
	Players      int
}

// NightlyOptions configures the ranking night job.
type NightlyOptions struct {
	Season   int32
	Location *time.Location
	AtHour   int // local hour (default 3)
	AtMinute int // local minute (default 30)
	Force    bool
	Log      *log.Logger
}

// RunNightly recalculates points from the rolling window and writes a rankings snapshot.
// Idempotent per local calendar day unless Force is set.
func RunNightly(ctx context.Context, store *db.Store, opts NightlyOptions) (NightlyResult, error) {
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	if opts.Season == 0 {
		opts.Season = int32(time.Now().In(opts.Location).Year())
	}
	logger := opts.Log
	if logger == nil {
		logger = log.Default()
	}

	now := time.Now().In(opts.Location)
	if !opts.Force {
		done, err := SnapshotExistsOnLocalDay(ctx, store, opts.Season, now)
		if err != nil {
			return NightlyResult{}, err
		}
		if done {
			logger.Printf("rankingsnapshot: skip season=%d — already snapshotted today (%s)", opts.Season, now.Format("2006-01-02"))
			return NightlyResult{
				Season:     opts.Season,
				Skipped:    true,
				SkipReason: "already_today",
			}, nil
		}
	}

	updated, err := RecalcPointsFromWindow(ctx, store, opts.Season, now)
	if err != nil {
		return NightlyResult{}, err
	}
	at, n, err := Snapshot(ctx, store, opts.Season)
	if err != nil {
		return NightlyResult{}, err
	}
	logger.Printf("rankingsnapshot: season=%d points_updated=%d players=%d at=%s",
		opts.Season, updated, n, at.UTC().Format(time.RFC3339))
	return NightlyResult{
		Season:        opts.Season,
		PointsUpdated: updated,
		SnapshotAt:    at,
		Players:       n,
	}, nil
}

// RecalcPointsFromWindow sets profile.points from best tournament results in the config window.
// Until singles/doubles are split on events, uses best (BestSingles+BestDoubles) of all tournament deltas.
func RecalcPointsFromWindow(ctx context.Context, store *db.Store, season int32, now time.Time) (int, error) {
	_, coeffs, err := loadConfig(ctx, store.Queries, season)
	if err != nil {
		return 0, err
	}
	weeks := coeffs.WindowWeeks
	if weeks <= 0 {
		weeks = DefaultCoeffs.WindowWeeks
	}
	bestN := coeffs.BestSingles + coeffs.BestDoubles
	if bestN <= 0 {
		bestN = DefaultCoeffs.BestSingles + DefaultCoeffs.BestDoubles
	}
	since := now.UTC().Add(-time.Duration(weeks) * 7 * 24 * time.Hour)

	rows, err := store.Queries.ListTournamentPointEventsInWindow(ctx, pgtype.Timestamptz{Time: since, Valid: true})
	if err != nil {
		return 0, err
	}

	byPlayer := map[string][]int{}
	ids := map[string]pgtype.UUID{}
	for _, row := range rows {
		key := uuidString(row.PlayerID)
		ids[key] = row.PlayerID
		byPlayer[key] = append(byPlayer[key], int(row.PointsDelta))
	}

	updated := 0
	for key, deltas := range byPlayer {
		sum := SelectBestSums(deltas, bestN)
		if _, err := store.Queries.SetProfilePoints(ctx, sqlc.SetProfilePointsParams{
			UserID: ids[key],
			Points: int32(sum),
		}); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

// SnapshotExistsOnLocalDay reports whether a rankings row exists for season on the local date of now.
func SnapshotExistsOnLocalDay(ctx context.Context, store *db.Store, season int32, now time.Time) (bool, error) {
	at, err := store.Queries.LatestRankingCalculatedAt(ctx, season)
	if err != nil {
		if errorsIsNoRows(err) {
			return false, nil
		}
		return false, err
	}
	if !at.Valid {
		return false, nil
	}
	loc := now.Location()
	a := at.Time.In(loc)
	b := now
	return a.Year() == b.Year() && a.YearDay() == b.YearDay(), nil
}

// NextRunAt returns the next local clock time for hour:minute (today or tomorrow).
func NextRunAt(now time.Time, hour, minute int) time.Time {
	loc := now.Location()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

// RunLoop sleeps until each scheduled time and runs nightly until ctx is cancelled.
func RunLoop(ctx context.Context, store *db.Store, opts NightlyOptions) error {
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	logger := opts.Log
	if logger == nil {
		logger = log.Default()
	}

	for {
		now := time.Now().In(opts.Location)
		next := NextRunAt(now, opts.AtHour, opts.AtMinute)
		wait := time.Until(next)
		logger.Printf("rankingsnapshot: next run at %s (in %s)", next.Format(time.RFC3339), wait.Round(time.Second))
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			if _, err := RunNightly(ctx, store, opts); err != nil {
				logger.Printf("rankingsnapshot: error: %v", err)
			}
		}
	}
}

func errorsIsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
