package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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
