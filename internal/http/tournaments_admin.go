package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/clubs"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// hiddenTournamentIDs returns the set of tournaments hidden from the public site.
func (s *Server) hiddenTournamentIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := s.store.Pool.Query(ctx,
		`SELECT id FROM tournaments WHERE club_id = $1 AND is_hidden`,
		s.currentClubIDFromContext(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id.Valid {
			out[uuidString(id)] = true
		}
	}
	return out, rows.Err()
}

// hiddenTournamentIDsAll is the platform-wide hidden set (cross-club catalog).
func (s *Server) hiddenTournamentIDsAll(ctx context.Context) (map[string]bool, error) {
	rows, err := s.store.Pool.Query(ctx, `SELECT id FROM tournaments WHERE is_hidden`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id.Valid {
			out[uuidString(id)] = true
		}
	}
	return out, rows.Err()
}

// currentClubIDFromContext mirrors currentClubID for callers without a request.
func (s *Server) currentClubIDFromContext(ctx context.Context) pgtype.UUID {
	if c := clubs.FromContext(ctx); c != nil {
		return c.ID
	}
	return clubs.DefaultUUID()
}

func (s *Server) isTournamentHidden(ctx context.Context, id pgtype.UUID) (bool, error) {
	var hidden bool
	err := s.store.Pool.QueryRow(ctx,
		`SELECT is_hidden FROM tournaments WHERE club_id = $1 AND id = $2`,
		s.currentClubIDFromContext(ctx), id).Scan(&hidden)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, pgx.ErrNoRows
	}
	return hidden, err
}

type tournamentVisibilityRequest struct {
	Hidden *bool `json:"hidden"`
}

func (s *Server) handleSetTournamentVisibility(w http.ResponseWriter, r *http.Request) {
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
	var req tournamentVisibilityRequest
	if err := decodeJSON(r, &req); err != nil || req.Hidden == nil {
		writeError(w, http.StatusBadRequest, "validation", "Укажите параметр hidden")
		return
	}

	tx, err := s.store.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось изменить видимость")
		return
	}
	defer tx.Rollback(r.Context())

	clubID := s.currentClubID(r)
	var title string
	if err := tx.QueryRow(r.Context(),
		`UPDATE tournaments SET is_hidden = $1, updated_at = now()
		 WHERE id = $2 AND club_id = $3
		 RETURNING title`, *req.Hidden, id, clubID).Scan(&title); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось изменить видимость")
		return
	}

	diff, _ := json.Marshal(map[string]any{"title": title, "hidden": *req.Hidden})
	if _, err := s.store.Queries.WithTx(tx).InsertAuditLog(r.Context(), sqlc.InsertAuditLogParams{
		ActorID:   actor.ID,
		Action:    "tournament.visibility",
		Entity:    "tournament",
		EntityID:  pgtype.Text{String: uuidString(id), Valid: true},
		Diff:      diff,
		Ip:        auth.ClientIPAddr(r),
		UserAgent: auth.TextOrNull(r.UserAgent()),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось изменить видимость")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось изменить видимость")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": uuidString(id), "hidden": *req.Hidden})
}

func (s *Server) handleDeleteTournament(w http.ResponseWriter, r *http.Request) {
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

	tx, err := s.store.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить турнир")
		return
	}
	defer tx.Rollback(r.Context())

	clubID := s.currentClubID(r)
	var title, slug string
	if err := tx.QueryRow(r.Context(),
		`DELETE FROM tournaments WHERE id = $1 AND club_id = $2
		 RETURNING title, slug`, id, clubID).Scan(&title, &slug); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить турнир")
		return
	}

	diff, _ := json.Marshal(map[string]any{"title": title, "slug": slug})
	if _, err := s.store.Queries.WithTx(tx).InsertAuditLog(r.Context(), sqlc.InsertAuditLogParams{
		ActorID:   actor.ID,
		Action:    "tournament.delete",
		Entity:    "tournament",
		EntityID:  pgtype.Text{String: uuidString(id), Valid: true},
		Diff:      diff,
		Ip:        auth.ClientIPAddr(r),
		UserAgent: auth.TextOrNull(r.UserAgent()),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить турнир")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить турнир")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
