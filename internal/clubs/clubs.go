package clubs

import (
	"context"
	"strings"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// DefaultSlug is the bootstrap tenant for the original single-club install.
const DefaultSlug = "mr-pushkin"

// DefaultID is the stable UUID seeded in migration 00011.
const DefaultID = "11111111-1111-4111-8111-111111111111"

type contextKey string

const clubContextKey contextKey = "tenant_club"

// Club is the resolved tenant for a request.
type Club struct {
	ID     pgtype.UUID
	Slug   string
	Name   string
	City   string
	Status sqlc.ClubStatus
}

// FromRow maps a sqlc club row.
func FromRow(row sqlc.Club) Club {
	return Club{
		ID:     row.ID,
		Slug:   row.Slug,
		Name:   row.Name,
		City:   row.City,
		Status: row.Status,
	}
}

// SetClub stores the tenant club on the request context.
func SetClub(ctx context.Context, club *Club) context.Context {
	return context.WithValue(ctx, clubContextKey, club)
}

// FromContext returns the tenant club, if any.
func FromContext(ctx context.Context) *Club {
	club, _ := ctx.Value(clubContextKey).(*Club)
	return club
}

// MustFromContext returns the club or panics if missing (handler bug).
func MustFromContext(ctx context.Context) Club {
	club := FromContext(ctx)
	if club == nil {
		panic("clubs: missing tenant club in context")
	}
	return *club
}

// DefaultUUID returns the seeded default club id as pgtype.UUID.
func DefaultUUID() pgtype.UUID {
	var id pgtype.UUID
	_ = id.Scan(DefaultID)
	return id
}

// MemberRoleFromUserRole maps the global users.role onto a club membership role.
func MemberRoleFromUserRole(role sqlc.UserRole) sqlc.ClubMemberRole {
	switch role {
	case sqlc.UserRoleOwner, sqlc.UserRoleSuperadmin:
		return sqlc.ClubMemberRoleOwner
	case sqlc.UserRoleOrganizer:
		return sqlc.ClubMemberRoleOrganizer
	default:
		return sqlc.ClubMemberRolePlayer
	}
}

// NormalizeSlug lowercases and trims a club slug candidate.
func NormalizeSlug(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
