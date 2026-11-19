package httpapi

import (
	"net/http"
	"strings"

	"github.com/Dima201030/ligaBackend/internal/auth"
)

type meProfileJSON struct {
	Slug          string  `json:"slug"`
	City          string  `json:"city"`
	Bio           string  `json:"bio"`
	AvatarURL     *string `json:"avatar_url,omitempty"`
	Status        string  `json:"status"`
	Rating        float64 `json:"rating"`
	Points        int32   `json:"points"`
	SinglesPoints int32   `json:"singles_points"`
	DoublesPoints int32   `json:"doubles_points"`
	MatchesPlayed int32   `json:"matches_played"`
	Wins          int32   `json:"wins"`
	Losses        int32   `json:"losses"`
	MemberSince   string  `json:"member_since,omitempty"`
}

type meRegistrationJSON struct {
	ID                 string  `json:"id"`
	TournamentID       string  `json:"tournament_id"`
	TournamentSlug     string  `json:"tournament_slug"`
	TournamentTitle    string  `json:"tournament_title"`
	TournamentStartsAt string  `json:"tournament_starts_at"`
	TournamentEntryFee int32   `json:"tournament_entry_fee"`
	TournamentStatus   string  `json:"tournament_status"`
	Status             string  `json:"status"`
	LookingForPartner  bool    `json:"looking_for_partner"`
	PartnerID          *string `json:"partner_id,omitempty"`
	PartnerName        *string `json:"partner_name,omitempty"`
	Seed               *int32  `json:"seed,omitempty"`
	RegisteredAt       string  `json:"registered_at"`
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	out := map[string]any{"user": sessionUserJSON(user)}
	prof, err := s.store.Queries.GetProfileByUserID(r.Context(), user.ID)
	if err == nil {
		out["profile"] = meProfileJSON{
			Slug:          prof.Slug,
			City:          prof.City,
			Bio:           prof.Bio,
			AvatarURL:     textPtr(prof.AvatarUrl),
			Status:        prof.PlayerStatus,
			Rating:        round1(prof.Rating),
			Points:        prof.Points,
			SinglesPoints: prof.SinglesPoints,
			DoublesPoints: prof.DoublesPoints,
			MatchesPlayed: prof.MatchesPlayed,
			Wins:          prof.Wins,
			Losses:        prof.Losses,
			MemberSince:   formatTS(prof.CreatedAt),
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleMyRegistrations(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	rows, err := s.store.Queries.ListRegistrationsByPlayer(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить заявки")
		return
	}
	out := make([]meRegistrationJSON, 0, len(rows))
	for _, row := range rows {
		item := meRegistrationJSON{
			ID:                 uuidString(row.ID),
			TournamentID:       uuidString(row.TournamentID),
			TournamentSlug:     row.TournamentSlug,
			TournamentTitle:    row.TournamentTitle,
			TournamentStartsAt: formatTS(row.TournamentStartsAt),
			TournamentEntryFee: row.TournamentEntryFee,
			TournamentStatus:   string(row.TournamentStatus),
			Status:             string(row.Status),
			LookingForPartner:  row.LookingForPartner,
			RegisteredAt:       formatTS(row.CreatedAt),
		}
		if row.PartnerID.Valid {
			v := uuidString(row.PartnerID)
			item.PartnerID = &v
		}
		if row.PartnerFirstName.Valid || row.PartnerLastName.Valid {
			name := strings.TrimSpace(row.PartnerFirstName.String + " " + row.PartnerLastName.String)
			if name != "" {
				item.PartnerName = &name
			}
		}
		if row.Seed.Valid {
			v := row.Seed.Int32
			item.Seed = &v
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"registrations": out})
}

func (s *Server) handleMyMatches(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	ids, err := s.store.Queries.ListMatchIDsByPlayer(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить матчи")
		return
	}
	matches, err := s.loadMatchesByIDs(r, ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить матчи")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
}
