package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type rankingJSON struct {
	Position         int32   `json:"position"`
	PreviousPosition *int32  `json:"previous_position,omitempty"`
	PlayerID         string  `json:"player_id"`
	Slug             string  `json:"slug"`
	FirstName        string  `json:"first_name"`
	LastName         string  `json:"last_name"`
	Category         string  `json:"category"`
	Points           int32   `json:"points"`
	Rating           float64 `json:"rating"`
	SinglesPoints    int32   `json:"singles_points"`
	DoublesPoints    int32   `json:"doubles_points"`
	CalculatedAt     string  `json:"calculated_at"`
}

type ratingEventJSON struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"`
	Discipline   string  `json:"discipline"`
	PointsDelta  int32   `json:"points_delta"`
	RatingDelta  float64 `json:"rating_delta"`
	RatingAfter  float64 `json:"rating_after"`
	PointsAfter  int32   `json:"points_after"`
	Reason       string  `json:"reason"`
	MatchID      *string `json:"match_id,omitempty"`
	TournamentID *string `json:"tournament_id,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

func (s *Server) handleListRankings(w http.ResponseWriter, r *http.Request) {
	season := int32(queryInt(r, "season", time.Now().Year(), 2000, 2100))
	category := strings.TrimSpace(r.URL.Query().Get("category"))

	rows, err := s.store.Queries.ListLatestRankings(r.Context(), sqlc.ListLatestRankingsParams{
		
		ClubID: s.currentClubID(r),Season:   season,
		Category: optionalText(category),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить рейтинг")
		return
	}
	if len(rows) == 0 {
		// Fallback: live table from profiles until first snapshot.
		players, err := s.store.Queries.ListPublicPlayers(r.Context(), sqlc.ListPublicPlayersParams{
			
		ClubID: s.currentClubID(r),Category:    optionalText(category),
			Query:       pgtype.Text{},
			LimitCount:  500,
			OffsetCount: 0,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить рейтинг")
			return
		}
		live := make([]rankingJSON, 0, len(players))
		for i, p := range players {
			live = append(live, rankingJSON{
				Position:      int32(i + 1),
				PlayerID:      uuidString(p.ID),
				Slug:          p.Slug,
				FirstName:     p.FirstName,
				LastName:      p.LastName,
				Category:      p.Level,
				Points:        p.Points,
				Rating:        round1(p.Rating),
				SinglesPoints: p.SinglesPoints,
				DoublesPoints: p.DoublesPoints,
				CalculatedAt:  time.Now().UTC().Format(time.RFC3339),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"season":   season,
			"source":   "live",
			"rankings": live,
		})
		return
	}
	out := make([]rankingJSON, 0, len(rows))
	for _, row := range rows {
		item := rankingJSON{
			Position:      row.Position,
			PlayerID:      uuidString(row.PlayerID),
			Slug:          row.Slug,
			FirstName:     row.FirstName,
			LastName:      row.LastName,
			Category:      row.Category,
			Points:        row.Points,
			Rating:        round1(row.Rating),
			SinglesPoints: row.SinglesPoints,
			DoublesPoints: row.DoublesPoints,
			CalculatedAt:  formatTS(row.CalculatedAt),
		}
		if row.PreviousPosition.Valid {
			v := row.PreviousPosition.Int32
			item.PreviousPosition = &v
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"season":   season,
		"source":   "snapshot",
		"rankings": out,
	})
}

func (s *Server) handlePlayerRatingEvents(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	player, err := s.store.Queries.GetPublicPlayerBySlug(r.Context(), sqlc.GetPublicPlayerBySlugParams{ClubID: s.currentClubID(r), Slug: slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Игрок не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить историю")
		return
	}
	limit := int32(queryInt(r, "limit", 50, 1, 200))
	rows, err := s.store.Queries.ListRatingEventsByPlayer(r.Context(), sqlc.ListRatingEventsByPlayerParams{
		PlayerID: player.ID,
		Limit:    limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить историю")
		return
	}
	out := make([]ratingEventJSON, 0, len(rows))
	for _, row := range rows {
		item := ratingEventJSON{
			ID:          uuidString(row.ID),
			Kind:        string(row.Kind),
			Discipline:  row.Discipline,
			PointsDelta: row.PointsDelta,
			RatingDelta: round1(row.RatingDelta),
			RatingAfter: round1(row.RatingAfter),
			PointsAfter: row.PointsAfter,
			Reason:      row.Reason,
			CreatedAt:   formatTS(row.CreatedAt),
		}
		if row.MatchID.Valid {
			v := uuidString(row.MatchID)
			item.MatchID = &v
		}
		if row.TournamentID.Valid {
			v := uuidString(row.TournamentID)
			item.TournamentID = &v
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

type manualRatingRequest struct {
	PlayerID    string  `json:"player_id"`
	Season      int     `json:"season"`
	PointsDelta int32   `json:"points_delta"`
	RatingDelta float64 `json:"rating_delta"`
	Discipline  string  `json:"discipline"`
	Reason      string  `json:"reason"`
}

func (s *Server) handleManualRating(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	var req manualRatingRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	playerID, err := parseUUID(req.PlayerID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный player_id")
		return
	}
	season := int32(req.Season)
	if season == 0 {
		season = int32(time.Now().Year())
	}
	ev, err := rating.ApplyManual(r.Context(), s.store, rating.ManualInput{
		
		ClubID: s.currentClubID(r),PlayerID:    playerID,
		Season:      season,
		PointsDelta: req.PointsDelta,
		RatingDelta: req.RatingDelta,
		Discipline:  req.Discipline,
		Reason:      strings.TrimSpace(req.Reason),
		ActorID:     actor.ID,
	})
	if err != nil {
		if errors.Is(err, rating.ErrInvalidManual) {
			writeError(w, http.StatusBadRequest, "validation", "Укажите причину и ненулевую корректировку")
			return
		}
		if errors.Is(err, rating.ErrConfigNotFound) {
			writeError(w, http.StatusConflict, "config_missing", "Нет конфигурации рейтинга для сезона")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось скорректировать рейтинг")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"event": ratingEventJSON{
			ID:          uuidString(ev.ID),
			Kind:        string(ev.Kind),
			PointsDelta: ev.PointsDelta,
			RatingDelta: round1(ev.RatingDelta),
			RatingAfter: round1(ev.RatingAfter),
			PointsAfter: ev.PointsAfter,
			Reason:      ev.Reason,
			CreatedAt:   formatTS(ev.CreatedAt),
		},
	})
}

func (s *Server) handleApplyTournamentRatings(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}
	tour, err := s.loadClubTournament(r, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось начислить очки")
		return
	}
	n, err := rating.ApplyTournamentPoints(r.Context(), s.store, rating.TournamentApplyInput{
		TournamentID: tour.ID,
		Season:       tour.Season,
		Category:     tour.Category,
		Type:         string(tour.Type),
		ActorID:      actor.ID,
	})
	if err != nil {
		if errors.Is(err, rating.ErrConfigNotFound) {
			writeError(w, http.StatusConflict, "config_missing", "Нет конфигурации рейтинга для сезона")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось начислить очки")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"applied": n, "tournament_id": uuidString(tour.ID)})
}

type snapshotRequest struct {
	Season int `json:"season"`
}

func (s *Server) handleRatingSnapshot(w http.ResponseWriter, r *http.Request) {
	var req snapshotRequest
	_ = decodeJSON(r, &req)
	season := int32(req.Season)
	if season == 0 {
		season = int32(time.Now().Year())
	}
	at, n, err := rating.Snapshot(r.Context(), s.store, s.currentClubID(r), season)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось пересчитать таблицу")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"season":        season,
		"players":       n,
		"calculated_at": at.UTC().Format(time.RFC3339),
	})
}
