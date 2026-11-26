package httpapi

import (
	"errors"
	"net/http"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
)

// Organizer access scopes for the current club tenant.
const (
	organizerAccessNone       = "none"
	organizerAccessTournament = "tournament" // limited: tournaments, matches, bracket, players
	organizerAccessFull       = "full"       // club owner: members, settings, applications, content
)

var errForbiddenOwner = errors.New("forbidden owner")

// actorClubMembershipRole returns the caller's membership role in the current club.
// Global owner/superadmin is treated as club owner when they have no membership row.
// Global organizer is treated as club organizer (limited) when they have no membership row.
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
	switch actor.Role {
	case sqlc.UserRoleOwner, sqlc.UserRoleSuperadmin:
		return sqlc.ClubMemberRoleOwner, nil
	case sqlc.UserRoleOrganizer:
		return sqlc.ClubMemberRoleOrganizer, nil
	default:
		return "", errForbiddenClub
	}
}

// actorOrganizerAccess resolves club-scoped organizer access for /me and UI gating.
func (s *Server) actorOrganizerAccess(r *http.Request) (access string, role sqlc.ClubMemberRole, err error) {
	role, err = s.actorClubMembershipRole(r)
	if err != nil {
		if errors.Is(err, errUnauthorized) || errors.Is(err, errForbiddenClub) {
			return organizerAccessNone, "", nil
		}
		return organizerAccessNone, "", err
	}
	switch role {
	case sqlc.ClubMemberRoleOwner:
		return organizerAccessFull, role, nil
	case sqlc.ClubMemberRoleOrganizer:
		return organizerAccessTournament, role, nil
	default:
		return organizerAccessNone, role, nil
	}
}

func (s *Server) requireClubOrganizerRole(r *http.Request) (sqlc.ClubMemberRole, error) {
	role, err := s.actorClubMembershipRole(r)
	if err != nil {
		return "", err
	}
	if clubRoleRank(role) < clubRoleRank(sqlc.ClubMemberRoleOrganizer) {
		return role, errForbiddenClub
	}
	return role, nil
}

func (s *Server) requireClubOwnerRole(r *http.Request) (sqlc.ClubMemberRole, error) {
	role, err := s.actorClubMembershipRole(r)
	if err != nil {
		return "", err
	}
	if role != sqlc.ClubMemberRoleOwner {
		return role, errForbiddenOwner
	}
	return role, nil
}

func (s *Server) writeOwnerRoleError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, errUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
	case errors.Is(err, errForbiddenClub), errors.Is(err, errForbiddenOwner):
		writeError(w, http.StatusForbidden, "forbidden", "Нужна роль владельца клуба")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось проверить права")
	}
	return true
}
