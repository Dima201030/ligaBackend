package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/matches"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type generateBracketRequest struct {
	ThirdPlace         bool   `json:"third_place"`
	GroupSize          int    `json:"group_size"`
	QualifiersPerGroup int    `json:"qualifiers_per_group"`
	Seeding            string `json:"seeding"`
	RandomSeed         int64  `json:"random_seed"`
}

type scoreRequest struct {
	Sets    json.RawMessage `json:"sets"`
	Version int32           `json:"version"`
}

type walkoverRequest struct {
	WinnerSide int16  `json:"winner_side"`
	Reason     string `json:"reason"`
	Version    int32  `json:"version"`
}

type retireRequest struct {
	WinnerSide int16           `json:"winner_side"`
	Sets       json.RawMessage `json:"sets"`
	Reason     string          `json:"reason"`
	Version    int32           `json:"version"`
}

type cancelMatchRequest struct {
	Reason  string `json:"reason"`
	Version int32  `json:"version"`
}

type scheduleRequest struct {
	ScheduledAt *string `json:"scheduled_at"`
	Court       *string `json:"court"`
	MarkLive    *bool   `json:"mark_live"`
}

func (s *Server) handleGetPublicBracket(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	tour, err := s.store.Queries.GetTournamentBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить сетку")
		return
	}
	user := auth.UserFromContext(r.Context())
	isOrg := user != nil && auth.IsOrganizerOrAbove(user.Role)
	if tour.BracketStatus != sqlc.BracketStatusPublished && !isOrg {
		writeJSON(w, http.StatusOK, map[string]any{
			"bracket": bracketMetaJSON{
				Status:     string(tour.BracketStatus),
				Version:    tour.BracketVersion,
				ThirdPlace: tour.BracketThirdPlace,
				Format:     string(tour.Format),
			},
			"matches": []matchJSON{},
		})
		return
	}
	s.writeBracketPayload(w, r, tour, true)
}

func (s *Server) handleGetPublicTournamentMatches(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	tour, err := s.store.Queries.GetTournamentBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить матчи")
		return
	}
	user := auth.UserFromContext(r.Context())
	isOrg := user != nil && auth.IsOrganizerOrAbove(user.Role)
	if tour.BracketStatus != sqlc.BracketStatusPublished && !isOrg {
		writeJSON(w, http.StatusOK, map[string]any{"matches": []matchJSON{}})
		return
	}
	list, err := s.loadTournamentMatches(r, tour.ID, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить матчи")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": list})
}

func (s *Server) handleGetOrganizerBracket(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}
	tour, err := s.store.Queries.GetTournamentByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить сетку")
		return
	}
	s.writeBracketPayload(w, r, tour, true)
}

func (s *Server) handleGenerateBracket(w http.ResponseWriter, r *http.Request) {
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
	var body generateBracketRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}

	seeding := strings.TrimSpace(body.Seeding)
	if seeding == "" {
		seeding = "seed"
	}
	randomSeed := body.RandomSeed
	if randomSeed == 0 {
		randomSeed = time.Now().UnixNano()
	}

	tour, draw, err := matches.Generate(r.Context(), s.store, matches.GenerateInput{
		TournamentID:       id,
		ThirdPlace:         body.ThirdPlace,
		GroupSize:          body.GroupSize,
		QualifiersPerGroup: body.QualifiersPerGroup,
		Seeding:            seeding,
		RandomSeed:         randomSeed,
		ActorID:            actor.ID,
	})
	if err != nil {
		switch {
		case errors.Is(err, matches.ErrTournamentNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
		case errors.Is(err, matches.ErrNotEnoughPlayers):
			writeError(w, http.StatusBadRequest, "not_enough_players", "Нужно минимум два подтверждённых участника")
		case errors.Is(err, matches.ErrBracketLocked):
			writeError(w, http.StatusConflict, "bracket_locked", "Сетка заблокирована: уже есть сыгранные матчи")
		default:
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось сгенерировать сетку")
		}
		return
	}

	list, _ := s.loadTournamentMatches(r, tour.ID, true)
	writeJSON(w, http.StatusCreated, map[string]any{
		"bracket": bracketMetaJSON{
			Status:     string(tour.BracketStatus),
			Version:    tour.BracketVersion,
			ThirdPlace: tour.BracketThirdPlace,
			Format:     string(tour.Format),
			MatchCount: draw.MatchCount,
		},
		"draw": map[string]any{
			"entries_count": draw.EntriesCount,
			"bracket_size":  draw.BracketSize,
			"byes":          draw.Byes,
			"match_count":   draw.MatchCount,
		},
		"matches": list,
	})
}

func (s *Server) handlePublishBracket(w http.ResponseWriter, r *http.Request) {
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
	tour, err := matches.Publish(r.Context(), s.store, id, actor.ID)
	if err != nil {
		switch {
		case errors.Is(err, matches.ErrTournamentNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
		case errors.Is(err, matches.ErrBracketMissing):
			writeError(w, http.StatusConflict, "bracket_missing", "Сначала сгенерируйте сетку")
		default:
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось опубликовать сетку")
		}
		return
	}
	var publishedAt *string
	if tour.BracketPublishedAt.Valid {
		v := tour.BracketPublishedAt.Time.UTC().Format(time.RFC3339)
		publishedAt = &v
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bracket": bracketMetaJSON{
			Status:      string(tour.BracketStatus),
			Version:     tour.BracketVersion,
			PublishedAt: publishedAt,
			ThirdPlace:  tour.BracketThirdPlace,
			Format:      string(tour.Format),
		},
	})
}

func (s *Server) handleListOrganizerMatches(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Queries.ListOrganizerMatches(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить матчи")
		return
	}
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	sides, err := s.store.Queries.ListMatchSidesByMatchIDs(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить матчи")
		return
	}
	players, _ := s.loadPlayerBriefs(r, sides)
	byMatch := sidesByMatch(sides)
	out := make([]matchJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, matchFromOrganizerRow(row, byMatch[uuidString(row.ID)], players))
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": out})
}

func (s *Server) handleListOrganizerTournamentMatches(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}
	if _, err := s.store.Queries.GetTournamentByID(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить матчи")
		return
	}
	list, err := s.loadTournamentMatches(r, id, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить матчи")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": list})
}

func (s *Server) handleSubmitScore(w http.ResponseWriter, r *http.Request) {
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
	var body scoreRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	sets, err := matches.ParseSetsJSON(body.Sets)
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", russianScoreErr(err))
		return
	}
	outcome, err := matches.SubmitScore(r.Context(), s.store, matches.ScoreInput{
		MatchID: id,
		Sets:    sets,
		Version: body.Version,
		ActorID: actor.ID,
	})
	if err != nil {
		s.writeMatchMutationError(w, err)
		return
	}
	list, _ := s.loadMatchesByIDs(r, []pgtype.UUID{outcome.Match.ID})
	resp := map[string]any{}
	if len(list) == 0 {
		resp["match"] = matchFromRow(outcome.Match, nil, nil)
	} else {
		resp["match"] = list[0]
	}
	if outcome.TournamentWentLive {
		resp["tournament_went_live"] = true
	}
	if outcome.TournamentCompleted {
		resp["tournament_completed"] = true
		resp["ratings_applied"] = outcome.RatingsApplied
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleWalkover(w http.ResponseWriter, r *http.Request) {
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
	var body walkoverRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	outcome, err := matches.Walkover(r.Context(), s.store, matches.WalkoverInput{
		MatchID:    id,
		WinnerSide: body.WinnerSide,
		Reason:     strings.TrimSpace(body.Reason),
		Version:    body.Version,
		ActorID:    actor.ID,
	})
	if err != nil {
		s.writeMatchMutationError(w, err)
		return
	}
	list, _ := s.loadMatchesByIDs(r, []pgtype.UUID{outcome.Match.ID})
	resp := map[string]any{}
	if len(list) == 0 {
		resp["match"] = matchFromRow(outcome.Match, nil, nil)
	} else {
		resp["match"] = list[0]
	}
	if outcome.TournamentWentLive {
		resp["tournament_went_live"] = true
	}
	if outcome.TournamentCompleted {
		resp["tournament_completed"] = true
		resp["ratings_applied"] = outcome.RatingsApplied
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRetireMatch(w http.ResponseWriter, r *http.Request) {
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
	var body retireRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	var sets []matches.SetScore
	if len(body.Sets) > 0 && string(body.Sets) != "null" {
		parsed, err := matches.ParseSetsJSON(body.Sets)
		if err != nil {
			// Partial mid-match scores may be incomplete — accept [[n,m],…] loosely.
			var pairs [][]int
			if err2 := json.Unmarshal(body.Sets, &pairs); err2 != nil {
				writeError(w, http.StatusBadRequest, "validation", "Некорректный счёт сетов")
				return
			}
			for _, p := range pairs {
				if len(p) != 2 || p[0] < 0 || p[1] < 0 {
					writeError(w, http.StatusBadRequest, "validation", "Некорректный счёт сетов")
					return
				}
				sets = append(sets, matches.SetScore{Side1: p[0], Side2: p[1]})
			}
		} else {
			sets = parsed
		}
	}
	outcome, err := matches.Retire(r.Context(), s.store, matches.RetireInput{
		MatchID:    id,
		WinnerSide: body.WinnerSide,
		Sets:       sets,
		Reason:     strings.TrimSpace(body.Reason),
		Version:    body.Version,
		ActorID:    actor.ID,
	})
	if err != nil {
		s.writeMatchMutationError(w, err)
		return
	}
	list, _ := s.loadMatchesByIDs(r, []pgtype.UUID{outcome.Match.ID})
	resp := map[string]any{}
	if len(list) == 0 {
		resp["match"] = matchFromRow(outcome.Match, nil, nil)
	} else {
		resp["match"] = list[0]
	}
	if outcome.TournamentWentLive {
		resp["tournament_went_live"] = true
	}
	if outcome.TournamentCompleted {
		resp["tournament_completed"] = true
		resp["ratings_applied"] = outcome.RatingsApplied
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCancelMatch(w http.ResponseWriter, r *http.Request) {
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
	var body cancelMatchRequest
	_ = decodeJSON(r, &body)
	updated, err := matches.Cancel(r.Context(), s.store, matches.CancelInput{
		MatchID: id,
		Reason:  strings.TrimSpace(body.Reason),
		Version: body.Version,
		ActorID: actor.ID,
	})
	if err != nil {
		s.writeMatchMutationError(w, err)
		return
	}
	list, _ := s.loadMatchesByIDs(r, []pgtype.UUID{updated.ID})
	if len(list) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"match": matchFromRow(updated, nil, nil)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"match": list[0]})
}

func (s *Server) handleUpdateMatchSchedule(w http.ResponseWriter, r *http.Request) {
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
	var body scheduleRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	var scheduledAt pgtype.Timestamptz
	if body.ScheduledAt != nil && strings.TrimSpace(*body.ScheduledAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*body.ScheduledAt))
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation", "Некорректная дата матча")
			return
		}
		scheduledAt = pgtype.Timestamptz{Time: t.UTC(), Valid: true}
	}
	var court pgtype.Text
	if body.Court != nil {
		court = pgtype.Text{String: strings.TrimSpace(*body.Court), Valid: true}
	}
	updated, err := matches.UpdateSchedule(r.Context(), s.store, matches.ScheduleInput{
		MatchID:     id,
		ScheduledAt: scheduledAt,
		Court:       court,
		MarkLive:    body.MarkLive,
		ActorID:     actor.ID,
	})
	if err != nil {
		s.writeMatchMutationError(w, err)
		return
	}
	list, _ := s.loadMatchesByIDs(r, []pgtype.UUID{updated.ID})
	if len(list) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"match": matchFromRow(updated, nil, nil)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"match": list[0]})
}

func (s *Server) writeMatchMutationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, matches.ErrMatchNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Матч не найден")
	case errors.Is(err, matches.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "Матч уже изменён — обновите страницу")
	case errors.Is(err, matches.ErrMatchNotPlayable):
		writeError(w, http.StatusConflict, "not_playable", "Матч нельзя изменить в текущем статусе")
	case errors.Is(err, matches.ErrSideIncomplete):
		writeError(w, http.StatusConflict, "sides_incomplete", "Обе стороны матча ещё не определены")
	case errors.Is(err, matches.ErrInvalidWinner):
		writeError(w, http.StatusBadRequest, "validation", "Укажите победителя: 1 или 2")
	case errors.Is(err, matches.ErrInvalidScore), errors.Is(err, matches.ErrEmptyScore):
		writeError(w, http.StatusBadRequest, "validation", russianScoreErr(err))
	default:
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить матч")
	}
}

func russianScoreErr(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ": "); i >= 0 {
		return strings.TrimSpace(msg[i+2:])
	}
	if errors.Is(err, matches.ErrEmptyScore) {
		return "Введите счёт матча"
	}
	return "Некорректный счёт"
}

func (s *Server) writeBracketPayload(w http.ResponseWriter, r *http.Request, tour sqlc.Tournament, includeByes bool) {
	list, err := s.loadTournamentMatches(r, tour.ID, includeByes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить сетку")
		return
	}
	var publishedAt *string
	if tour.BracketPublishedAt.Valid {
		v := tour.BracketPublishedAt.Time.UTC().Format(time.RFC3339)
		publishedAt = &v
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bracket": bracketMetaJSON{
			Status:      string(tour.BracketStatus),
			Version:     tour.BracketVersion,
			PublishedAt: publishedAt,
			ThirdPlace:  tour.BracketThirdPlace,
			Format:      string(tour.Format),
			MatchCount:  len(list),
		},
		"matches": list,
	})
}

func (s *Server) loadTournamentMatches(r *http.Request, tournamentID pgtype.UUID, includeByes bool) ([]matchJSON, error) {
	rows, err := s.store.Queries.ListMatchesByTournament(r.Context(), tournamentID)
	if err != nil {
		return nil, err
	}
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		if !includeByes && row.Status == sqlc.MatchStatusBye {
			continue
		}
		ids = append(ids, row.ID)
	}
	return s.loadMatchesByIDsFiltered(r, rows, ids, includeByes)
}

func (s *Server) loadMatchesByIDs(r *http.Request, ids []pgtype.UUID) ([]matchJSON, error) {
	if len(ids) == 0 {
		return []matchJSON{}, nil
	}
	sides, err := s.store.Queries.ListMatchSidesByMatchIDs(r.Context(), ids)
	if err != nil {
		return nil, err
	}
	players, _ := s.loadPlayerBriefs(r, sides)
	byMatch := sidesByMatch(sides)
	out := make([]matchJSON, 0, len(ids))
	for _, id := range ids {
		m, err := s.store.Queries.GetMatchByID(r.Context(), id)
		if err != nil {
			continue
		}
		out = append(out, matchFromRow(m, byMatch[uuidString(id)], players))
	}
	return out, nil
}

func (s *Server) loadMatchesByIDsFiltered(r *http.Request, rows []sqlc.Match, ids []pgtype.UUID, includeByes bool) ([]matchJSON, error) {
	sides, err := s.store.Queries.ListMatchSidesByMatchIDs(r.Context(), ids)
	if err != nil {
		return nil, err
	}
	players, _ := s.loadPlayerBriefs(r, sides)
	byMatch := sidesByMatch(sides)
	out := make([]matchJSON, 0, len(rows))
	for _, row := range rows {
		if !includeByes && row.Status == sqlc.MatchStatusBye {
			continue
		}
		out = append(out, matchFromRow(row, byMatch[uuidString(row.ID)], players))
	}
	return out, nil
}

func (s *Server) loadPlayerBriefs(r *http.Request, sides []sqlc.MatchSide) (map[string]playerBriefJSON, error) {
	ids := collectPlayerIDs(sides)
	out := map[string]playerBriefJSON{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.store.Queries.ListPlayerBriefsByIDs(r.Context(), ids)
	if err != nil {
		return out, err
	}
	for _, row := range rows {
		out[uuidString(row.ID)] = playerBriefJSON{
			ID:        uuidString(row.ID),
			FirstName: row.FirstName,
			LastName:  row.LastName,
			Level:     row.Level,
		}
	}
	return out, nil
}
