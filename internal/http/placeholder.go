package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const inviteTokenTTL = 10 * 365 * 24 * time.Hour

type createPlaceholderRequest struct {
	FirstName string  `json:"first_name"`
	LastName  string  `json:"last_name"`
	Level     string  `json:"level"`
	Phone     *string `json:"phone"`
}

type mergePlaceholderRequest struct {
	SourceUserID string `json:"source_user_id"`
}

// POST /api/organizer/players — create placeholder (no email / no login).
func (s *Server) handleCreatePlaceholderPlayer(w http.ResponseWriter, r *http.Request) {
	organizer := auth.UserFromContext(r.Context())
	if organizer == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}

	var req createPlaceholderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}

	firstName := strings.TrimSpace(req.FirstName)
	lastName := strings.TrimSpace(req.LastName)
	level := strings.TrimSpace(req.Level)
	phone := ""
	if req.Phone != nil {
		phone = strings.TrimSpace(*req.Phone)
	}

	if firstName == "" || lastName == "" {
		writeError(w, http.StatusBadRequest, "validation", "Укажите имя и фамилию")
		return
	}
	if utf8.RuneCountInString(firstName) > 80 || utf8.RuneCountInString(lastName) > 80 {
		writeError(w, http.StatusBadRequest, "validation", "Слишком длинное имя или фамилия")
		return
	}
	if !auth.ValidLevel(level) {
		writeError(w, http.StatusBadRequest, "validation", "Укажите уровень из списка взрослых категорий")
		return
	}

	// Unusable password: random high-entropy secret the player never receives.
	rawSecret, err := crypto.RandomBytes(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать игрока")
		return
	}
	hash, err := crypto.HashPassword(string(rawSecret))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать игрока")
		return
	}

	ctx := r.Context()
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать игрока")
		return
	}
	defer tx.Rollback(ctx)
	q := s.store.Queries.WithTx(tx)

	user, err := q.CreatePlaceholderUser(ctx, sqlc.CreatePlaceholderUserParams{
		PasswordHash: hash,
		FirstName:    firstName,
		LastName:     lastName,
		Role:         sqlc.UserRolePlayer,
		Status:       sqlc.UserStatusApproved,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать игрока")
		return
	}

	clubID := s.currentClubID(r)
	profile, err := q.CreateProfile(ctx, rating.NewProfileParams(
		user.ID, clubID, auth.TextOrNull(phone), level, firstName, lastName,
	))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать игрока")
		return
	}

	// Auto-approve profile so placeholder can enter tournaments/ratings (register gate).
	profile, err = q.ConfirmProfileVerification(ctx, sqlc.ConfirmProfileVerificationParams{
		UserID:             user.ID,
		VerificationStatus: sqlc.ProfileVerificationStatusApproved,
		VerifiedBy:         organizer.ID,
		VerificationNote:   optionalText("organizer_placeholder"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подтвердить профиль игрока")
		return
	}

	if _, err := q.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
		ClubID: clubID,
		UserID: user.ID,
		Role:   sqlc.ClubMemberRolePlayer,
		Status: sqlc.ClubMemberStatusActive,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать игрока")
		return
	}

	inviteRaw, err := crypto.RandomBytes(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать игрока")
		return
	}
	sum := crypto.HashToken(inviteRaw)
	if _, err := q.CreateVerificationToken(ctx, sqlc.CreateVerificationTokenParams{
		UserID:    user.ID,
		Kind:      sqlc.VerificationKindInvite,
		TokenHash: sum[:],
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(inviteTokenTTL), Valid: true},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать игрока")
		return
	}

	diff, _ := json.Marshal(map[string]any{
		"first_name": firstName,
		"last_name":  lastName,
		"level":      level,
		"phone":      phone,
	})
	if _, err := q.InsertAuditLog(ctx, sqlc.InsertAuditLogParams{
		ActorID:   organizer.ID,
		Action:    "player.placeholder_create",
		Entity:    "user",
		EntityID:  pgtype.Text{String: uuidString(user.ID), Valid: true},
		Diff:      diff,
		Ip:        auth.ClientIPAddr(r),
		UserAgent: auth.TextOrNull(r.UserAgent()),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать игрока")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать игрока")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"player": placeholderPlayerJSON(user, profile),
	})
}

// GET /api/organizer/players/placeholders — list unmerged placeholders.
func (s *Server) handleListPlaceholderPlayers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Queries.ListPlaceholderPlayers(r.Context(), s.currentClubID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить заготовки")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"id":                   uuidString(row.ID),
			"first_name":           row.FirstName,
			"last_name":            row.LastName,
			"slug":                 row.Slug,
			"level":                row.Level,
			"phone":                textPtr(row.Phone),
			"verification_status":  string(row.VerificationStatus),
			"rating":               round1(row.Rating),
			"points":               row.Points,
			"user_status":          string(row.UserStatus),
			"is_placeholder":       true,
			"created_at":           formatTS(row.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"players": out})
}

// POST /api/organizer/players/{id}/merge — link placeholder with a real application/account.
// Keeps placeholder id (tournaments/ratings); absorbs email/password from source; deletes source.
func (s *Server) handleMergePlaceholderPlayer(w http.ResponseWriter, r *http.Request) {
	organizer := auth.UserFromContext(r.Context())
	if organizer == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}

	placeholderID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}

	var req mergePlaceholderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	sourceID, err := parseUUID(strings.TrimSpace(req.SourceUserID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Укажите source_user_id заявки")
		return
	}
	if placeholderID.Bytes == sourceID.Bytes {
		writeError(w, http.StatusBadRequest, "validation", "Нельзя объединить игрока с самим собой")
		return
	}

	ctx := r.Context()
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось объединить аккаунты")
		return
	}
	defer tx.Rollback(ctx)
	q := s.store.Queries.WithTx(tx)

	placeholder, err := q.GetUserByID(ctx, placeholderID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Заготовка не найдена")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось объединить аккаунты")
		return
	}
	if placeholder.Email.Valid {
		writeError(w, http.StatusConflict, "not_placeholder", "Игрок уже привязан к аккаунту")
		return
	}

	phProfile, err := q.GetProfileByUserID(ctx, placeholderID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Профиль заготовки не найден")
		return
	}
	if phProfile.ClubID != s.currentClubID(r) {
		writeError(w, http.StatusNotFound, "not_found", "Заготовка не найдена в этом клубе")
		return
	}

	source, err := q.GetUserByID(ctx, sourceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Заявка / аккаунт не найдены")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось объединить аккаунты")
		return
	}
	if !source.Email.Valid || strings.TrimSpace(source.Email.String) == "" {
		writeError(w, http.StatusConflict, "invalid_source", "Источник тоже заготовка — укажите зарегистрированный аккаунт")
		return
	}
	if source.Role != sqlc.UserRolePlayer {
		writeError(w, http.StatusConflict, "invalid_source", "Можно объединять только игроков")
		return
	}
	if source.Status == sqlc.UserStatusRejected {
		writeError(w, http.StatusConflict, "invalid_source", "Отклонённую заявку нельзя объединить")
		return
	}

	srcProfile, err := q.GetProfileByUserID(ctx, sourceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Профиль заявки не найден")
		return
	}
	if srcProfile.ClubID != s.currentClubID(r) {
		writeError(w, http.StatusNotFound, "not_found", "Заявка не найдена в этом клубе")
		return
	}

	// Merge names: keep organizer-filled placeholder values; fill blanks from source.
	firstName := strings.TrimSpace(placeholder.FirstName)
	if firstName == "" {
		firstName = source.FirstName
	}
	lastName := strings.TrimSpace(placeholder.LastName)
	if lastName == "" {
		lastName = source.LastName
	}

	// Reassign competitive FKs from source → placeholder (usually no-ops for fresh applications).
	reassigns := []func() error{
		func() error {
			return q.ReassignRegistrationPlayer(ctx, sqlc.ReassignRegistrationPlayerParams{
				FromUserID: sourceID, ToUserID: placeholderID,
			})
		},
		func() error {
			return q.ReassignRegistrationPartner(ctx, sqlc.ReassignRegistrationPartnerParams{
				FromUserID: sourceID, ToUserID: placeholderID,
			})
		},
		func() error {
			return q.ReassignMatchSidePlayer(ctx, sqlc.ReassignMatchSidePlayerParams{
				FromUserID: sourceID, ToUserID: placeholderID,
			})
		},
		func() error {
			return q.ReassignMatchSidePartner(ctx, sqlc.ReassignMatchSidePartnerParams{
				FromUserID: sourceID, ToUserID: placeholderID,
			})
		},
		func() error {
			return q.ReassignRatingEventPlayer(ctx, sqlc.ReassignRatingEventPlayerParams{
				FromUserID: sourceID, ToUserID: placeholderID,
			})
		},
		func() error {
			return q.ReassignRankingPlayer(ctx, sqlc.ReassignRankingPlayerParams{
				FromUserID: sourceID, ToUserID: placeholderID,
			})
		},
	}
	for _, fn := range reassigns {
		if err := fn(); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				writeError(w, http.StatusConflict, "merge_conflict",
					"Нельзя объединить: оба аккаунта уже участвуют в одном турнире или рейтинге")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось перенести данные заявки")
			return
		}
	}

	if _, err := q.MergeProfileFromSource(ctx, sqlc.MergeProfileFromSourceParams{
		DstUserID: placeholderID,
		SrcUserID: sourceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось объединить профили")
		return
	}

	claimed, err := q.ClaimPlaceholderFromUser(ctx, sqlc.ClaimPlaceholderFromUserParams{
		ID:              placeholderID,
		Email:           source.Email,
		PasswordHash:    source.PasswordHash,
		EmailVerifiedAt: source.EmailVerifiedAt,
		FirstName:       firstName,
		LastName:        lastName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "not_placeholder", "Игрок уже привязан к аккаунту")
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "email_taken", "Email заявки уже занят другим аккаунтом")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось перенести учётные данные")
		return
	}

	_ = q.MarkInviteTokensUsed(ctx, placeholderID)

	if err := q.DeleteUserByID(ctx, sourceID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось удалить исходную заявку")
		return
	}

	diff, _ := json.Marshal(map[string]any{
		"placeholder_id": uuidString(placeholderID),
		"source_user_id": uuidString(sourceID),
		"email":          emailString(source.Email),
		"first_name":     firstName,
		"last_name":      lastName,
	})
	if _, err := q.InsertAuditLog(ctx, sqlc.InsertAuditLogParams{
		ActorID:   organizer.ID,
		Action:    "player.placeholder_merge",
		Entity:    "user",
		EntityID:  pgtype.Text{String: uuidString(placeholderID), Valid: true},
		Diff:      diff,
		Ip:        auth.ClientIPAddr(r),
		UserAgent: auth.TextOrNull(r.UserAgent()),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось записать аудит")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось объединить аккаунты")
		return
	}

	profile, _ := s.store.Queries.GetProfileByUserID(ctx, placeholderID)
	writeJSON(w, http.StatusOK, map[string]any{
		"player": placeholderPlayerJSON(claimed, profile),
		"merged": map[string]any{
			"source_user_id": uuidString(sourceID),
			"kept_user_id":   uuidString(placeholderID),
		},
	})
}

func placeholderPlayerJSON(user sqlc.User, profile sqlc.Profile) map[string]any {
	return map[string]any{
		"id":                  uuidString(user.ID),
		"email":               emailString(user.Email),
		"first_name":          user.FirstName,
		"last_name":           user.LastName,
		"slug":                profile.Slug,
		"level":               profile.Level,
		"category":            profile.Level,
		"phone":               textPtr(profile.Phone),
		"status":              profile.PlayerStatus,
		"user_status":         string(user.Status),
		"verification_status": string(profile.VerificationStatus),
		"rating":              round1(profile.Rating),
		"points":              profile.Points,
		"is_placeholder":      !user.Email.Valid,
		"member_since":        formatTS(profile.CreatedAt),
	}
}
