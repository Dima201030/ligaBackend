package httpapi

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

const ssoCodeTTL = 2 * time.Minute

// handleSSOIssue creates a one-time OIDC-style authorization code for the
// current liga session. Pairly exchanges it so the club host can Set-Cookie.
func (s *Server) handleSSOIssue(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}

	raw, err := crypto.RandomBytes(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось выдать код")
		return
	}
	sum := crypto.HashToken(raw)
	ctx := r.Context()

	if err := s.store.Queries.InvalidateUnusedTokens(ctx, sqlc.InvalidateUnusedTokensParams{
		UserID: user.ID,
		Kind:   sqlc.VerificationKindSso,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось выдать код")
		return
	}

	_, err = s.store.Queries.CreateVerificationToken(ctx, sqlc.CreateVerificationTokenParams{
		UserID:    user.ID,
		Kind:      sqlc.VerificationKindSso,
		TokenHash: sum[:],
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(ssoCodeTTL), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось выдать код")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"code":       base64.RawURLEncoding.EncodeToString(raw),
		"expires_in": int(ssoCodeTTL.Seconds()),
	})
}

// handleSSOConsume exchanges a one-time code for a session cookie and redirects.
// next must be a relative path under the club site (open-redirect safe).
func (s *Server) handleSSOConsume(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	next := sanitizeSSONext(r.URL.Query().Get("next"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "validation", "Укажите код SSO")
		return
	}

	raw, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil || len(raw) != 32 {
		writeError(w, http.StatusBadRequest, "invalid_code", "Код SSO недействителен")
		return
	}
	sum := crypto.HashToken(raw)
	ctx := r.Context()

	vt, err := s.store.Queries.GetVerificationTokenByHash(ctx, sum[:])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_code", "Код SSO недействителен")
		return
	}
	if vt.Kind != sqlc.VerificationKindSso || vt.UsedAt.Valid || !vt.ExpiresAt.Valid || !vt.ExpiresAt.Time.After(time.Now().UTC()) {
		writeError(w, http.StatusBadRequest, "invalid_code", "Код SSO недействителен или устарел")
		return
	}

	if _, err := s.store.Queries.MarkVerificationTokenUsed(ctx, vt.ID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_code", "Код SSO недействителен или устарел")
		return
	}

	token, _, err := auth.CreateSessionToken(ctx, s.store, vt.UserID, r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать сессию")
		return
	}
	s.setSessionCookie(w, token)

	http.Redirect(w, r, next, http.StatusFound)
}

func sanitizeSSONext(raw string) string {
	fallback := "/mp-7f3a9c2e5b8d4106/tournaments"
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || strings.HasPrefix(raw, "//") {
		return fallback
	}
	if !strings.HasPrefix(u.Path, "/") {
		return fallback
	}
	// Block API and absolute escapes; allow club basePath or /tournaments.
	if strings.Contains(u.Path, "/api/") {
		return fallback
	}
	if u.RawQuery != "" {
		return u.Path + "?" + u.RawQuery
	}
	return u.Path
}
