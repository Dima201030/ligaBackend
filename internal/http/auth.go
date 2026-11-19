package httpapi

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/mail"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	minPasswordLen       = 8
	emailConfirmTTL      = 48 * time.Hour
	registerRateLimitKey = "register:"
	loginRateLimitKey    = "login:"
)

type registerRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Password  string `json:"password"`
	Phone     string `json:"phone"`
	Level     string `json:"level"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type confirmEmailRequest struct {
	Token string `json:"token"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}

	firstName := strings.TrimSpace(req.FirstName)
	lastName := strings.TrimSpace(req.LastName)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	phone := strings.TrimSpace(req.Phone)
	level := strings.TrimSpace(req.Level)

	if firstName == "" || lastName == "" || email == "" {
		writeError(w, http.StatusBadRequest, "validation", "Заполните имя, фамилию и email")
		return
	}
	if !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "validation", "Укажите корректный email")
		return
	}
	if utf8.RuneCountInString(req.Password) < minPasswordLen {
		writeError(w, http.StatusBadRequest, "validation", "Пароль должен содержать минимум 8 символов")
		return
	}
	if !auth.ValidLevel(level) {
		writeError(w, http.StatusBadRequest, "validation", "Укажите уровень из списка взрослых категорий")
		return
	}

	ip := remoteIPString(r)
	if !s.limiter.Allow(registerRateLimitKey + ip + ":" + email) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Слишком много попыток. Подождите несколько минут.")
		return
	}

	hash, err := crypto.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать аккаунт")
		return
	}

	ctx := r.Context()
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать аккаунт")
		return
	}
	defer tx.Rollback(ctx)
	q := s.store.Queries.WithTx(tx)

	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        email,
		PasswordHash: hash,
		FirstName:    firstName,
		LastName:     lastName,
		Role:         sqlc.UserRolePlayer,
		Status:       sqlc.UserStatusPending,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "email_taken", "Аккаунт с таким email уже зарегистрирован")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать аккаунт")
		return
	}

	profile, err := q.CreateProfile(ctx, rating.NewProfileParams(
		user.ID, auth.TextOrNull(phone), level, firstName, lastName,
	))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать аккаунт")
		return
	}

	rawToken, err := crypto.RandomBytes(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать аккаунт")
		return
	}
	sum := crypto.HashToken(rawToken)
	_, err = q.CreateVerificationToken(ctx, sqlc.CreateVerificationTokenParams{
		UserID:    user.ID,
		Kind:      sqlc.VerificationKindEmail,
		TokenHash: sum[:],
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(emailConfirmTTL), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать аккаунт")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать аккаунт")
		return
	}

	confirmToken := base64.RawURLEncoding.EncodeToString(rawToken)
	payload := map[string]any{
		"first_name": firstName,
		"token":      confirmToken,
	}
	if s.cfg.PublicBaseURL != "" {
		payload["confirm_url"] = s.cfg.PublicBaseURL + "/api/auth/confirm-email?token=" + confirmToken
	}
	_ = mail.Enqueue(ctx, s.store, s.cfg, email, "confirm_email", payload)
	_ = mail.Enqueue(ctx, s.store, s.cfg, email, "registration_received", map[string]any{
		"first_name": firstName,
		"last_name":  lastName,
	})

	writeJSON(w, http.StatusCreated, map[string]any{
		"user": userWithProfileJSON(user, profile.Level, profile.Phone, user.CreatedAt, user.UpdatedAt),
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "validation", "Заполните email и пароль")
		return
	}

	ip := remoteIPString(r)
	if !s.limiter.Allow(loginRateLimitKey + ip + ":" + email) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Слишком много попыток. Подождите несколько минут.")
		return
	}

	ctx := r.Context()
	user, err := s.store.Queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "Неверный email или пароль")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось выполнить вход")
		return
	}

	ok, err := crypto.VerifyPassword(req.Password, user.PasswordHash)
	if err != nil || !ok {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Неверный email или пароль")
		return
	}

	name := user.FirstName + " " + user.LastName
	switch user.Status {
	case sqlc.UserStatusPending:
		writeJSON(w, http.StatusForbidden, errorBody{
			Code:    "pending",
			Message: name + ", аккаунт ещё не подтверждён администратором лиги",
		})
		return
	case sqlc.UserStatusRejected:
		writeJSON(w, http.StatusForbidden, errorBody{
			Code:    "rejected",
			Message: "Заявка " + name + " отклонена. Свяжитесь с организатором лиги, чтобы обсудить причину.",
		})
		return
	case sqlc.UserStatusApproved:
		// continue
	default:
		writeError(w, http.StatusForbidden, "forbidden", "Вход недоступен")
		return
	}

	if s.cfg.RequireEmailVerification && !user.EmailVerifiedAt.Valid {
		writeJSON(w, http.StatusForbidden, errorBody{
			Code:    "email_unverified",
			Message: "Подтвердите email по ссылке из письма, затем войдите снова",
		})
		return
	}

	profile, err := s.store.Queries.GetProfileByUserID(ctx, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось выполнить вход")
		return
	}

	token, _, err := auth.CreateSessionToken(ctx, s.store, user.ID, r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось выполнить вход")
		return
	}
	s.setSessionCookie(w, token)

	writeJSON(w, http.StatusOK, map[string]any{
		"user": userWithProfileJSON(user, profile.Level, profile.Phone, user.CreatedAt, user.UpdatedAt),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(s.cfg.SessionCookieName())
	if err == nil && cookie.Value != "" {
		if user, loadErr := auth.LoadSessionUser(r.Context(), s.store, cookie.Value); loadErr == nil && user != nil {
			_ = s.store.Queries.RevokeSession(r.Context(), user.SessionID)
		}
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleConfirmEmail(w http.ResponseWriter, r *http.Request) {
	token := ""
	if r.Method == http.MethodGet {
		token = strings.TrimSpace(r.URL.Query().Get("token"))
	} else {
		var req confirmEmailRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
			return
		}
		token = strings.TrimSpace(req.Token)
	}
	if token == "" {
		writeError(w, http.StatusBadRequest, "validation", "Укажите токен подтверждения")
		return
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		writeError(w, http.StatusBadRequest, "invalid_token", "Ссылка подтверждения недействительна")
		return
	}
	sum := crypto.HashToken(raw)
	ctx := r.Context()

	vt, err := s.store.Queries.GetVerificationTokenByHash(ctx, sum[:])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_token", "Ссылка подтверждения недействительна")
		return
	}
	if vt.Kind != sqlc.VerificationKindEmail || vt.UsedAt.Valid || !vt.ExpiresAt.Valid || !vt.ExpiresAt.Time.After(time.Now().UTC()) {
		writeError(w, http.StatusBadRequest, "invalid_token", "Ссылка подтверждения недействительна или устарела")
		return
	}

	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить email")
		return
	}
	defer tx.Rollback(ctx)
	q := s.store.Queries.WithTx(tx)

	if _, err := q.MarkVerificationTokenUsed(ctx, vt.ID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_token", "Ссылка подтверждения недействительна или устарела")
		return
	}
	user, err := q.MarkEmailVerified(ctx, vt.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already verified — treat as success.
			user, err = q.GetUserByID(ctx, vt.UserID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить email")
				return
			}
		} else {
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить email")
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить email")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"email": user.Email,
	})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.SessionCookieName(),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionAbsoluteTTL.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.SessionCookieName(),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func remoteIPString(r *http.Request) string {
	if addr := auth.ClientIPAddr(r); addr != nil {
		return addr.String()
	}
	return "unknown"
}
