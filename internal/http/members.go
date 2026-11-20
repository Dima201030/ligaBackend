package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type clubMemberJSON struct {
	ID         string `json:"id"`
	ClubID     string `json:"club_id"`
	UserID     string `json:"user_id"`
	Email      string `json:"email"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	UserStatus string `json:"user_status"`
	CreatedAt  string `json:"created_at,omitempty"`
}

func memberFromListRow(row sqlc.ListClubMembersRow) clubMemberJSON {
	out := clubMemberJSON{
		ID:         uuidString(row.ID),
		ClubID:     uuidString(row.ClubID),
		UserID:     uuidString(row.UserID),
		Email:      row.Email,
		FirstName:  row.FirstName,
		LastName:   row.LastName,
		Role:       string(row.Role),
		Status:     string(row.Status),
		UserStatus: string(row.UserStatus),
	}
	if row.CreatedAt.Valid {
		out.CreatedAt = row.CreatedAt.Time.UTC().Format(time.RFC3339)
	}
	return out
}

func memberFromGetRow(row sqlc.GetClubMembershipByIDRow) clubMemberJSON {
	out := clubMemberJSON{
		ID:         uuidString(row.ID),
		ClubID:     uuidString(row.ClubID),
		UserID:     uuidString(row.UserID),
		Email:      row.Email,
		FirstName:  row.FirstName,
		LastName:   row.LastName,
		Role:       string(row.Role),
		Status:     string(row.Status),
		UserStatus: string(row.UserStatus),
	}
	if row.CreatedAt.Valid {
		out.CreatedAt = row.CreatedAt.Time.UTC().Format(time.RFC3339)
	}
	return out
}

func parseClubMemberRole(raw string) (sqlc.ClubMemberRole, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "owner":
		return sqlc.ClubMemberRoleOwner, true
	case "organizer":
		return sqlc.ClubMemberRoleOrganizer, true
	case "player":
		return sqlc.ClubMemberRolePlayer, true
	default:
		return "", false
	}
}

func clubRoleRank(role sqlc.ClubMemberRole) int {
	switch role {
	case sqlc.ClubMemberRoleOwner:
		return 3
	case sqlc.ClubMemberRoleOrganizer:
		return 2
	case sqlc.ClubMemberRolePlayer:
		return 1
	default:
		return 0
	}
}

// actorClubMembershipRole returns the caller's membership role in the current club.
// Global owner/superadmin is treated as club owner when they have no membership row.
func (s *Server) actorClubMembershipRole(r *http.Request) (sqlc.ClubMemberRole, error) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		return "", errUnauthorized
	}
	row, err := s.store.Queries.GetClubMembership(r.Context(), sqlc.GetClubMembershipParams{
		ClubID: s.currentClubID(r),
		UserID: actor.ID,
	})
	if err == nil && row.Status == sqlc.ClubMemberStatusActive {
		return row.Role, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	// Global owner/superadmin may manage any club even without a membership row.
	switch actor.Role {
	case sqlc.UserRoleOwner, sqlc.UserRoleSuperadmin:
		return sqlc.ClubMemberRoleOwner, nil
	default:
		return "", errForbiddenClub
	}
}

var (
	errUnauthorized  = errors.New("unauthorized")
	errForbiddenClub = errors.New("forbidden club")
)

func (s *Server) handleListClubMembers(w http.ResponseWriter, r *http.Request) {
	if _, err := s.actorClubMembershipRole(r); err != nil {
		if errors.Is(err, errUnauthorized) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
			return
		}
		if errors.Is(err, errForbiddenClub) {
			writeError(w, http.StatusForbidden, "forbidden", "Нужна роль organizer или owner в этом клубе")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось проверить права")
		return
	}
	limit := int32(100)
	offset := int32(0)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = int32(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = int32(n)
		}
	}
	var statusFilter pgtype.Text
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		statusFilter = pgtype.Text{String: raw, Valid: true}
	}

	rows, err := s.store.Queries.ListClubMembers(r.Context(), sqlc.ListClubMembersParams{
		ClubID:      s.currentClubID(r),
		Status:      statusFilter,
		OffsetCount: offset,
		LimitCount:  limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить участников")
		return
	}
	out := make([]clubMemberJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, memberFromListRow(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}

type addClubMemberRequest struct {
	Email  string `json:"email"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

func (s *Server) handleAddClubMember(w http.ResponseWriter, r *http.Request) {
	actorRole, err := s.actorClubMembershipRole(r)
	if err != nil {
		if errors.Is(err, errUnauthorized) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
			return
		}
		if errors.Is(err, errForbiddenClub) {
			writeError(w, http.StatusForbidden, "forbidden", "Нужна роль organizer или owner в этом клубе")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось проверить права")
		return
	}
	var req addClubMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "validation", "Укажите email пользователя")
		return
	}
	role, ok := parseClubMemberRole(req.Role)
	if !ok {
		writeError(w, http.StatusBadRequest, "validation", "Роль: owner, organizer или player")
		return
	}
	if role == sqlc.ClubMemberRoleOwner && actorRole != sqlc.ClubMemberRoleOwner {
		writeError(w, http.StatusForbidden, "forbidden", "Назначить owner может только owner клуба")
		return
	}
	if clubRoleRank(role) > clubRoleRank(actorRole) {
		writeError(w, http.StatusForbidden, "forbidden", "Нельзя назначить роль выше своей")
		return
	}
	status := sqlc.ClubMemberStatusActive
	if raw := strings.TrimSpace(req.Status); raw != "" {
		switch strings.ToLower(raw) {
		case "active":
			status = sqlc.ClubMemberStatusActive
		case "invited":
			status = sqlc.ClubMemberStatusInvited
		default:
			writeError(w, http.StatusBadRequest, "validation", "Статус: active или invited")
			return
		}
	}

	user, err := s.store.Queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user_not_found", "Пользователь с таким email не найден — сначала регистрация")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось найти пользователя")
		return
	}

	clubID := s.currentClubID(r)
	_, getErr := s.store.Queries.GetClubMembership(r.Context(), sqlc.GetClubMembershipParams{
		ClubID: clubID,
		UserID: user.ID,
	})
	if getErr != nil && !errors.Is(getErr, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось проверить membership")
		return
	}

	created := errors.Is(getErr, pgx.ErrNoRows)
	var membershipID pgtype.UUID
	if created {
		row, createErr := s.store.Queries.CreateClubMembership(r.Context(), sqlc.CreateClubMembershipParams{
			ClubID: clubID,
			UserID: user.ID,
			Role:   role,
			Status: status,
		})
		if createErr != nil {
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось добавить участника")
			return
		}
		membershipID = row.ID
	} else {
		row, updErr := s.store.Queries.UpdateClubMembership(r.Context(), sqlc.UpdateClubMembershipParams{
			ClubID: clubID,
			UserID: user.ID,
			Role:   role,
			Status: status,
		})
		if updErr != nil {
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить участника")
			return
		}
		membershipID = row.ID
	}

	out, err := s.store.Queries.GetClubMembershipByID(r.Context(), sqlc.GetClubMembershipByIDParams{
		ID:     membershipID,
		ClubID: clubID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Участник сохранён, но не удалось загрузить")
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	writeJSON(w, code, map[string]any{"member": memberFromGetRow(out)})
}

type patchClubMemberRequest struct {
	Role   string `json:"role"`
	Status string `json:"status"`
}

func (s *Server) handlePatchClubMember(w http.ResponseWriter, r *http.Request) {
	actorRole, err := s.actorClubMembershipRole(r)
	if err != nil {
		if errors.Is(err, errUnauthorized) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
			return
		}
		if errors.Is(err, errForbiddenClub) {
			writeError(w, http.StatusForbidden, "forbidden", "Нужна роль organizer или owner в этом клубе")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось проверить права")
		return
	}
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}
	clubID := s.currentClubID(r)
	row, err := s.store.Queries.GetClubMembershipByID(r.Context(), sqlc.GetClubMembershipByIDParams{
		ID:     id,
		ClubID: clubID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Участник не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить участника")
		return
	}

	var req patchClubMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	role := row.Role
	if strings.TrimSpace(req.Role) != "" {
		parsed, ok := parseClubMemberRole(req.Role)
		if !ok {
			writeError(w, http.StatusBadRequest, "validation", "Роль: owner, organizer или player")
			return
		}
		role = parsed
	}
	status := row.Status
	if raw := strings.TrimSpace(req.Status); raw != "" {
		switch strings.ToLower(raw) {
		case "active":
			status = sqlc.ClubMemberStatusActive
		case "invited":
			status = sqlc.ClubMemberStatusInvited
		case "blocked":
			status = sqlc.ClubMemberStatusBlocked
		case "left":
			status = sqlc.ClubMemberStatusLeft
		default:
			writeError(w, http.StatusBadRequest, "validation", "Статус: active, invited, blocked или left")
			return
		}
	}

	if (role == sqlc.ClubMemberRoleOwner || row.Role == sqlc.ClubMemberRoleOwner) && actorRole != sqlc.ClubMemberRoleOwner {
		writeError(w, http.StatusForbidden, "forbidden", "Менять owner может только owner клуба")
		return
	}
	if clubRoleRank(role) > clubRoleRank(actorRole) {
		writeError(w, http.StatusForbidden, "forbidden", "Нельзя назначить роль выше своей")
		return
	}

	if _, err := s.store.Queries.UpdateClubMembership(r.Context(), sqlc.UpdateClubMembershipParams{
		ClubID: clubID,
		UserID: row.UserID,
		Role:   role,
		Status: status,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить участника")
		return
	}
	updated, err := s.store.Queries.GetClubMembershipByID(r.Context(), sqlc.GetClubMembershipByIDParams{
		ID:     id,
		ClubID: clubID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить участника")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"member": memberFromGetRow(updated)})
}
