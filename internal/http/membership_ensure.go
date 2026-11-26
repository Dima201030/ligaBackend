package httpapi

import (
	"context"
	"errors"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ensureActivePlayerMembership creates or reactivates a player membership for the club.
// Blocked members are left unchanged (guest-style registration still allowed by caller).
func (s *Server) ensureActivePlayerMembership(ctx context.Context, clubID, userID pgtype.UUID) error {
	row, err := s.store.Queries.GetClubMembership(ctx, sqlc.GetClubMembershipParams{
		ClubID: clubID,
		UserID: userID,
	})
	switch {
	case err == nil:
		switch row.Status {
		case sqlc.ClubMemberStatusActive:
			return nil
		case sqlc.ClubMemberStatusBlocked:
			return nil
		case sqlc.ClubMemberStatusLeft, sqlc.ClubMemberStatusInvited:
			role := row.Role
			if role == "" {
				role = sqlc.ClubMemberRolePlayer
			}
			_, updErr := s.store.Queries.UpdateClubMembership(ctx, sqlc.UpdateClubMembershipParams{
				ClubID: clubID,
				UserID: userID,
				Role:   role,
				Status: sqlc.ClubMemberStatusActive,
			})
			return updErr
		default:
			return nil
		}
	case errors.Is(err, pgx.ErrNoRows):
		_, createErr := s.store.Queries.CreateClubMembership(ctx, sqlc.CreateClubMembershipParams{
			ClubID: clubID,
			UserID: userID,
			Role:   sqlc.ClubMemberRolePlayer,
			Status: sqlc.ClubMemberStatusActive,
		})
		return createErr
	default:
		return err
	}
}
