package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/clubs"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type clubJSON struct {
	ID     string          `json:"id"`
	Slug   string          `json:"slug"`
	Name   string          `json:"name"`
	City   string          `json:"city"`
	Status string          `json:"status"`
	Settings json.RawMessage `json:"settings,omitempty"`
}

func clubFromRow(row sqlc.Club) clubJSON {
	out := clubJSON{
		ID:     uuidString(row.ID),
		Slug:   row.Slug,
		Name:   row.Name,
		City:   row.City,
		Status: string(row.Status),
	}
	if len(row.Settings) > 0 {
		out.Settings = json.RawMessage(row.Settings)
	}
	return out
}

func (s *Server) handleListClubs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Queries.ListActiveClubs(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить клубы")
		return
	}
	out := make([]clubJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, clubFromRow(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"clubs": out})
}

func (s *Server) handleGetClub(w http.ResponseWriter, r *http.Request) {
	slug := clubs.NormalizeSlug(chi.URLParam(r, "slug"))
	if slug == "" {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный slug клуба")
		return
	}
	row, err := s.store.Queries.GetActiveClubBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Клуб не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить клуб")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"club": clubFromRow(row)})
}

type onboardClubRequest struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	City     string `json:"city"`
	Timezone string `json:"timezone"`
	Season   int32  `json:"season"`
}

func (s *Server) handleOnboardClub(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	var req onboardClubRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	result, err := clubs.OnboardClub(r.Context(), s.store.Pool, s.store.Queries, clubs.OnboardInput{
		Slug:     req.Slug,
		Name:     req.Name,
		City:     req.City,
		Timezone: req.Timezone,
		Season:   req.Season,
		OwnerID:  actor.ID,
	})
	if err != nil {
		var inputErr *clubs.InputError
		switch {
		case errors.As(err, &inputErr):
			writeError(w, http.StatusBadRequest, "validation", inputErr.Msg)
		case errors.Is(err, clubs.ErrInvalidClubSlug):
			writeError(w, http.StatusBadRequest, "validation", "Укажите slug латиницей: my-club")
		case errors.Is(err, clubs.ErrInvalidClubName):
			writeError(w, http.StatusBadRequest, "validation", "Название клуба слишком короткое")
		case errors.Is(err, clubs.ErrClubSlugTaken):
			writeError(w, http.StatusConflict, "slug_taken", "Клуб с таким slug уже существует")
		default:
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать клуб")
		}
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"club": clubFromRow(result.Club)})
}

func (s *Server) handleMyClubs(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	rows, err := s.store.Queries.ListUserClubMemberships(r.Context(), actor.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить клубы")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"club_id":   uuidString(row.ClubID),
			"slug":      row.ClubSlug,
			"name":      row.ClubName,
			"status":    string(row.ClubStatus),
			"role":      string(row.Role),
			"membership_status": string(row.Status),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"clubs": out})
}

func (s *Server) currentClubID(r *http.Request) pgtype.UUID {
	if c := clubs.FromContext(r.Context()); c != nil {
		return c.ID
	}
	return clubs.DefaultUUID()
}

func (s *Server) sameClub(entityClubID pgtype.UUID, r *http.Request) bool {
	cur := s.currentClubID(r)
	return entityClubID.Valid && cur.Valid && entityClubID.Bytes == cur.Bytes
}

func (s *Server) loadClubTournament(r *http.Request, id pgtype.UUID) (sqlc.Tournament, error) {
	return s.store.Queries.GetTournamentByClubAndID(r.Context(), sqlc.GetTournamentByClubAndIDParams{
		ClubID: s.currentClubID(r),
		ID:     id,
	})
}

func (s *Server) loadClubNews(r *http.Request, id pgtype.UUID) (sqlc.NewsPost, error) {
	return s.store.Queries.GetNewsPostByClubAndID(r.Context(), sqlc.GetNewsPostByClubAndIDParams{
		ClubID: s.currentClubID(r),
		ID:     id,
	})
}

func (s *Server) loadClubEvent(r *http.Request, id pgtype.UUID) (sqlc.ClubEvent, error) {
	return s.store.Queries.GetClubEventByClubAndID(r.Context(), sqlc.GetClubEventByClubAndIDParams{
		ClubID: s.currentClubID(r),
		ID:     id,
	})
}

func resolveClubSlug(r *http.Request, fallback string) string {
	if h := clubs.NormalizeSlug(r.Header.Get("X-Club-Slug")); h != "" {
		return h
	}
	if q := clubs.NormalizeSlug(r.URL.Query().Get("club")); q != "" {
		return q
	}
	return clubs.NormalizeSlug(fallback)
}

func (s *Server) withClub(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.store == nil {
			next.ServeHTTP(w, r)
			return
		}
		// Club catalog endpoints resolve themselves; skip tenant lookup.
		path := r.URL.Path
		if path == "/api/clubs" || strings.HasPrefix(path, "/api/clubs/") {
			next.ServeHTTP(w, r)
			return
		}
		slug := resolveClubSlug(r, s.cfg.DefaultClubSlug)
		if slug == "" {
			slug = clubs.DefaultSlug
		}
		row, err := s.store.Queries.GetActiveClubBySlug(r.Context(), slug)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "club_not_found", "Клуб не найден")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось определить клуб")
			return
		}
		club := clubs.FromRow(row)
		r = r.WithContext(clubs.SetClub(r.Context(), &club))
		next.ServeHTTP(w, r)
	})
}
