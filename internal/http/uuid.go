package httpapi

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	b := id.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func parseUUID(s string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
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
