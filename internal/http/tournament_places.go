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

// listTournamentPlaces returns podium places for one tournament, ordered 1..3.
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

	out := make([]tournamentPlaceJSON, 0, 3)
	for rows.Next() {
		var place int16
		var raw string
		if err := rows.Scan(&place, &raw); err != nil {
			return nil, err
		}
		var ids []string
		if raw != "" {
			_ = json.Unmarshal([]byte(raw), &ids)
		}
		if ids == nil {
			ids = []string{}
		}
		out = append(out, tournamentPlaceJSON{Place: int(place), PlayerIDs: ids})
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
		var ids []string
		if raw != "" {
			_ = json.Unmarshal([]byte(raw), &ids)
		}
		if ids == nil {
			ids = []string{}
		}
		key := uuidString(id)
		out[key] = append(out[key], tournamentPlaceJSON{Place: int(place), PlayerIDs: ids})
	}
	return out, rows.Err()
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
	if _, err := s.loadClubTournament(r, tournamentID); err != nil {
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
	if len(req.Places) > 3 {
		writeError(w, http.StatusBadRequest, "validation", "Можно указать не больше трёх мест")
		return
	}

	seen := map[int]bool{}
	places := make([]tournamentPlaceJSON, 0, len(req.Places))
	allIDs := make([]pgtype.UUID, 0, 6)
	for _, entry := range req.Places {
		if entry.Place < 1 || entry.Place > 3 {
			writeError(w, http.StatusBadRequest, "validation", "Место должно быть от 1 до 3")
			return
		}
		if seen[entry.Place] {
			writeError(w, http.StatusBadRequest, "validation", "Место указано дважды")
			return
		}
		if len(entry.PlayerIDs) > 2 {
			writeError(w, http.StatusBadRequest, "validation", "На одном месте не больше двух игроков")
			return
		}
		seen[entry.Place] = true
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
	writeJSON(w, http.StatusOK, map[string]any{"places": out})
}
