package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/limits"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type confirmVerificationRequest struct {
	FirstName string  `json:"first_name"`
	LastName  string  `json:"last_name"`
	BirthDate string  `json:"birth_date"` // YYYY-MM-DD
	Level     *string `json:"level"`
	Phone     *string `json:"phone"`
	Status    string  `json:"status"` // approved | rejected
	Note      *string `json:"note"`
}

// profileAllowsTournamentRegistration is the server-side gate (not UI).
func profileAllowsTournamentRegistration(status sqlc.ProfileVerificationStatus) bool {
	return status == sqlc.ProfileVerificationStatusApproved
}

// emailAllowsTournamentRegistration is separate from organizer FIO/profile verification.
func emailAllowsTournamentRegistration(verifiedAt pgtype.Timestamptz) bool {
	return verifiedAt.Valid
}

func (s *Server) requireEmailVerified(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
			return
		}
		if !emailAllowsTournamentRegistration(user.EmailVerifiedAt) {
			writeError(w, http.StatusForbidden, "email_unverified", "Подтвердите email по ссылке из письма, затем запишитесь на турнир")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireProfileVerified(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
			return
		}
		prof, err := s.store.Queries.GetProfileByUserID(r.Context(), user.ID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusForbidden, "profile_unverified", "Организатор ещё не подтвердил ваш профиль")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось проверить статус профиля")
			return
		}
		if !profileAllowsTournamentRegistration(prof.VerificationStatus) {
			writeError(w, http.StatusForbidden, "profile_unverified", "Запись на турнир откроется после подтверждения профиля организатором")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleConfirmPlayerVerification(w http.ResponseWriter, r *http.Request) {
	organizer := auth.UserFromContext(r.Context())
	if organizer == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}

	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}

	var req confirmVerificationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}

	status := sqlc.ProfileVerificationStatus(strings.TrimSpace(req.Status))
	switch status {
	case sqlc.ProfileVerificationStatusApproved, sqlc.ProfileVerificationStatusRejected:
	default:
		writeError(w, http.StatusBadRequest, "validation", "status: approved или rejected")
		return
	}

	firstName := strings.TrimSpace(req.FirstName)
	lastName := strings.TrimSpace(req.LastName)
	if status == sqlc.ProfileVerificationStatusApproved {
		if firstName == "" || lastName == "" {
			writeError(w, http.StatusBadRequest, "validation", "Укажите подтверждённые имя и фамилию")
			return
		}
		if strings.TrimSpace(req.BirthDate) == "" {
			writeError(w, http.StatusBadRequest, "validation", "Укажите дату рождения")
			return
		}
	}

	var birth pgtype.Date
	if bd := strings.TrimSpace(req.BirthDate); bd != "" {
		t, err := time.Parse("2006-01-02", bd)
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation", "Дата рождения: YYYY-MM-DD")
			return
		}
		if msg := limits.ValidateAge(t, time.Now().UTC()); msg != "" {
			writeError(w, http.StatusBadRequest, "validation", msg)
			return
		}
		birth = pgtype.Date{Time: t, Valid: true}
	}

	if req.Level != nil && strings.TrimSpace(*req.Level) != "" && !auth.ValidLevel(strings.TrimSpace(*req.Level)) {
		writeError(w, http.StatusBadRequest, "validation", "Укажите уровень из списка взрослых категорий")
		return
	}

	ctx := r.Context()
	existing, err := s.store.Queries.GetProfileByUserID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Игрок не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить профиль")
		return
	}
	if existing.ClubID != s.currentClubID(r) {
		writeError(w, http.StatusNotFound, "not_found", "Игрок не найден в этом клубе")
		return
	}

	if firstName != "" && lastName != "" {
		if _, err := s.store.Queries.UpdateUserNames(ctx, sqlc.UpdateUserNamesParams{
			ID:        id,
			FirstName: firstName,
			LastName:  lastName,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить ФИО")
			return
		}
	}

	prof, err := s.store.Queries.ConfirmProfileVerification(ctx, sqlc.ConfirmProfileVerificationParams{
		UserID:             id,
		Phone:              optionalTextPtr(req.Phone),
		Level:              optionalTextPtr(req.Level),
		BirthDate:          birth,
		VerificationStatus: status,
		VerifiedBy:         organizer.ID,
		VerificationNote:   optionalTextPtr(req.Note),
	})
	if err != nil {
		log.Printf("confirm profile verification: user=%s status=%s: %v", uuidString(id), status, err)
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить профиль")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"player": map[string]any{
			"id":                  uuidString(prof.UserID),
			"verification_status": string(prof.VerificationStatus),
			"birth_date":          formatDate(prof.BirthDate),
			"verified_at":         formatTS(prof.VerifiedAt),
			"level":               prof.Level,
			"phone":               publicPhone(prof.Phone),
		},
	})
}

func formatDate(d pgtype.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.Time.Format("2006-01-02")
	return &s
}
