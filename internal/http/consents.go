package httpapi

import (
	"net/http"
	"strings"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// ConsentDocumentVersion is the published text version recorded with each grant.
const ConsentDocumentVersion = "2026-11-26"

func (s *Server) recordConsent(
	r *http.Request,
	q *sqlc.Queries,
	userID pgtype.UUID,
	kind string,
	granted bool,
) error {
	ip := remoteIPString(r)
	ua := strings.TrimSpace(r.UserAgent())
	var ipText, uaText pgtype.Text
	if ip != "" {
		ipText = pgtype.Text{String: ip, Valid: true}
	}
	if ua != "" {
		uaText = pgtype.Text{String: ua, Valid: true}
	}
	_, err := q.CreateUserConsent(r.Context(), sqlc.CreateUserConsentParams{
		UserID:    userID,
		Kind:      kind,
		Version:   ConsentDocumentVersion,
		Granted:   granted,
		Ip:        ipText,
		UserAgent: uaText,
	})
	return err
}

func (s *Server) handleListMyConsents(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
		return
	}
	rows, err := s.store.Queries.ListUserConsents(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить согласия")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := map[string]any{
			"id":         uuidString(row.ID),
			"kind":       row.Kind,
			"version":    row.Version,
			"granted":    row.Granted,
			"created_at": formatTS(row.CreatedAt),
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"consents": out,
		"document_version": ConsentDocumentVersion,
	})
}
