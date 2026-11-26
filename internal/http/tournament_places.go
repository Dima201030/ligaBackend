package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/Dima201030/ligaBackend/internal/uuidx"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type tournamentPlaceJSON struct {
	Place     int      `json:"place"`
	PlayerIDs []string `json:"player_ids"`
}

type tournamentPlacesRequest struct {
	Places []struct {
		Place     int      `json:"place"`
		PlayerIDs []string `json:"player_ids"`
	} `json:"places"`
}

// listTournamentPlaces returns podium places for one tournament, ordered 1..4.
func (s *Server) listTournamentPlaces(ctx context.Context, clubID, tournamentID pgtype.UUID) ([]tournamentPlaceJSON, error) {
	rows, err := s.store.Pool.Query(ctx, `
		SELECT place, array_to_json(player_ids)::text
		FROM tournament_places
		WHERE club_id = $1 AND tournament_id = $2
		ORDER BY place`, clubID, tournamentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]tournamentPlaceJSON, 0, 4)
	for rows.Next() {
		var place int16
		var raw string
		if err := rows.Scan(&place, &raw); err != nil {
			return nil, err
		}
		out = append(out, tournamentPlaceJSON{Place: int(place), PlayerIDs: parsePlacePlayerIDs(raw)})
	}
	return out, rows.Err()
}

// allTournamentPlaces groups podium places by tournament id for list responses.
func (s *Server) allTournamentPlaces(ctx context.Context, clubID pgtype.UUID) (map[string][]tournamentPlaceJSON, error) {
	rows, err := s.store.Pool.Query(ctx, `
		SELECT tournament_id, place, array_to_json(player_ids)::text
		FROM tournament_places
		WHERE club_id = $1
		ORDER BY place`, clubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string][]tournamentPlaceJSON{}
	for rows.Next() {
		var id pgtype.UUID
		var place int16
		var raw string
		if err := rows.Scan(&id, &place, &raw); err != nil {
			return nil, err
		}
		key := uuidString(id)
		out[key] = append(out[key], tournamentPlaceJSON{Place: int(place), PlayerIDs: parsePlacePlayerIDs(raw)})
	}
	return out, rows.Err()
}

func parsePlacePlayerIDs(raw string) []string {
	var ids []string
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &ids)
	}
	if ids == nil {
		return []string{}
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		n := uuidx.Normalize(id)
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

func podiumToJSON(slots []rating.PodiumPlace) []tournamentPlaceJSON {
	out := make([]tournamentPlaceJSON, 0, len(slots))
	for _, slot := range slots {
		ids := make([]string, 0, len(slot.PlayerIDs))
		for _, id := range slot.PlayerIDs {
			s := uuidString(id)
			if s != "" {
				ids = append(ids, s)
			}
		}
		if len(ids) == 0 {
			continue
		}
		out = append(out, tournamentPlaceJSON{Place: slot.Place, PlayerIDs: ids})
	}
	return out
}

func placesToPodium(places []tournamentPlaceJSON) []rating.PodiumPlace {
	out := make([]rating.PodiumPlace, 0, len(places))
	for _, p := range places {
		ids := make([]pgtype.UUID, 0, len(p.PlayerIDs))
		for _, raw := range p.PlayerIDs {
			id, err := parseUUID(raw)
			if err != nil {
				continue
			}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			continue
		}
		out = append(out, rating.PodiumPlace{Place: p.Place, PlayerIDs: ids})
	}
	return out
}

func (s *Server) suggestedTournamentPlaces(ctx context.Context, tour sqlc.Tournament) ([]tournamentPlaceJSON, error) {
	matches, err := s.store.Queries.ListMatchesByTournament(ctx, tour.ID)
	if err != nil {
		return nil, err
	}
	sides, err := s.store.Queries.ListMatchSidesByTournament(ctx, tour.ID)
	if err != nil {
		return nil, err
	}
	return podiumToJSON(rating.DerivePodiumPlaces(tour.Format, matches, sides)), nil
}

func (s *Server) attachPlacesSuggestion(ctx context.Context, tour sqlc.Tournament, tj *tournamentJSON) {
	suggested, err := s.suggestedTournamentPlaces(ctx, tour)
	if err != nil {
		return
	}
	tj.SuggestedPlaces = suggested
	tj.PlacesMismatch = rating.PodiumMismatch(placesToPodium(tj.Winners), placesToPodium(suggested))
}

// duplicatePlayerAcrossPlaces reports whether any player_id appears on more than one place
// (or twice inside one place).
func duplicatePlayerAcrossPlaces(places []tournamentPlaceJSON) bool {
	seen := map[string]struct{}{}
	for _, place := range places {
		for _, id := range place.PlayerIDs {
			key := uuidx.Normalize(id)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				return true
			}
			seen[key] = struct{}{}
		}
	}
	return false
}

func (s *Server) handleSetTournamentPlaces(w http.ResponseWriter, r *http.Request) {
	actor := auth.UserFromContext(r.Context())
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	tournamentID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор турнира")
		return
	}
	tour, err := s.loadClubTournament(r, tournamentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить турнир")
		return
	}

	var req tournamentPlacesRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Некорректное тело запроса")
		return
	}
	if len(req.Places) > 4 {
		writeError(w, http.StatusBadRequest, "validation", "Можно указать не больше четырёх мест")
		return
	}

	seenPlace := map[int]bool{}
	places := make([]tournamentPlaceJSON, 0, len(req.Places))
	allIDs := make([]pgtype.UUID, 0, 8)
	for _, entry := range req.Places {
		if entry.Place < 1 || entry.Place > 4 {
			writeError(w, http.StatusBadRequest, "validation", "Место должно быть от 1 до 4")
			return
		}
		if seenPlace[entry.Place] {
			writeError(w, http.StatusBadRequest, "validation", "Место указано дважды")
			return
		}
		if len(entry.PlayerIDs) > 2 {
			writeError(w, http.StatusBadRequest, "validation", "На одном месте не больше двух игроков")
			return
		}
		seenPlace[entry.Place] = true
		ids := make([]string, 0, len(entry.PlayerIDs))
		for _, raw := range entry.PlayerIDs {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			parsed, err := parseUUID(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор игрока")
				return
			}
			ids = append(ids, uuidString(parsed))
			allIDs = append(allIDs, parsed)
		}
		places = append(places, tournamentPlaceJSON{Place: entry.Place, PlayerIDs: ids})
	}

	if duplicatePlayerAcrossPlaces(places) {
		writeError(w, http.StatusBadRequest, "validation",
			"Один игрок не может занимать несколько мест сразу")
		return
	}

	clubID := s.currentClubID(r)
	if len(allIDs) > 0 {
		// Bind canonical text[] + explicit ::uuid[] cast (uuidx.TextArray).
		// Raw []pgtype.UUID into ANY($n) without cast failed on Pi (RSG places 500).
		idTexts := uuidx.TextArray(allIDs)
		var count int
		if err := s.store.Pool.QueryRow(r.Context(), `
			SELECT count(DISTINCT pid) FROM (
				SELECT player_id AS pid
				FROM registrations
				WHERE tournament_id = $1
				  AND status IN ('confirmed', 'pending', 'waitlist')
				  AND player_id = ANY($2::uuid[])
				UNION
				SELECT partner_id AS pid
				FROM registrations
				WHERE tournament_id = $1
				  AND partner_id IS NOT NULL
				  AND status IN ('confirmed', 'pending', 'waitlist')
				  AND partner_id = ANY($2::uuid[])
			) participants`,
			tournamentID, idTexts).Scan(&count); err != nil {
			log.Printf("tournament places: verify players: %v", err)
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось проверить игроков")
			return
		}
		if count != len(idTexts) {
			writeError(w, http.StatusBadRequest, "validation", "Игрок должен быть участником этого турнира")
			return
		}
	}

	tx, err := s.store.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить призёров")
		return
	}
	defer tx.Rollback(r.Context())

	if _, err := tx.Exec(r.Context(),
		`DELETE FROM tournament_places WHERE club_id = $1 AND tournament_id = $2`,
		clubID, tournamentID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить призёров")
		return
	}

	for _, place := range places {
		if len(place.PlayerIDs) == 0 {
			continue
		}
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO tournament_places (club_id, tournament_id, place, player_ids)
			VALUES ($1, $2, $3, $4::uuid[])`,
			clubID, tournamentID, place.Place, place.PlayerIDs); err != nil {
			log.Printf("tournament places: insert: %v", err)
			writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить призёров")
			return
		}
	}

	if _, err := tx.Exec(r.Context(),
		`UPDATE tournaments SET updated_at = now() WHERE id = $1`, tournamentID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить призёров")
		return
	}

	diff, _ := json.Marshal(map[string]any{"places": places})
	if _, err := s.store.Queries.WithTx(tx).InsertAuditLog(r.Context(), sqlc.InsertAuditLogParams{
		ActorID:   actor.ID,
		Action:    "tournament.places",
		Entity:    "tournament",
		EntityID:  pgtype.Text{String: uuidString(tournamentID), Valid: true},
		Diff:      diff,
		Ip:        auth.ClientIPAddr(r),
		UserAgent: auth.TextOrNull(r.UserAgent()),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить призёров")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось сохранить призёров")
		return
	}

	out := make([]tournamentPlaceJSON, 0, len(places))
	for _, place := range places {
		if len(place.PlayerIDs) > 0 {
			out = append(out, place)
		}
	}
	suggested, _ := s.suggestedTournamentPlaces(r.Context(), tour)
	writeJSON(w, http.StatusOK, map[string]any{
		"places":           out,
		"suggested_places": suggested,
		"places_mismatch":  rating.PodiumMismatch(placesToPodium(out), placesToPodium(suggested)),
	})
}
