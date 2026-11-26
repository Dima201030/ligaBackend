package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/clubs"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/mail"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/Dima201030/ligaBackend/internal/tournaments"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	defaultVenueName    = "Площадка организатора"
	defaultVenueAddress = ""
	defaultVenueCourts  = 1
)

type tournamentWriteRequest struct {
	Title                string  `json:"title"`
	Slug                 string  `json:"slug"`
	Description          string  `json:"description"`
	Type                 string  `json:"type"`
	Category             string  `json:"category"`
	Surface              string  `json:"surface"`
	Format               string  `json:"format"`
	Status               string  `json:"status"`
	StartsAt             string  `json:"starts_at"`
	EndsAt               *string `json:"ends_at"`
	RegistrationOpensAt  string  `json:"registration_opens_at"`
	RegistrationClosesAt string  `json:"registration_closes_at"`
	VenueName            string  `json:"venue_name"`
	VenueAddress         string  `json:"venue_address"`
	VenueCourts          *int32  `json:"venue_courts"`
	VenueSurface         string  `json:"venue_surface"`
	EntryFee             *int32  `json:"entry_fee"`
	MaxParticipants      *int32  `json:"max_participants"`
	CoverURL             *string `json:"cover_url"`
	Regulations          string  `json:"regulations"`
	Season               *int32  `json:"season"`
}

type createRegistrationRequest struct {
	PartnerID         *string `json:"partner_id"`
	LookingForPartner bool    `json:"looking_for_partner"`
}

type organizerAddRegistrationRequest struct {
	PlayerID          string  `json:"player_id"`
	PartnerID         *string `json:"partner_id"`
	LookingForPartner bool    `json:"looking_for_partner"`
}

type updateRegistrationStatusRequest struct {
	Status string `json:"status"`
}

func (s *Server) handleListPublicTournaments(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	var year int
	if y := strings.TrimSpace(r.URL.Query().Get("year")); y != "" {
		year, _ = strconv.Atoi(y)
	}
	scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	if scope == "platform" || r.URL.Query().Get("aggregate") == "1" {
		s.writePlatformPublicTournaments(w, r, statusFilter, category, typeFilter, year, query)
		return
	}

	rows, err := s.store.Queries.ListPublicTournaments(r.Context(), s.currentClubID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить турниры")
		return
	}
	s.ensureTournamentsCreatedBy(r.Context(), rows)

	hidden, _ := s.hiddenTournamentIDs(r.Context())
	places, _ := s.allTournamentPlaces(r.Context(), s.currentClubID(r))
	clubSlug, clubName := s.currentClubMeta(r)
	out := make([]tournamentJSON, 0, len(rows))
	for _, row := range rows {
		if hidden[uuidString(row.ID)] {
			continue
		}
		if !matchPublicFilters(row, statusFilter, category, typeFilter, year, query) {
			continue
		}
		regs, _ := s.latestRegulations(r, row.ID)
		tj := withClubMeta(tournamentFromRow(row, regs), clubSlug, clubName)
		tj.Winners = places[uuidString(row.ID)]
		tj.Venue = publicVenue(tj.Venue)
		out = append(out, tj)
	}
	s.attachTournamentCreators(r.Context(), out)
	writeJSON(w, http.StatusOK, map[string]any{"tournaments": out})
}

func (s *Server) writePlatformPublicTournaments(
	w http.ResponseWriter,
	r *http.Request,
	statusFilter, category, typeFilter string,
	year int,
	query string,
) {
	rows, err := s.store.Queries.ListPlatformPublicTournaments(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить турниры")
		return
	}
	s.ensureCreatedByOnPlatformRows(r.Context(), rows)
	hidden, _ := s.hiddenTournamentIDsAll(r.Context())
	out := make([]tournamentJSON, 0, len(rows))
	for _, row := range rows {
		if hidden[uuidString(row.ID)] {
			continue
		}
		tour := platformRowToTournament(row)
		if !matchPublicFilters(tour, statusFilter, category, typeFilter, year, query) {
			continue
		}
		regs, _ := s.latestRegulations(r, row.ID)
		tj := withClubMeta(tournamentFromRow(tour, regs), row.ClubSlug, row.ClubName)
		tj.Venue = publicVenue(tj.Venue)
		out = append(out, tj)
	}
	s.attachTournamentCreators(r.Context(), out)
	writeJSON(w, http.StatusOK, map[string]any{"tournaments": out, "scope": "platform"})
}

func platformRowToTournament(row sqlc.ListPlatformPublicTournamentsRow) sqlc.Tournament {
	return sqlc.Tournament{
		ID:                   row.ID,
		ClubID:               row.ClubID,
		Slug:                 row.Slug,
		Title:                row.Title,
		Description:          row.Description,
		Type:                 row.Type,
		Category:             row.Category,
		Surface:              row.Surface,
		Format:               row.Format,
		Status:               row.Status,
		StartsAt:             row.StartsAt,
		EndsAt:               row.EndsAt,
		RegistrationOpensAt:  row.RegistrationOpensAt,
		RegistrationClosesAt: row.RegistrationClosesAt,
		VenueName:            row.VenueName,
		VenueAddress:         row.VenueAddress,
		VenueCourts:          row.VenueCourts,
		VenueSurface:         row.VenueSurface,
		EntryFee:             row.EntryFee,
		MaxParticipants:      row.MaxParticipants,
		CoverUrl:             row.CoverUrl,
		CoverFileID:          row.CoverFileID,
		Season:               row.Season,
		CreatedBy:            row.CreatedBy,
		BracketStatus:        row.BracketStatus,
		BracketPublishedAt:   row.BracketPublishedAt,
		BracketVersion:       row.BracketVersion,
		BracketThirdPlace:    row.BracketThirdPlace,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
}

func (s *Server) currentClubMeta(r *http.Request) (slug, name string) {
	if c := clubs.FromContext(r.Context()); c != nil {
		return c.Slug, c.Name
	}
	return clubs.DefaultSlug, ""
}

func matchPublicFilters(t sqlc.Tournament, status, category, typeFilter string, year int, query string) bool {
	switch status {
	case "", "upcoming":
		switch t.Status {
		case sqlc.TournamentStatusRegistrationSoon,
			sqlc.TournamentStatusRegistrationOpen,
			sqlc.TournamentStatusRegistrationClosed,
			sqlc.TournamentStatusScheduled,
			sqlc.TournamentStatusLive:
		default:
			return false
		}
	case "all":
		// no status filter
	default:
		if string(t.Status) != status {
			return false
		}
	}
	if category != "" && t.Category != category {
		return false
	}
	if typeFilter != "" && string(t.Type) != typeFilter {
		return false
	}
	if year > 0 && int(t.Season) != year {
		return false
	}
	if query != "" && !strings.Contains(strings.ToLower(t.Title), strings.ToLower(query)) {
		return false
	}
	return true
}

func (s *Server) handleGetPublicTournament(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		writeError(w, http.StatusBadRequest, "validation", "Укажите slug турнира")
		return
	}
	tour, err := s.store.Queries.GetTournamentBySlug(r.Context(), sqlc.GetTournamentBySlugParams{ClubID: s.currentClubID(r), Slug: slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить турнир")
		return
	}
	hidden, _ := s.isTournamentHidden(r.Context(), tour.ID)
	if tour.Status == sqlc.TournamentStatusDraft || hidden {
		user := auth.UserFromContext(r.Context())
		if user == nil || !auth.IsOrganizerOrAbove(user.Role) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
	}
	s.ensureTournamentCreatedBy(r.Context(), &tour)
	regsText, _ := s.latestRegulations(r, tour.ID)
	regs, err := s.store.Queries.ListRegistrationsByTournament(r.Context(), tour.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить заявки")
		return
	}
	list := make([]registrationJSON, 0, len(regs))
	for _, row := range regs {
		list = append(list, registrationFromListRow(row))
	}
	clubSlug, clubName := s.currentClubMeta(r)
	tj := s.withTournamentCreator(r.Context(), withClubMeta(tournamentFromRow(tour, regsText), clubSlug, clubName))
	tj.Venue = publicVenue(tj.Venue)
	tj.Winners, _ = s.listTournamentPlaces(r.Context(), s.currentClubID(r), tour.ID)
	images, err := s.listTournamentImages(r, tour.ID)
	if err != nil {
		images = []tournamentImageJSON{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tournament":    tj,
		"registrations": list,
		"images":        images,
	})
}

func (s *Server) handleListOrganizerTournaments(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Queries.ListOrganizerTournaments(r.Context(), s.currentClubID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить турниры")
		return
	}
	s.ensureTournamentsCreatedBy(r.Context(), rows)
	hidden, _ := s.hiddenTournamentIDs(r.Context())
	places, _ := s.allTournamentPlaces(r.Context(), s.currentClubID(r))
	out := make([]tournamentJSON, 0, len(rows))
	for _, row := range rows {
		regs, _ := s.latestRegulations(r, row.ID)
		tj := tournamentFromRow(row, regs)
		tj.Hidden = hidden[uuidString(row.ID)]
		tj.Winners = places[uuidString(row.ID)]
		counts := s.registrationCounts(r, row.ID)
		tj.ConfirmedCount = &counts.confirmed
		tj.WaitlistCount = &counts.waitlist
		out = append(out, tj)
	}
	s.attachTournamentCreators(r.Context(), out)
	writeJSON(w, http.StatusOK, map[string]any{"tournaments": out})
}

func (s *Server) handleGetOrganizerTournament(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}
	tour, err := s.loadClubTournament(r, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить турнир")
		return
	}
	s.ensureTournamentCreatedBy(r.Context(), &tour)
	regsText, _ := s.latestRegulations(r, tour.ID)
	regs, err := s.store.Queries.ListOrganizerRegistrationsByTournament(r.Context(), tour.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить заявки")
		return
	}
	list := make([]registrationJSON, 0, len(regs))
	var confirmed, pending, waitlist int32
	for _, row := range regs {
		list = append(list, registrationFromOrganizerListRow(row))
		switch row.Status {
		case sqlc.RegistrationStatusConfirmed:
			confirmed++
		case sqlc.RegistrationStatusPending:
			pending++
		case sqlc.RegistrationStatusWaitlist:
			waitlist++
		}
	}
	tj := s.withTournamentCreator(r.Context(), tournamentFromRow(tour, regsText))
	tj.ConfirmedCount = &confirmed
	tj.WaitlistCount = &waitlist
	tj.Hidden, _ = s.isTournamentHidden(r.Context(), tour.ID)
	tj.Winners, _ = s.listTournamentPlaces(r.Context(), s.currentClubID(r), tour.ID)
	s.attachPlacesSuggestion(r.Context(), tour, &tj)
	images, err := s.listTournamentImages(r, tour.ID)
	if err != nil {
		images = []tournamentImageJSON{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tournament":    tj,
		"registrations": list,
		"images":        images,
		"counts": map[string]int32{
			"confirmed": confirmed,
			"pending":   pending,
			"waitlist":  waitlist,
		},
	})
}

func (s *Server) handleOrganizerAddRegistration(w http.ResponseWriter, r *http.Request) {
	tournamentID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор турнира")
		return
	}
	tour, err := s.loadClubTournament(r, tournamentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить турнир")
		return
	}

	var req organizerAddRegistrationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	playerID, err := parseUUID(strings.TrimSpace(req.PlayerID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Укажите игрока")
		return
	}

	ctx := r.Context()
	clubID := s.currentClubID(r)
	membership, err := s.store.Queries.GetClubMembership(ctx, sqlc.GetClubMembershipParams{
		ClubID: clubID,
		UserID: playerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "player_not_found", "Игрок не найден в клубе")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось добавить участника")
		return
	}
	if membership.Status != sqlc.ClubMemberStatusActive {
		writeError(w, http.StatusConflict, "player_inactive", "Игрок не состоит в клубе как активный участник")
		return
	}

	user, err := s.store.Queries.GetUserByID(ctx, playerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "player_not_found", "Игрок не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось добавить участника")
		return
	}
	if user.Status != sqlc.UserStatusApproved {
		writeError(w, http.StatusConflict, "player_not_approved", "Можно добавить только подтверждённого игрока или заготовку")
		return
	}
	if user.Role != sqlc.UserRolePlayer && user.Role != sqlc.UserRoleOrganizer {
		writeError(w, http.StatusConflict, "player_role", "В турнир можно добавить только игрока")
		return
	}

	lookingForPartner := req.LookingForPartner
	var partnerID pgtype.UUID
	if req.PartnerID != nil && strings.TrimSpace(*req.PartnerID) != "" {
		partnerID, err = parseUUID(strings.TrimSpace(*req.PartnerID))
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор партнёра")
			return
		}
	} else if tournaments.IsPairType(tour.Type) && !lookingForPartner {
		// Organizer singles-path helper for pair events: seat as looking for partner.
		lookingForPartner = true
	}

	reg, err := tournaments.CreateRegistration(ctx, s.store, tournaments.CreateRegistrationInput{
		TournamentID:      tour.ID,
		PlayerID:          playerID,
		PartnerID:         partnerID,
		LookingForPartner: lookingForPartner,
		SkipOpenCheck:     true,
	})
	if err != nil {
		switch {
		case errors.Is(err, tournaments.ErrRegistrationClosed):
			writeError(w, http.StatusConflict, "registration_closed", "Нельзя добавить участника в завершённый турнир")
		case errors.Is(err, tournaments.ErrAlreadyRegistered):
			writeError(w, http.StatusConflict, "already_registered", "Игрок уже заявлен на этот турнир")
		case errors.Is(err, tournaments.ErrSelfPartner):
			writeError(w, http.StatusBadRequest, "validation", "Нельзя указать себя в качестве партнёра")
		case errors.Is(err, tournaments.ErrPartnerRequired):
			writeError(w, http.StatusBadRequest, "validation", "Для парного турнира укажите партнёра или отметьте «ищу партнёра»")
		case errors.Is(err, tournaments.ErrPartnerNotFound):
			writeError(w, http.StatusBadRequest, "validation", "Партнёр не найден или не подтверждён")
		case errors.Is(err, tournaments.ErrPartnerBusy):
			writeError(w, http.StatusConflict, "partner_busy", "Партнёр уже заявлен на этот турнир")
		case errors.Is(err, tournaments.ErrInvalidPairFlags):
			writeError(w, http.StatusBadRequest, "validation", "Некорректные параметры парной заявки")
		case errors.Is(err, tournaments.ErrTournamentNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
		default:
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось добавить участника")
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"registration": registrationFromModel(reg)})
}

func (s *Server) handleUpdateOrganizerRegistrationStatus(w http.ResponseWriter, r *http.Request) {
	tournamentID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор турнира")
		return
	}
	regID, err := parseUUID(chi.URLParam(r, "regId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор заявки")
		return
	}
	tour, err := s.loadClubTournament(r, tournamentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить турнир")
		return
	}

	var req updateRegistrationStatusRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		writeError(w, http.StatusBadRequest, "validation", "Укажите статус заявки")
		return
	}

	ctx := r.Context()
	before, err := s.store.Queries.GetRegistrationByID(ctx, regID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Заявка не найдена")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить заявку")
		return
	}

	reg, promoted, err := tournaments.UpdateRegistrationStatus(ctx, s.store, tournamentID, regID, status)
	if err != nil {
		switch {
		case errors.Is(err, tournaments.ErrInvalidStatus):
			writeError(w, http.StatusBadRequest, "validation", "Недопустимый статус заявки")
		case errors.Is(err, tournaments.ErrNotRegistered), errors.Is(err, tournaments.ErrRegistrationMismatch):
			writeError(w, http.StatusNotFound, "not_found", "Заявка не найдена")
		case errors.Is(err, tournaments.ErrTournamentNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
		case errors.Is(err, tournaments.ErrCapacityFull):
			writeError(w, http.StatusConflict, "capacity_full", "Нет свободных мест в основном составе")
		default:
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить статус заявки")
		}
		return
	}

	if before.Status != reg.Status {
		s.enqueueTournamentRegistrationEmail(ctx, tour, reg)
	}
	if promoted != nil {
		s.enqueueTournamentRegistrationEmail(ctx, tour, *promoted)
	}

	writeJSON(w, http.StatusOK, map[string]any{"registration": registrationFromModel(reg)})
}

// enqueueTournamentRegistrationEmail queues confirm/reject/waitlist mail for a
// tournament registration when the player's notification_prefs.tournaments is on
// (default true). Disabled prefs skip outbox entirely.
func (s *Server) enqueueTournamentRegistrationEmail(ctx context.Context, tour sqlc.Tournament, reg sqlc.Registration) {
	template, ok := mail.TournamentRegistrationTemplate(string(reg.Status))
	if !ok {
		return
	}
	var prefsRaw []byte
	if prof, err := s.store.Queries.GetProfileByUserID(ctx, reg.PlayerID); err == nil {
		prefsRaw = prof.NotificationPrefs
	} else if !errors.Is(err, pgx.ErrNoRows) {
		log.Printf("tournament registration email: load prefs: %v", err)
	}
	if !allowsTournamentRegistrationEmails(prefsRaw) {
		log.Printf("tournament registration email: skipped template=%s (tournaments prefs off)", template)
		return
	}
	user, err := s.store.Queries.GetUserByID(ctx, reg.PlayerID)
	if err != nil {
		log.Printf("tournament registration email: load user: %v", err)
		return
	}
	if err := mail.Enqueue(ctx, s.store, s.cfg, emailString(user.Email), template, map[string]any{
		"first_name":       user.FirstName,
		"last_name":        user.LastName,
		"tournament_title": tour.Title,
		"tournament_slug":  tour.Slug,
		"status":           string(reg.Status),
	}); err != nil {
		log.Printf("tournament registration email: enqueue template=%s: %v", template, err)
	}
}

func (s *Server) handleCreateTournament(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	var req tournamentWriteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	parsed, msg := parseTournamentWrite(req, true)
	if msg != "" {
		writeError(w, http.StatusBadRequest, "validation", msg)
		return
	}

	ctx := r.Context()
	slug := parsed.slug
	if slug == "" {
		slug = tournaments.Slugify(parsed.title)
	}
	base := slug
	for i := 0; i < 20; i++ {
		exists, err := s.store.Queries.SlugExists(ctx, sqlc.SlugExistsParams{ClubID: s.currentClubID(r), Slug: slug})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать турнир")
			return
		}
		if !exists {
			break
		}
		slug = base + "-" + strconv.Itoa(i+2)
	}

	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать турнир")
		return
	}
	defer tx.Rollback(ctx)
	q := s.store.Queries.WithTx(tx)

	tour, err := q.CreateTournament(ctx, sqlc.CreateTournamentParams{

		ClubID: s.currentClubID(r), Slug: slug,
		Title:                parsed.title,
		Description:          parsed.description,
		Type:                 parsed.typ,
		Category:             parsed.category,
		Surface:              parsed.surface,
		Format:               parsed.format,
		Status:               parsed.status,
		StartsAt:             parsed.startsAt,
		EndsAt:               parsed.endsAt,
		RegistrationOpensAt:  parsed.opensAt,
		RegistrationClosesAt: parsed.closesAt,
		VenueName:            parsed.venueName,
		VenueAddress:         parsed.venueAddress,
		VenueCourts:          parsed.venueCourts,
		VenueSurface:         parsed.venueSurface,
		EntryFee:             parsed.entryFee,
		MaxParticipants:      parsed.maxParticipants,
		CoverUrl:             parsed.coverURL,
		Season:               parsed.season,
		CreatedBy:            actor.ID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "slug_taken", "Турнир с таким slug уже существует")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать турнир")
		return
	}

	_, err = q.CreateTournamentRuleVersion(ctx, sqlc.CreateTournamentRuleVersionParams{
		TournamentID: tour.ID,
		Version:      1,
		Rules:        []byte(`{}`),
		TextMarkdown: parsed.regulations,
		CreatedBy:    actor.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить регламент")
		return
	}

	diff, _ := json.Marshal(map[string]any{"after": map[string]any{"slug": tour.Slug, "title": tour.Title, "status": tour.Status}})
	_, err = q.InsertAuditLog(ctx, sqlc.InsertAuditLogParams{
		ActorID:   actor.ID,
		Action:    "tournament.create",
		Entity:    "tournament",
		EntityID:  pgtype.Text{String: uuidString(tour.ID), Valid: true},
		Diff:      diff,
		Ip:        auth.ClientIPAddr(r),
		UserAgent: auth.TextOrNull(r.UserAgent()),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать турнир")
		return
	}

	var awarded int
	if tour.Status == sqlc.TournamentStatusCompleted {
		n, err := rating.ApplyTournamentPointsTx(ctx, q, rating.TournamentApplyInput{
			TournamentID: tour.ID,
			Season:       tour.Season,
			Category:     tour.Category,
			Type:         string(tour.Type),
			ActorID:      actor.ID,
		})
		if err != nil {
			if errors.Is(err, rating.ErrConfigNotFound) {
				writeError(w, http.StatusConflict, "config_missing", "Нет конфигурации рейтинга для сезона — турнир не создан")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось начислить очки рейтинга")
			return
		}
		awarded = n
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось создать турнир")
		return
	}

	resp := map[string]any{"tournament": s.withTournamentCreator(r.Context(), tournamentFromRow(tour, parsed.regulations))}
	if tour.Status == sqlc.TournamentStatusCompleted {
		resp["ratings_applied"] = awarded
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleUpdateTournament(w http.ResponseWriter, r *http.Request) {
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
	var req tournamentWriteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}

	ctx := r.Context()
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить турнир")
		return
	}
	defer tx.Rollback(ctx)
	q := s.store.Queries.WithTx(tx)

	before, err := q.GetTournamentByClubAndID(ctx, sqlc.GetTournamentByClubAndIDParams{
		ClubID: s.currentClubID(r),
		ID:     id,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить турнир")
		return
	}

	merged := mergeTournamentWrite(req, before)
	parsed, msg := parseTournamentWrite(merged, false)
	if msg != "" {
		writeError(w, http.StatusBadRequest, "validation", msg)
		return
	}

	tour, err := q.UpdateTournament(ctx, sqlc.UpdateTournamentParams{
		ID:                   id,
		Title:                parsed.title,
		Description:          parsed.description,
		Type:                 parsed.typ,
		Category:             parsed.category,
		Surface:              parsed.surface,
		Format:               parsed.format,
		Status:               parsed.status,
		StartsAt:             parsed.startsAt,
		EndsAt:               parsed.endsAt,
		RegistrationOpensAt:  parsed.opensAt,
		RegistrationClosesAt: parsed.closesAt,
		VenueName:            parsed.venueName,
		VenueAddress:         parsed.venueAddress,
		VenueCourts:          parsed.venueCourts,
		VenueSurface:         parsed.venueSurface,
		EntryFee:             parsed.entryFee,
		MaxParticipants:      parsed.maxParticipants,
		CoverUrl:             parsed.coverURL,
		Season:               parsed.season,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить турнир")
		return
	}

	if strings.TrimSpace(req.Regulations) != "" {
		next, err := q.NextRuleVersion(ctx, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить регламент")
			return
		}
		_, err = q.CreateTournamentRuleVersion(ctx, sqlc.CreateTournamentRuleVersionParams{
			TournamentID: id,
			Version:      next,
			Rules:        []byte(`{}`),
			TextMarkdown: parsed.regulations,
			CreatedBy:    actor.ID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить регламент")
			return
		}
	}

	diff, _ := json.Marshal(map[string]any{
		"before": map[string]any{"status": before.Status, "title": before.Title},
		"after":  map[string]any{"status": tour.Status, "title": tour.Title},
	})
	_, err = q.InsertAuditLog(ctx, sqlc.InsertAuditLogParams{
		ActorID:   actor.ID,
		Action:    "tournament.update",
		Entity:    "tournament",
		EntityID:  pgtype.Text{String: uuidString(id), Valid: true},
		Diff:      diff,
		Ip:        auth.ClientIPAddr(r),
		UserAgent: auth.TextOrNull(r.UserAgent()),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить турнир")
		return
	}

	var awarded int
	if before.Status != sqlc.TournamentStatusCompleted && tour.Status == sqlc.TournamentStatusCompleted {
		n, err := rating.ApplyTournamentPointsTx(ctx, q, rating.TournamentApplyInput{
			TournamentID: tour.ID,
			Season:       tour.Season,
			Category:     tour.Category,
			Type:         string(tour.Type),
			ActorID:      actor.ID,
		})
		if err != nil {
			if errors.Is(err, rating.ErrConfigNotFound) {
				writeError(w, http.StatusConflict, "config_missing", "Нет конфигурации рейтинга для сезона — статус не сохранён")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось начислить очки рейтинга")
			return
		}
		awarded = n
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить турнир")
		return
	}

	s.ensureTournamentCreatedBy(r.Context(), &tour)
	regs, _ := s.latestRegulations(r, tour.ID)
	resp := map[string]any{"tournament": s.withTournamentCreator(r.Context(), tournamentFromRow(tour, regs))}
	if before.Status != sqlc.TournamentStatusCompleted && tour.Status == sqlc.TournamentStatusCompleted {
		resp["ratings_applied"] = awarded
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateRegistration(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	// Defense in depth: middleware already enforces session + approved + email + profile verified.
	if user.Status != sqlc.UserStatusApproved {
		writeError(w, http.StatusForbidden, "forbidden", "Заявка доступна только подтверждённым игрокам")
		return
	}
	if !emailAllowsTournamentRegistration(user.EmailVerifiedAt) {
		writeError(w, http.StatusForbidden, "email_unverified", "Подтвердите email по ссылке из письма, затем запишитесь на турнир")
		return
	}
	prof, err := s.store.Queries.GetProfileByUserID(r.Context(), user.ID)
	if err != nil || !profileAllowsTournamentRegistration(prof.VerificationStatus) {
		writeError(w, http.StatusForbidden, "profile_unverified", "Запись на турнир откроется после подтверждения профиля организатором")
		return
	}

	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	tour, err := s.store.Queries.GetTournamentBySlug(r.Context(), sqlc.GetTournamentBySlugParams{ClubID: s.currentClubID(r), Slug: slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подать заявку")
		return
	}

	var req createRegistrationRequest
	if err := decodeJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}

	var partnerID pgtype.UUID
	if req.PartnerID != nil && strings.TrimSpace(*req.PartnerID) != "" {
		partnerID, err = parseUUID(strings.TrimSpace(*req.PartnerID))
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор партнёра")
			return
		}
	}

	// Silent auto-join: organizer tooling expects club membership; player UX has no club switcher.
	if err := s.ensureActivePlayerMembership(r.Context(), tour.ClubID, user.ID); err != nil {
		log.Printf("ensure club membership before registration: user=%s club=%s: %v", uuidString(user.ID), uuidString(tour.ClubID), err)
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось подать заявку")
		return
	}

	reg, err := tournaments.CreateRegistration(r.Context(), s.store, tournaments.CreateRegistrationInput{
		TournamentID:      tour.ID,
		PlayerID:          user.ID,
		PartnerID:         partnerID,
		LookingForPartner: req.LookingForPartner,
	})
	if err != nil {
		switch {
		case errors.Is(err, tournaments.ErrRegistrationClosed):
			writeError(w, http.StatusConflict, "registration_closed", "Регистрация на турнир закрыта")
		case errors.Is(err, tournaments.ErrAlreadyRegistered):
			writeError(w, http.StatusConflict, "already_registered", "Вы уже подали заявку на этот турнир")
		case errors.Is(err, tournaments.ErrSelfPartner):
			writeError(w, http.StatusBadRequest, "validation", "Нельзя указать себя в качестве партнёра")
		case errors.Is(err, tournaments.ErrPartnerRequired):
			writeError(w, http.StatusBadRequest, "validation", "Для парного турнира укажите партнёра или отметьте «ищу партнёра»")
		case errors.Is(err, tournaments.ErrPartnerNotFound):
			writeError(w, http.StatusBadRequest, "validation", "Партнёр не найден или не подтверждён")
		case errors.Is(err, tournaments.ErrPartnerBusy):
			writeError(w, http.StatusConflict, "partner_busy", "Партнёр уже заявлен на этот турнир")
		case errors.Is(err, tournaments.ErrInvalidPairFlags):
			writeError(w, http.StatusBadRequest, "validation", "Некорректные параметры парной заявки")
		case errors.Is(err, tournaments.ErrTournamentNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
		default:
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось подать заявку")
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"registration": registrationFromModel(reg)})
}

func (s *Server) handleCancelRegistration(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	tour, err := s.store.Queries.GetTournamentBySlug(r.Context(), sqlc.GetTournamentBySlugParams{ClubID: s.currentClubID(r), Slug: slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось отменить заявку")
		return
	}

	reg, err := tournaments.CancelRegistration(r.Context(), s.store, tour.ID, user.ID)
	if err != nil {
		switch {
		case errors.Is(err, tournaments.ErrNotRegistered):
			writeError(w, http.StatusNotFound, "not_found", "Активная заявка не найдена")
		case errors.Is(err, tournaments.ErrTournamentNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
		default:
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось отменить заявку")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"registration": registrationFromModel(reg)})
}

func mergeTournamentWrite(req tournamentWriteRequest, before sqlc.Tournament) tournamentWriteRequest {
	out := req
	if strings.TrimSpace(out.Title) == "" {
		out.Title = before.Title
	}
	if strings.TrimSpace(out.Description) == "" {
		out.Description = before.Description
	}
	if strings.TrimSpace(out.Type) == "" {
		out.Type = string(before.Type)
	}
	if strings.TrimSpace(out.Category) == "" {
		out.Category = before.Category
	}
	if strings.TrimSpace(out.Surface) == "" {
		out.Surface = string(before.Surface)
	}
	if strings.TrimSpace(out.Format) == "" {
		out.Format = string(before.Format)
	}
	if strings.TrimSpace(out.Status) == "" {
		out.Status = string(before.Status)
	}
	if strings.TrimSpace(out.StartsAt) == "" {
		out.StartsAt = formatTS(before.StartsAt)
	}
	if out.EndsAt == nil && before.EndsAt.Valid {
		s := formatTS(before.EndsAt)
		out.EndsAt = &s
	}
	if strings.TrimSpace(out.RegistrationOpensAt) == "" {
		out.RegistrationOpensAt = formatTS(before.RegistrationOpensAt)
	}
	if strings.TrimSpace(out.RegistrationClosesAt) == "" {
		out.RegistrationClosesAt = formatTS(before.RegistrationClosesAt)
	}
	if strings.TrimSpace(out.VenueName) == "" {
		out.VenueName = before.VenueName
	}
	if strings.TrimSpace(out.VenueAddress) == "" {
		out.VenueAddress = before.VenueAddress
	}
	if out.VenueCourts == nil {
		v := before.VenueCourts
		out.VenueCourts = &v
	}
	if strings.TrimSpace(out.VenueSurface) == "" {
		out.VenueSurface = string(before.VenueSurface)
	}
	if out.EntryFee == nil {
		v := before.EntryFee
		out.EntryFee = &v
	}
	if out.MaxParticipants == nil {
		v := before.MaxParticipants
		out.MaxParticipants = &v
	}
	if out.CoverURL == nil && before.CoverUrl.Valid {
		v := before.CoverUrl.String
		out.CoverURL = &v
	}
	if out.Season == nil {
		v := before.Season
		out.Season = &v
	}
	return out
}

type parsedTournament struct {
	slug            string
	title           string
	description     string
	typ             sqlc.TournamentType
	category        string
	surface         sqlc.SurfaceType
	format          sqlc.TournamentFormat
	status          sqlc.TournamentStatus
	startsAt        pgtype.Timestamptz
	endsAt          pgtype.Timestamptz
	opensAt         pgtype.Timestamptz
	closesAt        pgtype.Timestamptz
	venueName       string
	venueAddress    string
	venueCourts     int32
	venueSurface    sqlc.SurfaceType
	entryFee        int32
	maxParticipants int32
	coverURL        pgtype.Text
	season          int32
	regulations     string
}

func parseTournamentWrite(req tournamentWriteRequest, creating bool) (parsedTournament, string) {
	title := strings.TrimSpace(req.Title)
	if utf8.RuneCountInString(title) < 3 {
		return parsedTournament{}, "Название турнира должно содержать минимум 3 символа"
	}
	description := strings.TrimSpace(req.Description)
	regulations := strings.TrimSpace(req.Regulations)
	if creating && utf8.RuneCountInString(regulations) < 20 {
		return parsedTournament{}, "Опишите регламент — минимум 20 символов"
	}

	typ, ok := parseTournamentType(req.Type)
	if !ok {
		return parsedTournament{}, "Укажите корректный тип турнира"
	}
	if !auth.ValidLevel(strings.TrimSpace(req.Category)) {
		return parsedTournament{}, "Укажите категорию из списка взрослых уровней"
	}
	surface, ok := parseSurface(req.Surface)
	if !ok {
		surface = sqlc.SurfaceTypeHard
	}
	format, ok := parseTournamentFormat(req.Format)
	if !ok {
		return parsedTournament{}, "Укажите корректный формат турнира"
	}
	status, ok := parseTournamentStatus(req.Status)
	if !ok {
		if creating {
			status = sqlc.TournamentStatusDraft
		} else {
			return parsedTournament{}, "Укажите корректный статус турнира"
		}
	}

	startsAt, err := parseRFC3339(req.StartsAt)
	if err != nil {
		return parsedTournament{}, "Укажите корректную дату старта (RFC3339)"
	}
	closesAt, err := parseRFC3339(req.RegistrationClosesAt)
	if err != nil {
		return parsedTournament{}, "Укажите корректный дедлайн регистрации (RFC3339)"
	}
	opensAt := closesAt
	if strings.TrimSpace(req.RegistrationOpensAt) != "" {
		opensAt, err = parseRFC3339(req.RegistrationOpensAt)
		if err != nil {
			return parsedTournament{}, "Укажите корректную дату открытия регистрации (RFC3339)"
		}
	} else if creating {
		opensAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	}
	if closesAt.Time.After(startsAt.Time) {
		return parsedTournament{}, "Регистрация должна закрыться до старта турнира"
	}
	if opensAt.Time.After(closesAt.Time) {
		return parsedTournament{}, "Открытие регистрации не может быть позже дедлайна"
	}

	var endsAt pgtype.Timestamptz
	if req.EndsAt != nil && strings.TrimSpace(*req.EndsAt) != "" {
		endsAt, err = parseRFC3339(*req.EndsAt)
		if err != nil {
			return parsedTournament{}, "Укажите корректную дату окончания (RFC3339)"
		}
	}

	maxParticipants := int32(16)
	if req.MaxParticipants != nil {
		maxParticipants = *req.MaxParticipants
	}
	if maxParticipants < 2 || maxParticipants > 256 {
		return parsedTournament{}, "Вместимость должна быть от 2 до 256"
	}
	entryFee := int32(0)
	if req.EntryFee != nil {
		entryFee = *req.EntryFee
	}
	if entryFee < 0 {
		return parsedTournament{}, "Взнос не может быть отрицательным"
	}

	venueName := strings.TrimSpace(req.VenueName)
	if venueName == "" {
		venueName = defaultVenueName
	}
	venueAddress := strings.TrimSpace(req.VenueAddress)
	if venueAddress == "" {
		venueAddress = defaultVenueAddress
	}
	venueCourts := int32(defaultVenueCourts)
	if req.VenueCourts != nil {
		venueCourts = *req.VenueCourts
	}
	if venueCourts < 1 || venueCourts > 64 {
		return parsedTournament{}, "Число кортов должно быть от 1 до 64"
	}
	venueSurface, ok := parseSurface(req.VenueSurface)
	if !ok {
		venueSurface = surface
	}

	season := int32(startsAt.Time.UTC().Year())
	if req.Season != nil {
		season = *req.Season
	}

	var cover pgtype.Text
	if req.CoverURL != nil && strings.TrimSpace(*req.CoverURL) != "" {
		cover = pgtype.Text{String: strings.TrimSpace(*req.CoverURL), Valid: true}
	}

	slug := strings.TrimSpace(req.Slug)
	if slug != "" {
		slug = tournaments.Slugify(slug)
	}

	return parsedTournament{
		slug:            slug,
		title:           title,
		description:     description,
		typ:             typ,
		category:        strings.TrimSpace(req.Category),
		surface:         surface,
		format:          format,
		status:          status,
		startsAt:        startsAt,
		endsAt:          endsAt,
		opensAt:         opensAt,
		closesAt:        closesAt,
		venueName:       venueName,
		venueAddress:    venueAddress,
		venueCourts:     venueCourts,
		venueSurface:    venueSurface,
		entryFee:        entryFee,
		maxParticipants: maxParticipants,
		coverURL:        cover,
		season:          season,
		regulations:     regulations,
	}, ""
}

func parseRFC3339(s string) (pgtype.Timestamptz, error) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		// accept datetime-local without zone as UTC
		t, err = time.ParseInLocation("2006-01-02T15:04", strings.TrimSpace(s), time.UTC)
		if err != nil {
			t, err = time.ParseInLocation("2006-01-02T15:04:05", strings.TrimSpace(s), time.UTC)
			if err != nil {
				return pgtype.Timestamptz{}, err
			}
		}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}, nil
}

func parseTournamentType(s string) (sqlc.TournamentType, bool) {
	v := sqlc.TournamentType(strings.TrimSpace(s))
	switch v {
	case sqlc.TournamentTypeSingles, sqlc.TournamentTypeDoubles, sqlc.TournamentTypeMixed,
		sqlc.TournamentTypeFinal, sqlc.TournamentTypeCup, sqlc.TournamentTypeTeams:
		return v, true
	default:
		return "", false
	}
}

func parseTournamentFormat(s string) (sqlc.TournamentFormat, bool) {
	v := sqlc.TournamentFormat(strings.TrimSpace(s))
	switch v {
	case sqlc.TournamentFormatOlympic, sqlc.TournamentFormatRoundRobin,
		sqlc.TournamentFormatGroupsPlayoff, sqlc.TournamentFormatDoubleElimination:
		return v, true
	default:
		return "", false
	}
}

func parseTournamentStatus(s string) (sqlc.TournamentStatus, bool) {
	v := sqlc.TournamentStatus(strings.TrimSpace(s))
	switch v {
	case sqlc.TournamentStatusDraft, sqlc.TournamentStatusRegistrationSoon,
		sqlc.TournamentStatusRegistrationOpen, sqlc.TournamentStatusRegistrationClosed,
		sqlc.TournamentStatusScheduled, sqlc.TournamentStatusLive,
		sqlc.TournamentStatusCompleted, sqlc.TournamentStatusCancelled:
		return v, true
	default:
		return "", false
	}
}

func parseSurface(s string) (sqlc.SurfaceType, bool) {
	v := sqlc.SurfaceType(strings.TrimSpace(s))
	switch v {
	case sqlc.SurfaceTypeHard, sqlc.SurfaceTypeClay, sqlc.SurfaceTypeGrass, sqlc.SurfaceTypeCarpet:
		return v, true
	default:
		return "", false
	}
}

func (s *Server) latestRegulations(r *http.Request, tournamentID pgtype.UUID) (string, error) {
	row, err := s.store.Queries.LatestTournamentRuleVersion(r.Context(), tournamentID)
	if err != nil {
		return "", err
	}
	return row.TextMarkdown, nil
}

type regCounts struct {
	confirmed int32
	waitlist  int32
}

func (s *Server) registrationCounts(r *http.Request, tournamentID pgtype.UUID) regCounts {
	rows, err := s.store.Queries.ListRegistrationsByTournament(r.Context(), tournamentID)
	if err != nil {
		return regCounts{}
	}
	var c regCounts
	for _, row := range rows {
		switch row.Status {
		case sqlc.RegistrationStatusConfirmed, sqlc.RegistrationStatusPending:
			c.confirmed++
		case sqlc.RegistrationStatusWaitlist:
			c.waitlist++
		}
	}
	return c
}
