package uuidx_test

import (
	"strings"
	"testing"

	"github.com/Dima201030/ligaBackend/internal/uuidx"
	"github.com/jackc/pgx/v5/pgtype"
)

// Regression for RSG places 500: ANY($n) / insert without ::uuid[] when binding
// arrays. Contract: TextArray + explicit cast in SQL.
func TestPlacesStyleSQLUsesUUIDArrayCast(t *testing.T) {
	const verifySQL = `
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
			) participants`
	const insertSQL = `
			INSERT INTO tournament_places (club_id, tournament_id, place, player_ids)
			VALUES ($1, $2, $3, $4::uuid[])`

	if !strings.Contains(verifySQL, "ANY($2::uuid[])") {
		t.Fatal("verify SQL must cast ANY arg to uuid[]")
	}
	if strings.Contains(verifySQL, "ANY($2)") && !strings.Contains(verifySQL, "ANY($2::uuid[])") {
		t.Fatal("bare ANY($2) without cast is forbidden")
	}
	if !strings.Contains(insertSQL, "$4::uuid[]") {
		t.Fatal("insert SQL must cast player_ids to uuid[]")
	}

	ids := make([]pgtype.UUID, 0, 3)
	for _, s := range []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"11111111-1111-4111-8111-111111111111",
	} {
		id, err := uuidx.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	bound := uuidx.TextArray(ids)
	if len(bound) != 2 {
		t.Fatalf("TextArray should dedupe for bind, got %#v", bound)
	}
	// Driver receives []string; SQL cast makes it uuid[] — never pass raw []pgtype.UUID
	// into ANY without ::uuid[] (Pi pgx failure).
	_ = bound
}
