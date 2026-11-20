package httpapi

import (
	"encoding/json"
	"time"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type matchSideJSON struct {
	Side     int16    `json:"side"`
	PlayerID *string  `json:"player_id,omitempty"`
	PartnerID *string `json:"partner_id,omitempty"`
	Seed     *int32   `json:"seed,omitempty"`
	Label    *string  `json:"label,omitempty"`
	IsBye    bool     `json:"is_bye"`
	Player   *playerBriefJSON `json:"player,omitempty"`
	Partner  *playerBriefJSON `json:"partner,omitempty"`
}

type matchJSON struct {
	ID               string          `json:"id"`
	TournamentID     string          `json:"tournament_id"`
	TournamentSlug   string          `json:"tournament_slug,omitempty"`
	TournamentTitle  string          `json:"tournament_title,omitempty"`
	TournamentType   string          `json:"tournament_type,omitempty"`
	BracketKey       string          `json:"bracket_key"`
	Stage            string          `json:"stage"`
	RoundIndex       int32           `json:"round_index"`
	Position         int32           `json:"position"`
	RoundLabel       string          `json:"round_label"`
	ScheduledAt      *string         `json:"scheduled_at,omitempty"`
	Court            *string         `json:"court,omitempty"`
	Status           string          `json:"status"`
	ScoreSets        json.RawMessage `json:"score_sets,omitempty"`
	ScoreText        *string         `json:"score_text,omitempty"`
	WinnerSide       *int16          `json:"winner_side,omitempty"`
	NextMatchID      *string         `json:"next_match_id,omitempty"`
	NextSlot         *int16          `json:"next_slot,omitempty"`
	LoserNextMatchID *string         `json:"loser_next_match_id,omitempty"`
	LoserNextSlot    *int16          `json:"loser_next_slot,omitempty"`
	IsBye            bool            `json:"is_bye"`
	Version          int32           `json:"version"`
	Side1            matchSideJSON   `json:"side1"`
	Side2            matchSideJSON   `json:"side2"`
}

type bracketMetaJSON struct {
	Status      string  `json:"status"`
	Version     int32   `json:"version"`
	PublishedAt *string `json:"published_at,omitempty"`
	ThirdPlace  bool    `json:"third_place"`
	Format      string  `json:"format"`
	MatchCount  int     `json:"match_count"`
}

func matchFromRow(m sqlc.Match, sides []sqlc.MatchSide, players map[string]playerBriefJSON) matchJSON {
	out := matchJSON{
		ID:           uuidString(m.ID),
		TournamentID: uuidString(m.TournamentID),
		BracketKey:   m.BracketKey,
		Stage:        m.Stage,
		RoundIndex:   m.RoundIndex,
		Position:     m.Position,
		RoundLabel:   m.RoundLabel,
		Status:       string(m.Status),
		IsBye:        m.IsBye,
		Version:      m.Version,
		Side1:        matchSideJSON{Side: 1},
		Side2:        matchSideJSON{Side: 2},
	}
	if m.ScheduledAt.Valid {
		s := m.ScheduledAt.Time.UTC().Format(time.RFC3339)
		out.ScheduledAt = &s
	}
	if m.Court.Valid {
		v := m.Court.String
		out.Court = &v
	}
	if len(m.ScoreSets) > 0 && string(m.ScoreSets) != "null" {
		out.ScoreSets = json.RawMessage(m.ScoreSets)
	}
	if m.ScoreText.Valid {
		v := m.ScoreText.String
		out.ScoreText = &v
	}
	if m.WinnerSide.Valid {
		v := m.WinnerSide.Int16
		out.WinnerSide = &v
	}
	if m.NextMatchID.Valid {
		v := uuidString(m.NextMatchID)
		out.NextMatchID = &v
	}
	if m.NextSlot.Valid {
		v := m.NextSlot.Int16
		out.NextSlot = &v
	}
	if m.LoserNextMatchID.Valid {
		v := uuidString(m.LoserNextMatchID)
		out.LoserNextMatchID = &v
	}
	if m.LoserNextSlot.Valid {
		v := m.LoserNextSlot.Int16
		out.LoserNextSlot = &v
	}

	for _, s := range sides {
		js := matchSideJSON{
			Side:  s.Side,
			IsBye: s.IsBye,
		}
		if s.PlayerID.Valid {
			v := uuidString(s.PlayerID)
			js.PlayerID = &v
			if p, ok := players[v]; ok {
				cp := p
				js.Player = &cp
			}
		}
		if s.PartnerID.Valid {
			v := uuidString(s.PartnerID)
			js.PartnerID = &v
			if p, ok := players[v]; ok {
				cp := p
				js.Partner = &cp
			}
		}
		if s.Seed.Valid {
			v := s.Seed.Int32
			js.Seed = &v
		}
		if s.Label.Valid {
			v := s.Label.String
			js.Label = &v
		}
		if s.Side == 1 {
			out.Side1 = js
		} else if s.Side == 2 {
			out.Side2 = js
		}
	}
	return out
}

func matchFromOrganizerRow(m sqlc.ListOrganizerMatchesRow, sides []sqlc.MatchSide, players map[string]playerBriefJSON) matchJSON {
	base := sqlc.Match{
		ID:               m.ID,
		TournamentID:     m.TournamentID,
		BracketKey:       m.BracketKey,
		Stage:            m.Stage,
		RoundIndex:       m.RoundIndex,
		Position:         m.Position,
		RoundLabel:       m.RoundLabel,
		ScheduledAt:      m.ScheduledAt,
		Court:            m.Court,
		Status:           m.Status,
		ScoreSets:        m.ScoreSets,
		ScoreText:        m.ScoreText,
		WinnerSide:       m.WinnerSide,
		NextMatchID:      m.NextMatchID,
		NextSlot:         m.NextSlot,
		LoserNextMatchID: m.LoserNextMatchID,
		LoserNextSlot:    m.LoserNextSlot,
		IsBye:            m.IsBye,
		Version:          m.Version,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
	out := matchFromRow(base, sides, players)
	out.TournamentSlug = m.TournamentSlug
	out.TournamentTitle = m.TournamentTitle
	out.TournamentType = string(m.TournamentType)
	return out
}

func sidesByMatch(all []sqlc.MatchSide) map[string][]sqlc.MatchSide {
	out := make(map[string][]sqlc.MatchSide)
	for _, s := range all {
		key := uuidString(s.MatchID)
		out[key] = append(out[key], s)
	}
	return out
}

func collectPlayerIDs(sides []sqlc.MatchSide) []pgtype.UUID {
	seen := map[string]struct{}{}
	var ids []pgtype.UUID
	for _, s := range sides {
		for _, id := range []pgtype.UUID{s.PlayerID, s.PartnerID} {
			if !id.Valid {
				continue
			}
			key := uuidString(id)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}
