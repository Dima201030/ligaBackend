package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/limits"
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
		Email:      emailString(row.Email),
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
		Email:      emailString(row.Email),
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

var (
	errUnauthorized  = errors.New("unauthorized")
	errForbiddenClub = errors.New("forbidden club")
)

func (s *Server) handleListClubMembers(w http.ResponseWriter, r *http.Request) {
	if _, err := s.requireClubOwnerRole(r); s.writeOwnerRoleError(w, err) {
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
	rawStatus := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	includeLeft := rawStatus == "left" || rawStatus == "all"
	var statusFilter pgtype.Text
	switch rawStatus {
	case "", "all":
		// default / all: query without status predicate; left filtered below unless all
	case "active", "invited", "blocked", "left":
		statusFilter = pgtype.Text{String: rawStatus, Valid: true}
	default:
		writeError(w, http.StatusBadRequest, "validation", "Статус: active, invited, blocked, left или all")
		return
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
		// Soft-removed memberships (status=left) stay in DB but are hidden from the
		// default organizer list so DELETE feels like a real removal.
		if !includeLeft && row.Status == sqlc.ClubMemberStatusLeft {
			continue
		}
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
	actorRole, err := s.requireClubOwnerRole(r)
	if s.writeOwnerRoleError(w, err) {
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

	user, err := s.store.Queries.GetUserByEmail(r.Context(), textEmail(email))
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
		since := time.Now().UTC().Add(-24 * time.Hour)
		inviteCount, countErr := s.store.Queries.CountClubMembershipsCreatedSince(r.Context(), sqlc.CountClubMembershipsCreatedSinceParams{
			ClubID:    clubID,
			CreatedAt: pgtype.Timestamptz{Time: since, Valid: true},
		})
		if countErr != nil {
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось добавить участника")
			return
		}
		if inviteCount >= limits.MaxClubInvitesPerDay {
			writeError(w, http.StatusTooManyRequests, "rate_limited",
				"Слишком много приглашений за сутки — максимум "+strconv.Itoa(limits.MaxClubInvitesPerDay))
			return
		}
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
	if status == sqlc.ClubMemberStatusActive {
		s.activateClubPlayerProfile(r, out.UserID)
	}
	s.auditMemberRoleChange(r, out.UserID, string(role), string(status), created)
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
	actorRole, err := s.requireClubOwnerRole(r)
	if s.writeOwnerRoleError(w, err) {
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
	losingOwner := row.Role == sqlc.ClubMemberRoleOwner && row.Status == sqlc.ClubMemberStatusActive &&
		(role != sqlc.ClubMemberRoleOwner || status != sqlc.ClubMemberStatusActive)
	if losingOwner {
		if err := s.ensureNotLastActiveOwner(r, clubID); err != nil {
			if errors.Is(err, errLastOwner) {
				writeError(w, http.StatusConflict, "last_owner", "Нельзя снять последнего owner клуба")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось проверить владельцев")
			return
		}
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
	switch status {
	case sqlc.ClubMemberStatusLeft, sqlc.ClubMemberStatusBlocked:
		s.archiveClubPlayerProfile(r, row.UserID)
	case sqlc.ClubMemberStatusActive:
		s.activateClubPlayerProfile(r, row.UserID)
	}
	updated, err := s.store.Queries.GetClubMembershipByID(r.Context(), sqlc.GetClubMembershipByIDParams{
		ID:     id,
		ClubID: clubID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить участника")
		return
	}
	s.auditMemberRoleChange(r, updated.UserID, string(role), string(status), false)
	writeJSON(w, http.StatusOK, map[string]any{"member": memberFromGetRow(updated)})
}

func (s *Server) auditMemberRoleChange(r *http.Request, targetUserID pgtype.UUID, role, status string, created bool) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil || s.store == nil {
		return
	}
	action := "membership.role_update"
	if created {
		action = "membership.role_assign"
	}
	diff, _ := json.Marshal(map[string]any{
		"club_id": uuidString(s.currentClubID(r)),
		"role":    role,
		"status":  status,
	})
	_, _ = s.store.Queries.InsertAuditLog(r.Context(), sqlc.InsertAuditLogParams{
		ActorID:   actor.ID,
		Action:    action,
		Entity:    "club_membership",
		EntityID:  pgtype.Text{String: uuidString(targetUserID), Valid: true},
		Diff:      diff,
		Ip:        auth.ClientIPAddr(r),
		UserAgent: auth.TextOrNull(r.UserAgent()),
	})
}

var errLastOwner = errors.New("last owner")

func (s *Server) ensureNotLastActiveOwner(r *http.Request, clubID pgtype.UUID) error {
	n, err := s.store.Queries.CountActiveClubOwners(r.Context(), clubID)
	if err != nil {
		return err
	}
	if n <= 1 {
		return errLastOwner
	}
	return nil
}

// archiveClubPlayerProfile hides the user from public players / live ranking for the club.
func (s *Server) archiveClubPlayerProfile(r *http.Request, userID pgtype.UUID) {
	if s.store == nil {
		return
	}
	_, _ = s.store.Queries.SetProfilePlayerStatus(r.Context(), sqlc.SetProfilePlayerStatusParams{
		UserID:       userID,
		PlayerStatus: "archived",
	})
}

func (s *Server) activateClubPlayerProfile(r *http.Request, userID pgtype.UUID) {
	if s.store == nil {
		return
	}
	_, _ = s.store.Queries.SetProfilePlayerStatus(r.Context(), sqlc.SetProfilePlayerStatusParams{
		UserID:       userID,
		PlayerStatus: "active",
	})
}

// handleDeleteClubMember soft-leaves club membership and, for player accounts, erases
// the Pairly account (anonymize → «Удалённый аккаунт», hide from ranking/players,
// cancel registrations). Owner-only; last active owner cannot be removed.
func (s *Server) handleDeleteClubMember(w http.ResponseWriter, r *http.Request) {
	actorRole, err := s.requireClubOwnerRole(r)
	if s.writeOwnerRoleError(w, err) {
		return
	}
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}
	clubID := s.currentClubID(r)
	ctx := r.Context()
	row, err := s.store.Queries.GetClubMembershipByID(ctx, sqlc.GetClubMembershipByIDParams{
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
	if row.Status == sqlc.ClubMemberStatusLeft {
		s.archiveClubPlayerProfile(r, row.UserID)
		writeJSON(w, http.StatusOK, map[string]any{"member": memberFromGetRow(row)})
		return
	}
	if row.Role == sqlc.ClubMemberRoleOwner && actorRole != sqlc.ClubMemberRoleOwner {
		writeError(w, http.StatusForbidden, "forbidden", "Удалить owner может только owner клуба")
		return
	}
	if clubRoleRank(row.Role) > clubRoleRank(actorRole) {
		writeError(w, http.StatusForbidden, "forbidden", "Нельзя удалить участника с ролью выше своей")
		return
	}
	if row.Role == sqlc.ClubMemberRoleOwner && row.Status == sqlc.ClubMemberStatusActive {
		if err := s.ensureNotLastActiveOwner(r, clubID); err != nil {
			if errors.Is(err, errLastOwner) {
				writeError(w, http.StatusConflict, "last_owner", "Нельзя удалить последнего owner клуба")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось проверить владельцев")
			return
		}
	}

	target, err := s.store.Queries.GetUserByID(ctx, row.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить участника")
		return
	}
	eraseAccount := !auth.IsOrganizerOrAbove(target.Role)

	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить участника")
		return
	}
	defer tx.Rollback(ctx)
	q := s.store.Queries.WithTx(tx)

	if _, err := q.UpdateClubMembership(ctx, sqlc.UpdateClubMembershipParams{
		ClubID: clubID,
		UserID: row.UserID,
		Role:   row.Role,
		Status: sqlc.ClubMemberStatusLeft,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить участника")
		return
	}
	if eraseAccount {
		if err := s.erasePlayerAccount(ctx, q, r, row.UserID, true); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить аккаунт")
			return
		}
	} else {
		if _, err := q.SetProfilePlayerStatus(ctx, sqlc.SetProfilePlayerStatusParams{
			UserID:       row.UserID,
			PlayerStatus: "archived",
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось скрыть профиль")
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить участника")
		return
	}

	updated, err := s.store.Queries.GetClubMembershipByID(ctx, sqlc.GetClubMembershipByIDParams{
		ID:     id,
		ClubID: clubID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить участника")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"member":        memberFromGetRow(updated),
		"account_erased": eraseAccount,
	})
}
