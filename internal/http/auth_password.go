package httpapi

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/mail"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	passwordResetTTL      = time.Hour
	forgotRateLimitKey    = "forgot:"
	resendConfirmLimitKey = "resend-confirm:"
)

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type resendConfirmRequest struct {
	Email string `json:"email"`
}

// handleForgotPassword always returns 202 to avoid email enumeration.
func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "validation", "Укажите email")
		return
	}
	ip := remoteIPString(r)
	if !s.limiter.Allow(forgotRateLimitKey + ip + ":" + email) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Слишком много попыток. Подождите несколько минут.")
		return
	}

	ctx := r.Context()
	user, err := s.store.Queries.GetUserByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось обработать запрос")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"ok":      true,
			"message": "Если аккаунт существует, мы отправили письмо со ссылкой для сброса пароля",
		})
		return
	}

	rawToken, err := crypto.RandomBytes(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обработать запрос")
		return
	}
	sum := crypto.HashToken(rawToken)

	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обработать запрос")
		return
	}
	defer tx.Rollback(ctx)
	q := s.store.Queries.WithTx(tx)

	if err := q.InvalidateUnusedTokens(ctx, sqlc.InvalidateUnusedTokensParams{
		UserID: user.ID,
		Kind:   sqlc.VerificationKindReset,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обработать запрос")
		return
	}
	_, err = q.CreateVerificationToken(ctx, sqlc.CreateVerificationTokenParams{
		UserID:    user.ID,
		Kind:      sqlc.VerificationKindReset,
		TokenHash: sum[:],
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(passwordResetTTL), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обработать запрос")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обработать запрос")
		return
	}

	token := base64.RawURLEncoding.EncodeToString(rawToken)
	payload := map[string]any{
		"first_name": user.FirstName,
		"token":      token,
	}
	if s.cfg.PublicBaseURL != "" {
		payload["reset_url"] = s.cfg.PublicBaseURL + "/api/auth/reset-password?token=" + token
	}
	_ = mail.Enqueue(ctx, s.store, s.cfg, user.Email, "password_reset", payload)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":      true,
		"message": "Если аккаунт существует, мы отправили письмо со ссылкой для сброса пароля",
	})
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		token = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	password := req.NewPassword
	if token == "" || password == "" {
		writeError(w, http.StatusBadRequest, "validation", "Укажите токен и новый пароль")
		return
	}
	if utf8.RuneCountInString(password) < minPasswordLen {
		writeError(w, http.StatusBadRequest, "validation", "Пароль должен быть не короче 8 символов")
		return
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		writeError(w, http.StatusBadRequest, "invalid_token", "Ссылка сброса недействительна")
		return
	}
	sum := crypto.HashToken(raw)
	ctx := r.Context()

	vt, err := s.store.Queries.GetVerificationTokenByHash(ctx, sum[:])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_token", "Ссылка сброса недействительна")
		return
	}
	if vt.Kind != sqlc.VerificationKindReset || vt.UsedAt.Valid || !vt.ExpiresAt.Valid || !vt.ExpiresAt.Time.After(time.Now().UTC()) {
		writeError(w, http.StatusBadRequest, "invalid_token", "Ссылка сброса недействительна или устарела")
		return
	}

	hash, err := crypto.HashPassword(password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сменить пароль")
		return
	}

	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сменить пароль")
		return
	}
	defer tx.Rollback(ctx)
	q := s.store.Queries.WithTx(tx)

	if _, err := q.MarkVerificationTokenUsed(ctx, vt.ID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_token", "Ссылка сброса недействительна или устарела")
		return
	}
	if _, err := q.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
		ID:           vt.UserID,
		PasswordHash: hash,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сменить пароль")
		return
	}
	_ = q.RevokeUserSessions(ctx, vt.UserID)
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сменить пароль")
		return
	}

	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleResendConfirm re-issues email confirmation (anti-enumeration 202).
func (s *Server) handleResendConfirm(w http.ResponseWriter, r *http.Request) {
	var req resendConfirmRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "validation", "Укажите email")
		return
	}
	ip := remoteIPString(r)
	if !s.limiter.Allow(resendConfirmLimitKey + ip + ":" + email) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Слишком много попыток. Подождите несколько минут.")
		return
	}

	ctx := r.Context()
	user, err := s.store.Queries.GetUserByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось отправить письмо")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
		return
	}
	if user.EmailVerifiedAt.Valid {
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
		return
	}

	rawToken, err := crypto.RandomBytes(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось отправить письмо")
		return
	}
	sum := crypto.HashToken(rawToken)

	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось отправить письмо")
		return
	}
	defer tx.Rollback(ctx)
	q := s.store.Queries.WithTx(tx)
	if err := q.InvalidateUnusedTokens(ctx, sqlc.InvalidateUnusedTokensParams{
		UserID: user.ID,
		Kind:   sqlc.VerificationKindEmail,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось отправить письмо")
		return
	}
	_, err = q.CreateVerificationToken(ctx, sqlc.CreateVerificationTokenParams{
		UserID:    user.ID,
		Kind:      sqlc.VerificationKindEmail,
		TokenHash: sum[:],
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(emailConfirmTTL), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось отправить письмо")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось отправить письмо")
		return
	}

	token := base64.RawURLEncoding.EncodeToString(rawToken)
	payload := map[string]any{
		"first_name": user.FirstName,
		"token":      token,
	}
	if s.cfg.PublicBaseURL != "" {
		payload["confirm_url"] = s.cfg.PublicBaseURL + "/api/auth/confirm-email?token=" + token
	}
	_ = mail.Enqueue(ctx, s.store, s.cfg, user.Email, "confirm_email", payload)

	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}
