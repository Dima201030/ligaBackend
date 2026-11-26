package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/mail"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type approveRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Level     string `json:"level"`
}

func (s *Server) handleListApplications(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Queries.ListApplications(r.Context(), s.currentClubID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить заявки")
		return
	}
	out := make([]applicationJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, applicationFromRow(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"applications": out})
}

func (s *Server) handleApproveApplication(w http.ResponseWriter, r *http.Request) {
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

	var req approveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	firstName := strings.TrimSpace(req.FirstName)
	lastName := strings.TrimSpace(req.LastName)
	if firstName == "" || lastName == "" {
		writeError(w, http.StatusBadRequest, "validation", "Имя и фамилия не могут быть пустыми — проверьте данные перед подтверждением.")
		return
	}

	ctx := r.Context()
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить заявку")
		return
	}
	defer tx.Rollback(ctx)
	q := s.store.Queries.WithTx(tx)

	before, err := q.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Заявка не найдена")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить заявку")
		return
	}
	beforeProfile, err := q.GetProfileByUserID(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить заявку")
		return
	}

	// Level is deprecated in player UX; keep existing profile value when omitted.
	rawLevel := strings.TrimSpace(req.Level)
	if rawLevel == "" {
		rawLevel = beforeProfile.Level
	}
	level, levelOK := auth.ResolveLevel(rawLevel)
	if !levelOK {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный уровень")
		return
	}

	user, err := q.ApproveUser(ctx, sqlc.ApproveUserParams{
		ID:        id,
		FirstName: firstName,
		LastName:  lastName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "not_pending", "Заявка уже обработана")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить заявку")
		return
	}
	if err := q.UpdateProfileLevelAndNames(ctx, sqlc.UpdateProfileLevelAndNamesParams{
		UserID: id,
		Level:  level,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить заявку")
		return
	}

	diff, _ := json.Marshal(map[string]any{
		"before": map[string]any{
			"first_name": before.FirstName,
			"last_name":  before.LastName,
			"level":      beforeProfile.Level,
			"status":     before.Status,
		},
		"after": map[string]any{
			"first_name": firstName,
			"last_name":  lastName,
			"level":      level,
			"status":     "approved",
		},
	})
	_, err = q.InsertAuditLog(ctx, sqlc.InsertAuditLogParams{
		ActorID:   actor.ID,
		Action:    "application.approve",
		Entity:    "user",
		EntityID:  pgtype.Text{String: uuidString(id), Valid: true},
		Diff:      diff,
		Ip:        auth.ClientIPAddr(r),
		UserAgent: auth.TextOrNull(r.UserAgent()),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить заявку")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить заявку")
		return
	}

	_ = mail.Enqueue(ctx, s.store, s.cfg, emailString(user.Email), "application_approved", map[string]any{
		"first_name": firstName,
		"last_name":  lastName,
		"level":      level,
	})

	profile, _ := s.store.Queries.GetProfileByUserID(ctx, id)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": userWithProfileJSON(user, profile.Level, profile.Phone, user.CreatedAt, user.UpdatedAt),
	})
}

func (s *Server) handleRejectApplication(w http.ResponseWriter, r *http.Request) {
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

	ctx := r.Context()
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось отклонить заявку")
		return
	}
	defer tx.Rollback(ctx)
	q := s.store.Queries.WithTx(tx)

	before, err := q.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Заявка не найдена")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось отклонить заявку")
		return
	}

	user, err := q.RejectUser(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "not_pending", "Заявка уже обработана")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось отклонить заявку")
		return
	}

	diff, _ := json.Marshal(map[string]any{
		"before": map[string]any{"status": before.Status},
		"after":  map[string]any{"status": "rejected"},
	})
	_, err = q.InsertAuditLog(ctx, sqlc.InsertAuditLogParams{
		ActorID:   actor.ID,
		Action:    "application.reject",
		Entity:    "user",
		EntityID:  pgtype.Text{String: uuidString(id), Valid: true},
		Diff:      diff,
		Ip:        auth.ClientIPAddr(r),
		UserAgent: auth.TextOrNull(r.UserAgent()),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось отклонить заявку")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось отклонить заявку")
		return
	}

	_ = mail.Enqueue(ctx, s.store, s.cfg, emailString(user.Email), "application_rejected", map[string]any{
		"first_name": user.FirstName,
		"last_name":  user.LastName,
	})

	profile, _ := s.store.Queries.GetProfileByUserID(ctx, id)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": userWithProfileJSON(user, profile.Level, profile.Phone, user.CreatedAt, user.UpdatedAt),
	})
}
