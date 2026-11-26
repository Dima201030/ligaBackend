package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Dima201030/ligaBackend/internal/clubs"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/limits"
	"github.com/jackc/pgx/v5"
)

type organizerClubJSON struct {
	ID       string          `json:"id"`
	Slug     string          `json:"slug"`
	Name     string          `json:"name"`
	City     string          `json:"city"`
	Status   string          `json:"status"`
	Settings json.RawMessage `json:"settings"`
}

func organizerClubFromRow(row sqlc.Club) organizerClubJSON {
	return organizerClubJSON{
		ID:       uuidString(row.ID),
		Slug:     row.Slug,
		Name:     row.Name,
		City:     row.City,
		Status:   string(row.Status),
		Settings: clubs.PublicSettingsJSON(row.Settings),
	}
}

func (s *Server) writeClubRoleError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, errUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
	case errors.Is(err, errForbiddenClub):
		writeError(w, http.StatusForbidden, "forbidden", "Нужна роль organizer или owner в этом клубе")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось проверить права")
	}
	return true
}

// GET /api/organizer/club — current tenant club with safe settings.
func (s *Server) handleGetOrganizerClub(w http.ResponseWriter, r *http.Request) {
	if _, err := s.actorClubMembershipRole(r); s.writeClubRoleError(w, err) {
		return
	}
	row, err := s.store.Queries.GetClubByID(r.Context(), s.currentClubID(r))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Клуб не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить клуб")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"club": organizerClubFromRow(row)})
}

type patchClubRequest struct {
	Name     *string         `json:"name"`
	City     *string         `json:"city"`
	Settings json.RawMessage `json:"settings"`
}

// PATCH /api/organizer/club — update name/city/safe settings for current tenant.
func (s *Server) handlePatchOrganizerClub(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireClubOwnerRole(r); s.writeOwnerRoleError(w, err) {
		return
	}

	var req patchClubRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	if req.Name == nil && req.City == nil && len(req.Settings) == 0 {
		writeError(w, http.StatusBadRequest, "validation", "Укажите name, city или settings")
		return
	}

	existing, err := s.store.Queries.GetClubByID(r.Context(), s.currentClubID(r))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Клуб не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить клуб")
		return
	}

	name := existing.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if msg := limits.ValidateClubName(name); msg != "" {
			writeError(w, http.StatusBadRequest, "validation", msg)
			return
		}
	}
	city := existing.City
	if req.City != nil {
		city = strings.TrimSpace(*req.City)
		if utf8.RuneCountInString(city) > 80 {
			writeError(w, http.StatusBadRequest, "validation", "Город слишком длинный")
			return
		}
	}

	settings := existing.Settings
	if len(req.Settings) > 0 && string(req.Settings) != "null" {
		var patch map[string]any
		if err := json.Unmarshal(req.Settings, &patch); err != nil || patch == nil {
			writeError(w, http.StatusBadRequest, "validation", "settings должен быть объектом")
			return
		}
		merged, err := clubs.MergeSettings(existing.Settings, patch)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation", err.Error())
			return
		}
		settings = merged
	}

	updated, err := s.store.Queries.UpdateClub(r.Context(), sqlc.UpdateClubParams{
		ID:       existing.ID,
		Name:     name,
		City:     city,
		Status:   existing.Status,
		Settings: settings,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить клуб")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"club": organizerClubFromRow(updated)})
}
