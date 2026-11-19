package rating

import (
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// NewProfileParams builds CreateProfile defaults (start rating 1200, active).
func NewProfileParams(userID pgtype.UUID, phone pgtype.Text, level, firstName, lastName string) sqlc.CreateProfileParams {
	return sqlc.CreateProfileParams{
		UserID:        userID,
		Phone:         phone,
		Level:         level,
		Slug:          PlayerSlug(lastName, firstName, userID),
		Gender:        pgtype.Text{},
		City:          "Пушкин",
		Bio:           "",
		AvatarUrl:     pgtype.Text{},
		PlayerStatus:  "active",
		Rating:         DefaultCoeffs.StartRating,
		Points:         0,
		SinglesPoints:  0,
		DoublesPoints:  0,
		MatchesPlayed:  0,
		Wins:           0,
		Losses:         0,
	}
}
