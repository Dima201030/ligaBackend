package httpapi

import (
	"errors"
	"net/http"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
)

type updateNotificationsRequest struct {
	Tournaments *bool `json:"tournaments"`
	Reminders   *bool `json:"reminders"`
	Results     *bool `json:"results"`
	News        *bool `json:"news"`
}

// handleUpdateMe rejects self-serve profile mutations. Name, phone, level/category,
// city, bio and gender are set at registration or by organizer/admin only.
func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	writeError(w, http.StatusForbidden, "profile_readonly",
		"Изменение профиля недоступно. Имя, категорию, email и телефон меняет организатор.")
}

func (s *Server) handleGetMyNotifications(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	prof, err := s.store.Queries.GetProfileByUserID(r.Context(), user.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Профиль не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить профиль")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": decodeNotificationPrefs(prof.NotificationPrefs),
	})
}

func (s *Server) handleUpdateMyNotifications(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	var req updateNotificationsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	if req.Tournaments == nil && req.Reminders == nil && req.Results == nil && req.News == nil {
		writeError(w, http.StatusBadRequest, "validation", "Нет полей для обновления")
		return
	}

	prof, err := s.store.Queries.GetProfileByUserID(r.Context(), user.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Профиль не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить профиль")
		return
	}

	merged, err := mergeNotificationPrefs(decodeNotificationPrefs(prof.NotificationPrefs), map[string]*bool{
		"tournaments": req.Tournaments,
		"reminders":   req.Reminders,
		"results":     req.Results,
		"news":        req.News,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректные настройки уведомлений")
		return
	}
	raw, err := encodeNotificationPrefs(merged)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить настройки")
		return
	}
	row, err := s.store.Queries.UpdateNotificationPrefs(r.Context(), sqlc.UpdateNotificationPrefsParams{
		UserID:            user.ID,
		NotificationPrefs: raw,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить настройки")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": decodeNotificationPrefs(row.NotificationPrefs),
	})
}

func meProfileFromRow(prof sqlc.Profile) meProfileJSON {
	var verifiedAt *string
	if ts := formatTS(prof.VerifiedAt); ts != "" {
		verifiedAt = &ts
	}
	return meProfileJSON{
		Slug:               prof.Slug,
		City:               prof.City,
		Bio:                prof.Bio,
		AvatarURL:          textPtr(prof.AvatarUrl),
		Status:             prof.PlayerStatus,
		BirthDate:          formatDate(prof.BirthDate),
		VerificationStatus: string(prof.VerificationStatus),
		VerifiedAt:         verifiedAt,
		Rating:             round1(prof.Rating),
		Points:             prof.Points,
		SinglesPoints:      prof.SinglesPoints,
		DoublesPoints:      prof.DoublesPoints,
		MatchesPlayed:      prof.MatchesPlayed,
		Wins:               prof.Wins,
		Losses:             prof.Losses,
		MemberSince:        formatTS(prof.CreatedAt),
	}
}
