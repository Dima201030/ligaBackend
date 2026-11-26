package clubs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/limits"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidClubSlug = errors.New("invalid club slug")
	ErrInvalidClubName = errors.New("invalid club name")
	ErrClubSlugTaken   = errors.New("club slug taken")
)

// InputError is a Russian validation message for club onboarding fields.
type InputError struct {
	Msg string
	err error
}

func (e *InputError) Error() string { return e.Msg }
func (e *InputError) Unwrap() error { return e.err }

// OnboardInput creates a new tenant club with seed content.
type OnboardInput struct {
	Slug     string
	Name     string
	City     string
	Timezone string
	Season   int32
	OwnerID  pgtype.UUID
}

// OnboardResult is the seeded club.
type OnboardResult struct {
	Club sqlc.Club
}

type pageSeed struct {
	Slug  string
	Title string
	Body  string
}

// OnboardClub creates clubs row, owner membership, default pages, rating config.
func OnboardClub(ctx context.Context, pool *pgxpool.Pool, q *sqlc.Queries, in OnboardInput) (OnboardResult, error) {
	slug := NormalizeSlug(in.Slug)
	name := strings.TrimSpace(in.Name)
	city := strings.TrimSpace(in.City)
	if msg := limits.ValidateClubSlug(slug); msg != "" {
		return OnboardResult{}, &InputError{Msg: msg, err: ErrInvalidClubSlug}
	}
	if msg := limits.ValidateClubName(name); msg != "" {
		return OnboardResult{}, &InputError{Msg: msg, err: ErrInvalidClubName}
	}
	if utf8.RuneCountInString(city) > 80 {
		return OnboardResult{}, &InputError{Msg: "Город слишком длинный", err: ErrInvalidClubName}
	}
	if in.Season == 0 {
		in.Season = int32(time.Now().Year())
	}
	tz := strings.TrimSpace(in.Timezone)
	if tz == "" {
		tz = "Europe/Moscow"
	}

	exists, err := q.ClubSlugExists(ctx, slug)
	if err != nil {
		return OnboardResult{}, err
	}
	if exists {
		return OnboardResult{}, ErrClubSlugTaken
	}

	settings, err := json.Marshal(map[string]any{"timezone": tz})
	if err != nil {
		return OnboardResult{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return OnboardResult{}, err
	}
	defer tx.Rollback(ctx)
	qx := q.WithTx(tx)

	club, err := qx.CreateClub(ctx, sqlc.CreateClubParams{
		Slug:     slug,
		Name:     name,
		City:     city,
		Status:   sqlc.ClubStatusActive,
		Settings: settings,
	})
	if err != nil {
		return OnboardResult{}, err
	}

	if _, err := qx.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
		ClubID: club.ID,
		UserID: in.OwnerID,
		Role:   sqlc.ClubMemberRoleOwner,
		Status: sqlc.ClubMemberStatusActive,
	}); err != nil {
		return OnboardResult{}, err
	}

	for _, page := range defaultPages(name) {
		if _, err := qx.UpsertPage(ctx, sqlc.UpsertPageParams{
			ClubID:       club.ID,
			Slug:         page.Slug,
			Title:        page.Title,
			BodyMarkdown: page.Body,
			UpdatedBy:    in.OwnerID,
		}); err != nil {
			return OnboardResult{}, fmt.Errorf("seed page %s: %w", page.Slug, err)
		}
	}

	coeffs, err := seedCoefficients(ctx, qx, in.Season)
	if err != nil {
		return OnboardResult{}, err
	}
	if _, err := qx.InsertRatingConfig(ctx, sqlc.InsertRatingConfigParams{
		ClubID:       club.ID,
		Season:       in.Season,
		Version:      1,
		Coefficients: coeffs,
		CreatedBy:    in.OwnerID,
	}); err != nil {
		return OnboardResult{}, fmt.Errorf("seed rating config: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return OnboardResult{}, err
	}
	return OnboardResult{Club: club}, nil
}

func defaultPages(clubName string) []pageSeed {
	return []pageSeed{
		{
			Slug:  "club",
			Title: "Клуб",
			Body:  fmt.Sprintf("%s — теннисный клуб на платформе Pairly.", clubName),
		},
		{
			Slug:  "contacts",
			Title: "Контакты",
			Body:  "Телефон и адрес клуба.",
		},
		{
			Slug:  "rules",
			Title: "Правила",
			Body:  "Регламент участия в турнирах клуба.",
		},
	}
}

func seedCoefficients(ctx context.Context, q *sqlc.Queries, season int32) ([]byte, error) {
	cfg, err := q.GetLatestRatingConfig(ctx, sqlc.GetLatestRatingConfigParams{
		ClubID: DefaultUUID(),
		Season: season,
	})
	if err == nil && len(cfg.Coefficients) > 0 {
		return cfg.Coefficients, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return []byte(`{
      "start_rating": 1200,
      "min_rating": 800,
      "window_weeks": 52,
      "best_singles": 10,
      "best_doubles": 5,
      "base_by_wins": {"0": 30, "1": 75, "2": 135, "3": 210, "4": 300, "5": 375, "6": 435},
      "category": {
        "masters": 1.6, "challenger": 1.3, "futures": 1.0,
        "rookies": 0.8, "beginners": 0.6, "newcomers": 0.4
      },
      "tournament_type": {
        "singles": 1.0, "doubles": 0.8, "mixed": 0.8,
        "cup": 1.4, "final": 1.5, "teams": 0.9
      },
      "elo_k": {
        "singles": {"lt10": 32, "lt30": 24, "default": 16},
        "doubles": {"lt10": 16, "lt30": 12, "default": 8}
      },
      "walkover_points_factor": 0.5,
      "walkover_elo": false,
      "consolation_factor": 0.5,
      "teams_share": 0.6,
      "retire_elo_factor": 0.5
    }`), nil
}
