package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type updateMeRequest struct {
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
	Phone     *string `json:"phone"`
	City      *string `json:"city"`
	Bio       *string `json:"bio"`
	Level     *string `json:"level"`
	Category  *string `json:"category"`
	Gender    *string `json:"gender"`
}

type updateNotificationsRequest struct {
	Tournaments *bool `json:"tournaments"`
	Reminders   *bool `json:"reminders"`
	Results     *bool `json:"results"`
	News        *bool `json:"news"`
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	var req updateMeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	if req.FirstName == nil && req.LastName == nil && req.Phone == nil &&
		req.City == nil && req.Bio == nil && req.Level == nil && req.Category == nil && req.Gender == nil {
		writeError(w, http.StatusBadRequest, "validation", "Нет полей для обновления")
		return
	}

	firstName := user.FirstName
	lastName := user.LastName
	if req.FirstName != nil {
		firstName = strings.TrimSpace(*req.FirstName)
	}
	if req.LastName != nil {
		lastName = strings.TrimSpace(*req.LastName)
	}
	if firstName == "" || lastName == "" {
		writeError(w, http.StatusBadRequest, "validation", "Имя и фамилия обязательны")
		return
	}
	if utf8.RuneCountInString(firstName) > 80 || utf8.RuneCountInString(lastName) > 80 {
		writeError(w, http.StatusBadRequest, "validation", "Слишком длинное имя")
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

	phone := prof.Phone
	if req.Phone != nil {
		v := strings.TrimSpace(*req.Phone)
		if v == "" {
			phone = pgtype.Text{}
		} else {
			if utf8.RuneCountInString(v) > 40 {
				writeError(w, http.StatusBadRequest, "validation", "Слишком длинный телефон")
				return
			}
			phone = pgtype.Text{String: v, Valid: true}
		}
	}

	city := prof.City
	if req.City != nil {
		city = strings.TrimSpace(*req.City)
		if utf8.RuneCountInString(city) > 80 {
			writeError(w, http.StatusBadRequest, "validation", "Слишком длинный город")
			return
		}
	}

	bio := prof.Bio
	if req.Bio != nil {
		bio = strings.TrimSpace(*req.Bio)
		if utf8.RuneCountInString(bio) > 500 {
			writeError(w, http.StatusBadRequest, "validation", "Слишком длинное описание")
			return
		}
	}

	level := prof.Level
	levelInput := req.Level
	if levelInput == nil {
		levelInput = req.Category
	}
	if levelInput != nil {
		level = strings.TrimSpace(*levelInput)
		if !auth.ValidLevel(level) {
			writeError(w, http.StatusBadRequest, "validation", "Некорректная категория")
			return
		}
	}

	gender := prof.Gender
	if req.Gender != nil {
		g := strings.TrimSpace(*req.Gender)
		if g == "" {
			gender = pgtype.Text{}
		} else if g == "male" || g == "female" {
			gender = pgtype.Text{String: g, Valid: true}
		} else {
			writeError(w, http.StatusBadRequest, "validation", "Некорректный пол")
			return
		}
	}

	updatedUser, err := s.store.Queries.UpdateUserNames(r.Context(), sqlc.UpdateUserNamesParams{
		ID:        user.ID,
		FirstName: firstName,
		LastName:  lastName,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить имя")
		return
	}

	updatedProf, err := s.store.Queries.UpdateMyProfile(r.Context(), sqlc.UpdateMyProfileParams{
		UserID: user.ID,
		Phone:  phone,
		City:   city,
		Bio:    bio,
		Level:  level,
		Gender: gender,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить профиль")
		return
	}

	session := *user
	session.FirstName = updatedUser.FirstName
	session.LastName = updatedUser.LastName
	session.Level = updatedProf.Level
	if updatedProf.Phone.Valid {
		v := updatedProf.Phone.String
		session.Phone = &v
	} else {
		session.Phone = nil
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user":          sessionUserJSON(&session),
		"profile":       meProfileFromRow(updatedProf),
		"notifications": decodeNotificationPrefs(updatedProf.NotificationPrefs),
	})
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
	return meProfileJSON{
		Slug:          prof.Slug,
		City:          prof.City,
		Bio:           prof.Bio,
		AvatarURL:     textPtr(prof.AvatarUrl),
		Status:        prof.PlayerStatus,
		Rating:        round1(prof.Rating),
		Points:        prof.Points,
		SinglesPoints: prof.SinglesPoints,
		DoublesPoints: prof.DoublesPoints,
		MatchesPlayed: prof.MatchesPlayed,
		Wins:          prof.Wins,
		Losses:        prof.Losses,
		MemberSince:   formatTS(prof.CreatedAt),
	}
}
