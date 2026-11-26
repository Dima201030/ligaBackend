package httpapi

import (
	"strings"
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

type creatorJSON struct {
	ID        string  `json:"id"`
	FirstName string  `json:"first_name"`
	LastName  string  `json:"last_name"`
	Email     *string `json:"email,omitempty"`
}

type tournamentJSON struct {
	ID                   string                `json:"id"`
	ClubID               string                `json:"club_id,omitempty"`
	ClubSlug             string                `json:"club_slug,omitempty"`
	ClubName             string                `json:"club_name,omitempty"`
	Slug                 string                `json:"slug"`
	Title                string                `json:"title"`
	Description          string                `json:"description"`
	Type                 string                `json:"type"`
	Category             string                `json:"category"`
	Surface              string                `json:"surface"`
	Format               string                `json:"format"`
	Status               string                `json:"status"`
	StartsAt             string                `json:"starts_at"`
	EndsAt               *string               `json:"ends_at,omitempty"`
	RegistrationOpensAt  string                `json:"registration_opens_at"`
	RegistrationClosesAt string                `json:"registration_closes_at"`
	Venue                venueJSON             `json:"venue"`
	EntryFee             int32                 `json:"entry_fee"`
	MaxParticipants      int32                 `json:"max_participants"`
	CoverURL             *string               `json:"cover_url,omitempty"`
	Regulations          string                `json:"regulations"`
	Season               int32                 `json:"season"`
	Hidden               bool                  `json:"is_hidden"`
	ConfirmedCount       *int32                `json:"confirmed_count,omitempty"`
	WaitlistCount        *int32                `json:"waitlist_count,omitempty"`
	BracketStatus        string                `json:"bracket_status,omitempty"`
	BracketVersion       *int32                `json:"bracket_version,omitempty"`
	BracketPublishedAt   *string               `json:"bracket_published_at,omitempty"`
	BracketThirdPlace    *bool                 `json:"bracket_third_place,omitempty"`
	Winners              []tournamentPlaceJSON `json:"winners,omitempty"`
	SuggestedPlaces      []tournamentPlaceJSON `json:"suggested_places,omitempty"`
	PlacesMismatch       bool                  `json:"places_mismatch,omitempty"`
	CreatedByID          string                `json:"created_by_id,omitempty"`
	CreatedBy            *creatorJSON          `json:"created_by,omitempty"`
}

type playerBriefJSON struct {
	ID        string `json:"id"`
	Slug      string `json:"slug,omitempty"`
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

func withClubMeta(tj tournamentJSON, slug, name string) tournamentJSON {
	tj.ClubSlug = slug
	tj.ClubName = name
	return tj
}

func tournamentFromRow(t sqlc.Tournament, regulations string) tournamentJSON {
	clubID := uuidString(t.ClubID)
	out := tournamentJSON{
		ID:                   uuidString(t.ID),
		ClubID:               clubID,
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
			ID:      clubID,
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
	out.BracketStatus = string(t.BracketStatus)
	v := t.BracketVersion
	out.BracketVersion = &v
	tp := t.BracketThirdPlace
	out.BracketThirdPlace = &tp
	if t.BracketPublishedAt.Valid {
		s := t.BracketPublishedAt.Time.UTC().Format(time.RFC3339)
		out.BracketPublishedAt = &s
	}
	if t.CreatedBy.Valid {
		out.CreatedByID = uuidString(t.CreatedBy)
	}
	return out
}

func registrationFromListRow(row sqlc.ListRegistrationsByTournamentRow) registrationJSON {
	return registrationFromJoinedRow(
		row.ID, row.TournamentID, row.PlayerID, row.PartnerID,
		row.LookingForPartner, row.Status, row.Seed, row.WaitlistPosition, row.CreatedAt,
		row.PlayerFirstName, row.PlayerLastName, row.PlayerLevel, row.PlayerSlug,
		row.PartnerFirstName, row.PartnerLastName, row.PartnerLevel, row.PartnerSlug,
	)
}

func registrationFromOrganizerListRow(row sqlc.ListOrganizerRegistrationsByTournamentRow) registrationJSON {
	return registrationFromJoinedRow(
		row.ID, row.TournamentID, row.PlayerID, row.PartnerID,
		row.LookingForPartner, row.Status, row.Seed, row.WaitlistPosition, row.CreatedAt,
		row.PlayerFirstName, row.PlayerLastName, row.PlayerLevel, row.PlayerSlug,
		row.PartnerFirstName, row.PartnerLastName, row.PartnerLevel, row.PartnerSlug,
	)
}

func registrationFromJoinedRow(
	id, tournamentID, playerID, partnerID pgtype.UUID,
	lookingForPartner bool,
	status sqlc.RegistrationStatus,
	seed, waitlistPosition pgtype.Int4,
	createdAt pgtype.Timestamptz,
	playerFirstName, playerLastName, playerLevel, playerSlug string,
	partnerFirstName, partnerLastName, partnerLevel, partnerSlug pgtype.Text,
) registrationJSON {
	out := registrationJSON{
		ID:                uuidString(id),
		TournamentID:      uuidString(tournamentID),
		PlayerID:          uuidString(playerID),
		LookingForPartner: lookingForPartner,
		Status:            string(status),
		RegisteredAt:      formatTS(createdAt),
		Player: playerBriefJSON{
			ID:        uuidString(playerID),
			Slug:      playerSlug,
			FirstName: playerFirstName,
			LastName:  playerLastName,
			Level:     playerLevel,
		},
	}
	if partnerID.Valid {
		pid := uuidString(partnerID)
		out.PartnerID = &pid
		if partnerFirstName.Valid && partnerLastName.Valid {
			level := ""
			if partnerLevel.Valid {
				level = partnerLevel.String
			}
			slug := ""
			if partnerSlug.Valid {
				slug = partnerSlug.String
			}
			out.Partner = &playerBriefJSON{
				ID:        pid,
				Slug:      slug,
				FirstName: partnerFirstName.String,
				LastName:  partnerLastName.String,
				Level:     level,
			}
		}
	}
	if seed.Valid {
		v := seed.Int32
		out.Seed = &v
	}
	if waitlistPosition.Valid {
		v := waitlistPosition.Int32
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

// publicVenue cleans legacy club branding out of publicly visible venue data.
// Internal organizer views keep the original values.
func publicVenue(v venueJSON) venueJSON {
	legacy := func(s string) bool {
		l := strings.ToLower(strings.TrimSpace(s))
		return strings.Contains(l, "pushkin") ||
			strings.Contains(l, "пушкин") ||
			strings.Contains(l, "волхонск") ||
			strings.Contains(l, "pairly") ||
			strings.Contains(l, "парли")
	}
	if legacy(v.Name) {
		v.Name = "Корты"
	}
	if legacy(v.Address) {
		v.Address = ""
	}
	return v
}
