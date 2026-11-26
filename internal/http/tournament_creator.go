package httpapi

import (
	"context"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// ensureTournamentCreatedBy fills tournaments.created_by from audit_log when missing.
func (s *Server) ensureTournamentCreatedBy(ctx context.Context, tour *sqlc.Tournament) {
	if tour == nil || tour.CreatedBy.Valid {
		return
	}
	actorID, err := s.store.Queries.GetTournamentCreateActor(ctx, pgtype.Text{
		String: uuidString(tour.ID),
		Valid:  true,
	})
	if err != nil || !actorID.Valid {
		return
	}
	if err := s.store.Queries.SetTournamentCreatedByIfNull(ctx, sqlc.SetTournamentCreatedByIfNullParams{
		ID:        tour.ID,
		CreatedBy: actorID,
	}); err != nil {
		return
	}
	tour.CreatedBy = actorID
}

func (s *Server) ensureTournamentsCreatedBy(ctx context.Context, rows []sqlc.Tournament) {
	for i := range rows {
		s.ensureTournamentCreatedBy(ctx, &rows[i])
	}
}

func (s *Server) ensureCreatedByOnPlatformRows(ctx context.Context, rows []sqlc.ListPlatformPublicTournamentsRow) {
	for i := range rows {
		if rows[i].CreatedBy.Valid {
			continue
		}
		tour := platformRowToTournament(rows[i])
		s.ensureTournamentCreatedBy(ctx, &tour)
		rows[i].CreatedBy = tour.CreatedBy
	}
}

func (s *Server) attachTournamentCreators(ctx context.Context, items []tournamentJSON) {
	if len(items) == 0 {
		return
	}
	ids := make([]pgtype.UUID, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.CreatedByID == "" {
			continue
		}
		if _, ok := seen[item.CreatedByID]; ok {
			continue
		}
		id, err := parseUUID(item.CreatedByID)
		if err != nil {
			continue
		}
		seen[item.CreatedByID] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return
	}
	rows, err := s.store.Queries.ListUserContactBriefsByIDs(ctx, ids)
	if err != nil {
		return
	}
	byID := make(map[string]creatorJSON, len(rows))
	for _, row := range rows {
		c := creatorJSON{
			ID:        uuidString(row.ID),
			FirstName: row.FirstName,
			LastName:  row.LastName,
		}
		if row.Email.Valid && row.Email.String != "" {
			email := row.Email.String
			c.Email = &email
		}
		byID[c.ID] = c
	}
	for i := range items {
		if c, ok := byID[items[i].CreatedByID]; ok {
			cp := c
			items[i].CreatedBy = &cp
		}
	}
}

func (s *Server) withTournamentCreator(ctx context.Context, tj tournamentJSON) tournamentJSON {
	items := []tournamentJSON{tj}
	s.attachTournamentCreators(ctx, items)
	return items[0]
}
