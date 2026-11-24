package matches

import (
	"context"
	"encoding/json"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/Dima201030/ligaBackend/internal/rating"
	"github.com/Dima201030/ligaBackend/internal/uuidx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// maybeAutoFillMissingPlaces inserts algorithm places only for empty podium slots.
// Never overwrites an existing tournament_places row (Winter Cup manual data stays).
// Skips any suggested player already present on another saved place.
func maybeAutoFillMissingPlaces(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, tour sqlc.Tournament) error {
	matches, err := q.ListMatchesByTournament(ctx, tour.ID)
	if err != nil {
		return err
	}
	sides, err := q.ListMatchSidesByTournament(ctx, tour.ID)
	if err != nil {
		return err
	}
	suggested := rating.DerivePodiumPlaces(tour.Format, matches, sides)
	if len(suggested) == 0 {
		return nil
	}

	existing, err := loadPlacePlayerSets(ctx, tx, tour.ClubID, tour.ID)
	if err != nil {
		return err
	}

	taken := map[string]struct{}{}
	for _, ids := range existing {
		for _, id := range ids {
			taken[id] = struct{}{}
		}
	}

	for _, slot := range suggested {
		if _, occupied := existing[slot.Place]; occupied {
			continue
		}
		ids := make([]string, 0, len(slot.PlayerIDs))
		for _, id := range slot.PlayerIDs {
			key := uuidx.String(id)
			if key == "" {
				continue
			}
			if _, used := taken[key]; used {
				continue
			}
			ids = append(ids, key)
			taken[key] = struct{}{}
		}
		if len(ids) == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO tournament_places (club_id, tournament_id, place, player_ids)
			VALUES ($1, $2, $3, $4::uuid[])
			ON CONFLICT (tournament_id, place) DO NOTHING`,
			tour.ClubID, tour.ID, slot.Place, ids); err != nil {
			return err
		}
	}
	return nil
}

func loadPlacePlayerSets(ctx context.Context, tx pgx.Tx, clubID, tournamentID pgtype.UUID) (map[int][]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT place, array_to_json(player_ids)::text
		FROM tournament_places
		WHERE club_id = $1 AND tournament_id = $2`, clubID, tournamentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int][]string{}
	for rows.Next() {
		var place int16
		var raw string
		if err := rows.Scan(&place, &raw); err != nil {
			return nil, err
		}
		ids := uuidStringsFromJSONArray(raw)
		out[int(place)] = ids
	}
	return out, rows.Err()
}

func uuidStringsFromJSONArray(raw string) []string {
	if raw == "" || raw == "null" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil
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
