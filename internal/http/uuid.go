package httpapi

import (
	"strings"

	"github.com/Dima201030/ligaBackend/internal/uuidx"
	"github.com/jackc/pgx/v5/pgtype"
)

func uuidString(id pgtype.UUID) string {
	return uuidx.String(id)
}

func parseUUID(s string) (pgtype.UUID, error) {
	return uuidx.Parse(s)
}

func textEmail(s string) pgtype.Text {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}

func emailString(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}
