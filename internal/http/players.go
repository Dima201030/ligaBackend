package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type playerJSON struct {
	ID            string  `json:"id"`
	Slug          string  `json:"slug"`
	FirstName     string  `json:"first_name"`
	LastName      string  `json:"last_name"`
	Level         string  `json:"level"`
	Category      string  `json:"category"`
	Gender        *string `json:"gender,omitempty"`
	City          string  `json:"city"`
	Bio           string  `json:"bio"`
	AvatarURL     *string `json:"avatar_url,omitempty"`
	Status        string  `json:"status"`
	Rating         float64 `json:"rating"`
	Points         int32   `json:"points"`
	SinglesPoints  int32   `json:"singles_points"`
	DoublesPoints  int32   `json:"doubles_points"`
	MatchesPlayed  int32   `json:"matches_played"`
	Wins           int32   `json:"wins"`
	Losses         int32   `json:"losses"`
	Provisional    bool    `json:"provisional"`
	Email          string  `json:"email,omitempty"`
	Phone          *string `json:"phone,omitempty"`
	UserStatus     string  `json:"user_status,omitempty"`
	Role           string  `json:"role,omitempty"`
	MemberSince    string  `json:"member_since,omitempty"`
}

func (s *Server) handleListPublicPlayers(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := queryInt(r, "limit", 50, 1, 200)
	offset := queryInt(r, "offset", 0, 0, 100000)

	rows, err := s.store.Queries.ListPublicPlayers(r.Context(), sqlc.ListPublicPlayersParams{
		Category:    optionalText(category),
		Query:       optionalText(query),
		LimitCount:  int32(limit),
		OffsetCount: int32(offset),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить игроков")
		return
	}
	out := make([]playerJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, publicPlayerFromRow(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"players": out})
}

func (s *Server) handleGetPublicPlayer(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	row, err := s.store.Queries.GetPublicPlayerBySlug(r.Context(), sqlc.GetPublicPlayerBySlugParams{ClubID: s.currentClubID(r), Slug: slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Игрок не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить игрока")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"player": publicPlayerDetail(row)})
}

func (s *Server) handleListOrganizerPlayers(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	limit := queryInt(r, "limit", 100, 1, 500)
	offset := queryInt(r, "offset", 0, 0, 100000)

	rows, err := s.store.Queries.ListOrganizerPlayers(r.Context(), sqlc.ListOrganizerPlayersParams{
		UserStatus:  optionalText(status),
		Category:    optionalText(category),
		LimitCount:  int32(limit),
		OffsetCount: int32(offset),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить игроков")
		return
	}
	out := make([]playerJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, organizerPlayerFromRow(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"players": out})
}

type updatePlayerRequest struct {
	Level        *string `json:"level"`
	Gender       *string `json:"gender"`
	City         *string `json:"city"`
	Bio          *string `json:"bio"`
	AvatarURL    *string `json:"avatar_url"`
	PlayerStatus *string `json:"player_status"`
}

func (s *Server) handleUpdateOrganizerPlayer(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}
	var req updatePlayerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	if req.Level != nil && !auth.ValidLevel(strings.TrimSpace(*req.Level)) {
		writeError(w, http.StatusBadRequest, "validation", "Некорректная категория")
		return
	}
	if req.PlayerStatus != nil {
		switch strings.TrimSpace(*req.PlayerStatus) {
		case "active", "blocked", "archived":
		default:
			writeError(w, http.StatusBadRequest, "validation", "Некорректный статус игрока")
			return
		}
	}
	if req.Gender != nil {
		g := strings.TrimSpace(*req.Gender)
		if g != "" && g != "male" && g != "female" {
			writeError(w, http.StatusBadRequest, "validation", "Некорректный пол")
			return
		}
	}

	prof, err := s.store.Queries.UpdateProfilePublic(r.Context(), sqlc.UpdateProfilePublicParams{
		UserID:       id,
		Level:        optionalTextPtr(req.Level),
		Gender:       optionalTextPtr(req.Gender),
		City:         optionalTextPtr(req.City),
		Bio:          optionalTextPtr(req.Bio),
		AvatarUrl:    optionalTextPtr(req.AvatarURL),
		PlayerStatus: optionalTextPtr(req.PlayerStatus),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Игрок не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось обновить игрока")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"player": map[string]any{
			"id":            uuidString(prof.UserID),
			"slug":          prof.Slug,
			"level":         prof.Level,
			"status":        prof.PlayerStatus,
			"rating":        prof.Rating,
			"points":        prof.Points,
			"city":          prof.City,
			"bio":           prof.Bio,
			"matches_played": prof.MatchesPlayed,
		},
	})
}

func publicPlayerFromRow(row sqlc.ListPublicPlayersRow) playerJSON {
	return playerJSON{
		ID:            uuidString(row.ID),
		Slug:          row.Slug,
		FirstName:     row.FirstName,
		LastName:      row.LastName,
		Level:         row.Level,
		Category:      row.Level,
		Gender:        textPtr(row.Gender),
		City:          row.City,
		Bio:           row.Bio,
		AvatarURL:     textPtr(row.AvatarUrl),
		Status:        row.PlayerStatus,
		Rating:        round1(row.Rating),
		Points:        row.Points,
		SinglesPoints: row.SinglesPoints,
		DoublesPoints: row.DoublesPoints,
		MatchesPlayed: row.MatchesPlayed,
		Wins:          row.Wins,
		Losses:        row.Losses,
		Provisional:   row.MatchesPlayed < 10,
		MemberSince:   formatTS(row.CreatedAt),
	}
}

func publicPlayerDetail(row sqlc.GetPublicPlayerBySlugRow) playerJSON {
	return playerJSON{
		ID:            uuidString(row.ID),
		Slug:          row.Slug,
		FirstName:     row.FirstName,
		LastName:      row.LastName,
		Level:         row.Level,
		Category:      row.Level,
		Gender:        textPtr(row.Gender),
		City:          row.City,
		Bio:           row.Bio,
		AvatarURL:     textPtr(row.AvatarUrl),
		Status:        row.PlayerStatus,
		Rating:        round1(row.Rating),
		Points:        row.Points,
		SinglesPoints: row.SinglesPoints,
		DoublesPoints: row.DoublesPoints,
		MatchesPlayed: row.MatchesPlayed,
		Wins:          row.Wins,
		Losses:        row.Losses,
		Provisional:   row.MatchesPlayed < 10,
		MemberSince:   formatTS(row.CreatedAt),
	}
}

func organizerPlayerFromRow(row sqlc.ListOrganizerPlayersRow) playerJSON {
	return playerJSON{
		ID:            uuidString(row.ID),
		Slug:          row.Slug,
		FirstName:     row.FirstName,
		LastName:      row.LastName,
		Level:         row.Level,
		Category:      row.Level,
		Gender:        textPtr(row.Gender),
		City:          row.City,
		Bio:           row.Bio,
		AvatarURL:     textPtr(row.AvatarUrl),
		Status:        row.PlayerStatus,
		Rating:        round1(row.Rating),
		Points:        row.Points,
		SinglesPoints: row.SinglesPoints,
		DoublesPoints: row.DoublesPoints,
		MatchesPlayed: row.MatchesPlayed,
		Wins:          row.Wins,
		Losses:        row.Losses,
		Provisional:   row.MatchesPlayed < 10,
		Email:         row.Email,
		Phone:         textPtr(row.Phone),
		UserStatus:    string(row.UserStatus),
		Role:          string(row.Role),
		MemberSince:   formatTS(row.CreatedAt),
	}
}

func optionalText(s string) pgtype.Text {
	if strings.TrimSpace(s) == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func optionalTextPtr(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	v := t.String
	return &v
}

func queryInt(r *http.Request, key string, def, min, max int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
