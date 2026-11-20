package rating

import (
	"context"
	"errors"
	"fmt"
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
	clubList, err := store.Queries.ListActiveClubs(ctx)
	if err != nil {
		return NightlyResult{}, err
	}
	if len(clubList) == 0 {
		return NightlyResult{Season: opts.Season, Skipped: true, SkipReason: "no_clubs"}, nil
	}

	totalUpdated := 0
	totalPlayers := 0
	var lastAt time.Time
	skippedAll := true
	for _, club := range clubList {
		if !opts.Force {
			done, err := SnapshotExistsOnLocalDay(ctx, store, club.ID, opts.Season, now)
			if err != nil {
				return NightlyResult{}, err
			}
			if done {
				logger.Printf("rankingsnapshot: skip club=%s season=%d — already snapshotted today (%s)",
					club.Slug, opts.Season, now.Format("2006-01-02"))
				continue
			}
		}
		skippedAll = false
		updated, err := RecalcPointsFromWindow(ctx, store, club.ID, opts.Season, now)
		if err != nil {
			return NightlyResult{}, fmt.Errorf("club %s recalc: %w", club.Slug, err)
		}
		at, n, err := Snapshot(ctx, store, club.ID, opts.Season)
		if err != nil {
			return NightlyResult{}, fmt.Errorf("club %s snapshot: %w", club.Slug, err)
		}
		totalUpdated += updated
		totalPlayers += n
		lastAt = at
		logger.Printf("rankingsnapshot: club=%s season=%d points_updated=%d players=%d at=%s",
			club.Slug, opts.Season, updated, n, at.UTC().Format(time.RFC3339))
	}
	if skippedAll {
		return NightlyResult{
			Season:     opts.Season,
			Skipped:    true,
			SkipReason: "already_today",
		}, nil
	}
	return NightlyResult{
		Season:        opts.Season,
		PointsUpdated: totalUpdated,
		SnapshotAt:    lastAt,
		Players:       totalPlayers,
	}, nil
}

// RecalcPointsFromWindow sets profile singles/doubles/points from best tournament results
// in the config window (best BestSingles singles + best BestDoubles doubles).
func RecalcPointsFromWindow(ctx context.Context, store *db.Store, clubID pgtype.UUID, season int32, now time.Time) (int, error) {
	_, coeffs, err := loadConfig(ctx, store.Queries, clubID, season)
	if err != nil {
		return 0, err
	}
	weeks := coeffs.WindowWeeks
	if weeks <= 0 {
		weeks = DefaultCoeffs.WindowWeeks
	}
	since := now.UTC().Add(-time.Duration(weeks) * 7 * 24 * time.Hour)

	rows, err := store.Queries.ListTournamentPointEventsInWindow(ctx, sqlc.ListTournamentPointEventsInWindowParams{
		ClubID:    clubID,
		CreatedAt: pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return 0, err
	}

	type buckets struct {
		singles []int
		doubles []int
	}
	byPlayer := map[string]*buckets{}
	ids := map[string]pgtype.UUID{}
	for _, row := range rows {
		key := uuidString(row.PlayerID)
		ids[key] = row.PlayerID
		b, ok := byPlayer[key]
		if !ok {
			b = &buckets{}
			byPlayer[key] = b
		}
		delta := int(row.PointsDelta)
		if NormalizeDiscipline(row.Discipline) == DisciplineDoubles {
			b.doubles = append(b.doubles, delta)
		} else {
			b.singles = append(b.singles, delta)
		}
	}

	updated := 0
	for key, b := range byPlayer {
		br := SplitBestWindowSums(b.singles, b.doubles, coeffs)
		if _, err := store.Queries.SetProfilePointsBreakdown(ctx, sqlc.SetProfilePointsBreakdownParams{
			UserID:        ids[key],
			SinglesPoints: int32(br.Singles),
			DoublesPoints: int32(br.Doubles),
		}); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

// SnapshotExistsOnLocalDay reports whether a rankings row exists for season on the local date of now.
func SnapshotExistsOnLocalDay(ctx context.Context, store *db.Store, clubID pgtype.UUID, season int32, now time.Time) (bool, error) {
	at, err := store.Queries.LatestRankingCalculatedAt(ctx, sqlc.LatestRankingCalculatedAtParams{
		ClubID: clubID,
		Season: season,
	})
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
