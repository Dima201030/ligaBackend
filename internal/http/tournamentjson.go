package httpapi

import (
	"time"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type venueJSON struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Courts  int32  `json:"courts"`
	Surface string `json:"surface"`
}

type tournamentJSON struct {
	ID                   string    `json:"id"`
	Slug                 string    `json:"slug"`
	Title                string    `json:"title"`
	Description          string    `json:"description"`
	Type                 string    `json:"type"`
	Category             string    `json:"category"`
	Surface              string    `json:"surface"`
	Format               string    `json:"format"`
	Status               string    `json:"status"`
	StartsAt             string    `json:"starts_at"`
	EndsAt               *string   `json:"ends_at,omitempty"`
	RegistrationOpensAt  string    `json:"registration_opens_at"`
	RegistrationClosesAt string    `json:"registration_closes_at"`
	Venue                venueJSON `json:"venue"`
	EntryFee             int32     `json:"entry_fee"`
	MaxParticipants      int32     `json:"max_participants"`
	CoverURL             *string   `json:"cover_url,omitempty"`
	Regulations          string    `json:"regulations"`
	Season               int32     `json:"season"`
	ConfirmedCount       *int32    `json:"confirmed_count,omitempty"`
	WaitlistCount        *int32    `json:"waitlist_count,omitempty"`
}

type playerBriefJSON struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Level     string `json:"level"`
}

type registrationJSON struct {
	ID                string           `json:"id"`
	TournamentID      string           `json:"tournament_id"`
	PlayerID          string           `json:"player_id"`
	PartnerID         *string          `json:"partner_id,omitempty"`
	LookingForPartner bool             `json:"looking_for_partner"`
	Status            string           `json:"status"`
	Seed              *int32           `json:"seed,omitempty"`
	WaitlistPosition  *int32           `json:"waitlist_position,omitempty"`
	RegisteredAt      string           `json:"registered_at"`
	Player            playerBriefJSON  `json:"player"`
	Partner           *playerBriefJSON `json:"partner,omitempty"`
}

func tournamentFromRow(t sqlc.Tournament, regulations string) tournamentJSON {
	out := tournamentJSON{
		ID:                   uuidString(t.ID),
		Slug:                 t.Slug,
		Title:                t.Title,
		Description:          t.Description,
		Type:                 string(t.Type),
		Category:             t.Category,
		Surface:              string(t.Surface),
		Format:               string(t.Format),
		Status:               string(t.Status),
		StartsAt:             formatTS(t.StartsAt),
		RegistrationOpensAt:  formatTS(t.RegistrationOpensAt),
		RegistrationClosesAt: formatTS(t.RegistrationClosesAt),
		Venue: venueJSON{
			ID:      "club-default",
			Name:    t.VenueName,
			Address: t.VenueAddress,
			Courts:  t.VenueCourts,
			Surface: string(t.VenueSurface),
		},
		EntryFee:        t.EntryFee,
		MaxParticipants: t.MaxParticipants,
		Regulations:     regulations,
		Season:          t.Season,
	}
	if t.EndsAt.Valid {
		s := t.EndsAt.Time.UTC().Format(time.RFC3339)
		out.EndsAt = &s
	}
	if t.CoverUrl.Valid {
		v := t.CoverUrl.String
		out.CoverURL = &v
	}
	return out
}

func registrationFromListRow(row sqlc.ListRegistrationsByTournamentRow) registrationJSON {
	out := registrationJSON{
		ID:                uuidString(row.ID),
		TournamentID:      uuidString(row.TournamentID),
		PlayerID:          uuidString(row.PlayerID),
		LookingForPartner: row.LookingForPartner,
		Status:            string(row.Status),
		RegisteredAt:      formatTS(row.CreatedAt),
		Player: playerBriefJSON{
			ID:        uuidString(row.PlayerID),
			FirstName: row.PlayerFirstName,
			LastName:  row.PlayerLastName,
			Level:     row.PlayerLevel,
		},
	}
	if row.PartnerID.Valid {
		id := uuidString(row.PartnerID)
		out.PartnerID = &id
		if row.PartnerFirstName.Valid && row.PartnerLastName.Valid {
			level := ""
			if row.PartnerLevel.Valid {
				level = row.PartnerLevel.String
			}
			out.Partner = &playerBriefJSON{
				ID:        id,
				FirstName: row.PartnerFirstName.String,
				LastName:  row.PartnerLastName.String,
				Level:     level,
			}
		}
	}
	if row.Seed.Valid {
		v := row.Seed.Int32
		out.Seed = &v
	}
	if row.WaitlistPosition.Valid {
		v := row.WaitlistPosition.Int32
		out.WaitlistPosition = &v
	}
	return out
}

func registrationFromModel(reg sqlc.Registration) registrationJSON {
	out := registrationJSON{
		ID:                uuidString(reg.ID),
		TournamentID:      uuidString(reg.TournamentID),
		PlayerID:          uuidString(reg.PlayerID),
		LookingForPartner: reg.LookingForPartner,
		Status:            string(reg.Status),
		RegisteredAt:      formatTS(reg.CreatedAt),
		Player:            playerBriefJSON{ID: uuidString(reg.PlayerID)},
	}
	if reg.PartnerID.Valid {
		id := uuidString(reg.PartnerID)
		out.PartnerID = &id
	}
	if reg.Seed.Valid {
		v := reg.Seed.Int32
		out.Seed = &v
	}
	if reg.WaitlistPosition.Valid {
		v := reg.WaitlistPosition.Int32
		out.WaitlistPosition = &v
	}
	return out
}

func formatTS(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.UTC().Format(time.RFC3339)
}
