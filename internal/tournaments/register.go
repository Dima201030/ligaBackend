package tournaments

import (
	"context"
	"errors"
	"time"

	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrTournamentNotFound = errors.New("tournament not found")
	ErrRegistrationClosed = errors.New("registration closed")
	ErrAlreadyRegistered  = errors.New("already registered")
	ErrPartnerBusy        = errors.New("partner busy")
	ErrSelfPartner        = errors.New("self partner")
	ErrPartnerRequired    = errors.New("partner required")
	ErrPartnerNotFound    = errors.New("partner not found")
	ErrNotRegistered      = errors.New("not registered")
	ErrInvalidPairFlags   = errors.New("invalid pair flags")
)

// IsPairType reports whether the tournament type needs a partner or looking_for_partner.
func IsPairType(t sqlc.TournamentType) bool {
	switch t {
	case sqlc.TournamentTypeDoubles, sqlc.TournamentTypeMixed, sqlc.TournamentTypeTeams:
		return true
	default:
		return false
	}
}

type CreateRegistrationInput struct {
	TournamentID      pgtype.UUID
	PlayerID          pgtype.UUID
	PartnerID         pgtype.UUID // Valid=false if none
	LookingForPartner bool
}

// CreateRegistration seats a player under SELECT FOR UPDATE capacity lock.
func CreateRegistration(ctx context.Context, store *db.Store, in CreateRegistrationInput) (sqlc.Registration, error) {
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return sqlc.Registration{}, err
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)

	tour, err := q.LockTournamentByID(ctx, in.TournamentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Registration{}, ErrTournamentNotFound
		}
		return sqlc.Registration{}, err
	}
	if tour.Status != sqlc.TournamentStatusRegistrationOpen {
		return sqlc.Registration{}, ErrRegistrationClosed
	}

	if in.PartnerID.Valid && uuidEqual(in.PartnerID, in.PlayerID) {
		return sqlc.Registration{}, ErrSelfPartner
	}
	if in.PartnerID.Valid && in.LookingForPartner {
		return sqlc.Registration{}, ErrInvalidPairFlags
	}

	if IsPairType(tour.Type) {
		if !in.PartnerID.Valid && !in.LookingForPartner {
			return sqlc.Registration{}, ErrPartnerRequired
		}
	} else if in.PartnerID.Valid || in.LookingForPartner {
		return sqlc.Registration{}, ErrInvalidPairFlags
	}

	busy, err := q.PlayerAlreadyActiveInTournament(ctx, sqlc.PlayerAlreadyActiveInTournamentParams{
		TournamentID: in.TournamentID,
		PlayerID:     in.PlayerID,
	})
	if err != nil {
		return sqlc.Registration{}, err
	}
	if busy {
		return sqlc.Registration{}, ErrAlreadyRegistered
	}

	if in.PartnerID.Valid {
		partner, err := q.GetApprovedUserByID(ctx, in.PartnerID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return sqlc.Registration{}, ErrPartnerNotFound
			}
			return sqlc.Registration{}, err
		}
		_ = partner
		partnerBusy, err := q.PlayerAlreadyActiveInTournament(ctx, sqlc.PlayerAlreadyActiveInTournamentParams{
			TournamentID: in.TournamentID,
			PlayerID:     in.PartnerID,
		})
		if err != nil {
			return sqlc.Registration{}, err
		}
		if partnerBusy {
			return sqlc.Registration{}, ErrPartnerBusy
		}
	}

	occupied, err := q.CountOccupyingRegistrations(ctx, in.TournamentID)
	if err != nil {
		return sqlc.Registration{}, err
	}

	status := sqlc.RegistrationStatusConfirmed
	var waitlistPos pgtype.Int4
	var confirmedAt pgtype.Timestamptz
	if occupied >= tour.MaxParticipants {
		status = sqlc.RegistrationStatusWaitlist
		next, err := q.NextWaitlistPosition(ctx, in.TournamentID)
		if err != nil {
			return sqlc.Registration{}, err
		}
		waitlistPos = pgtype.Int4{Int32: next, Valid: true}
	} else {
		confirmedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	}

	reg, err := q.CreateRegistration(ctx, sqlc.CreateRegistrationParams{
		TournamentID:      in.TournamentID,
		PlayerID:          in.PlayerID,
		PartnerID:         in.PartnerID,
		LookingForPartner: in.LookingForPartner,
		Status:            status,
		WaitlistPosition:  waitlistPos,
		ConfirmedAt:       confirmedAt,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return sqlc.Registration{}, ErrAlreadyRegistered
		}
		return sqlc.Registration{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return sqlc.Registration{}, err
	}
	return reg, nil
}

// CancelRegistration cancels the player's active entry and promotes waitlist if needed.
func CancelRegistration(ctx context.Context, store *db.Store, tournamentID, playerID pgtype.UUID) (sqlc.Registration, error) {
	tx, err := store.Pool.Begin(ctx)
	if err != nil {
		return sqlc.Registration{}, err
	}
	defer tx.Rollback(ctx)
	q := store.Queries.WithTx(tx)

	if _, err := q.LockTournamentByID(ctx, tournamentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Registration{}, ErrTournamentNotFound
		}
		return sqlc.Registration{}, err
	}

	before, err := q.LockRegistrationByTournamentPlayer(ctx, sqlc.LockRegistrationByTournamentPlayerParams{
		TournamentID: tournamentID,
		PlayerID:     playerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Registration{}, ErrNotRegistered
		}
		return sqlc.Registration{}, err
	}
	wasOccupying := before.Status == sqlc.RegistrationStatusConfirmed ||
		before.Status == sqlc.RegistrationStatusPending

	reg, err := q.CancelRegistration(ctx, sqlc.CancelRegistrationParams{
		TournamentID: tournamentID,
		PlayerID:     playerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Registration{}, ErrNotRegistered
		}
		return sqlc.Registration{}, err
	}

	if wasOccupying {
		tour, err := q.GetTournamentByID(ctx, tournamentID)
		if err != nil {
			return sqlc.Registration{}, err
		}
		occupied, err := q.CountOccupyingRegistrations(ctx, tournamentID)
		if err != nil {
			return sqlc.Registration{}, err
		}
		if occupied < tour.MaxParticipants {
			wait, err := q.LockOldestWaitlist(ctx, tournamentID)
			if err == nil {
				if _, err := q.PromoteRegistration(ctx, wait.ID); err != nil {
					return sqlc.Registration{}, err
				}
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return sqlc.Registration{}, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return sqlc.Registration{}, err
	}
	return reg, nil
}

func uuidEqual(a, b pgtype.UUID) bool {
	return a.Valid && b.Valid && a.Bytes == b.Bytes
}
