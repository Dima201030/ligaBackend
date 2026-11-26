package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// erasePlayerAccount anonymizes PII, archives the club profile, leaves memberships,
// cancels active tournament registrations, and revokes sessions. Match FKs stay.
func (s *Server) erasePlayerAccount(
	ctx context.Context,
	q *sqlc.Queries,
	r *http.Request,
	userID pgtype.UUID,
	recordWithdrawal bool,
) error {
	if recordWithdrawal {
		if err := s.recordConsent(r, q, userID, "withdrawal", true); err != nil {
			return err
		}
		for _, kind := range []string{"pdn_processing", "public_profile", "marketing"} {
			_ = s.recordConsent(r, q, userID, kind, false)
		}
	}

	raw, err := crypto.RandomBytes(32)
	if err != nil {
		return err
	}
	deadHash, err := crypto.HashPassword(string(raw))
	if err != nil {
		return err
	}
	if _, err := q.AnonymizeUserAccount(ctx, sqlc.AnonymizeUserAccountParams{
		ID:           userID,
		PasswordHash: deadHash,
	}); err != nil {
		return err
	}
	if _, err := q.AnonymizeProfile(ctx, userID); err != nil {
		return err
	}
	_ = q.LeaveAllActiveClubMemberships(ctx, userID)
	_ = q.CancelActiveRegistrationsForPlayer(ctx, userID)
	_ = q.RevokeUserSessions(ctx, userID)
	return nil
}

func (s *Server) beginEraseTx(ctx context.Context) (pgx.Tx, *sqlc.Queries, error) {
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	return tx, s.store.Queries.WithTx(tx), nil
}

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
	tx, q, err := s.beginEraseTx(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
		return
	}
	defer tx.Rollback(ctx)

	if err := s.erasePlayerAccount(ctx, q, r, user.ID, true); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
		return
	}

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

// DELETE /api/organizer/players/{id} — owner erases a player account everywhere.
func (s *Server) handleDeleteOrganizerPlayer(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireClubOwnerRole(r); s.writeOwnerRoleError(w, err) {
		return
	}
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}

	ctx := r.Context()
	target, err := s.store.Queries.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Игрок не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
		return
	}
	if auth.IsOrganizerOrAbove(target.Role) {
		writeError(w, http.StatusForbidden, "forbidden", "Нельзя удалить организаторский аккаунт")
		return
	}
	prof, err := s.store.Queries.GetProfileByUserID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Игрок не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
		return
	}
	if !uuidEqual(prof.ClubID, s.currentClubID(r)) {
		writeError(w, http.StatusNotFound, "not_found", "Игрок не найден в этом клубе")
		return
	}

	tx, q, err := s.beginEraseTx(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
		return
	}
	defer tx.Rollback(ctx)

	if err := s.erasePlayerAccount(ctx, q, r, id, true); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "Аккаунт удалён и обезличен во всех списках.",
	})
}

func uuidEqual(a, b pgtype.UUID) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	return a.Bytes == b.Bytes
}
