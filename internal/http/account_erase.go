package httpapi

import (
	"net/http"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
)

// DELETE /api/auth/me — right to erasure via anonymization (keeps match FKs).
func (s *Server) handleDeleteMe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	if auth.IsOrganizerOrAbove(user.Role) {
		writeError(w, http.StatusForbidden, "forbidden", "Организаторский аккаунт удаляется через поддержку")
		return
	}

	ctx := r.Context()
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
		return
	}
	defer tx.Rollback(ctx)
	q := s.store.Queries.WithTx(tx)

	if err := s.recordConsent(r, q, user.ID, "withdrawal", true); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось зафиксировать отзыв согласия")
		return
	}
	for _, kind := range []string{"pdn_processing", "public_profile", "marketing"} {
		_ = s.recordConsent(r, q, user.ID, kind, false)
	}

	raw, err := crypto.RandomBytes(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
		return
	}
	deadHash, err := crypto.HashPassword(string(raw))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
		return
	}
	if _, err := q.AnonymizeUserAccount(ctx, sqlc.AnonymizeUserAccountParams{
		ID:           user.ID,
		PasswordHash: deadHash,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
		return
	}
	if _, err := q.AnonymizeProfile(ctx, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
		return
	}
	_ = q.RevokeUserSessions(ctx, user.ID)

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
		return
	}

	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "Аккаунт обезличен. Вход больше недоступен.",
	})
}
