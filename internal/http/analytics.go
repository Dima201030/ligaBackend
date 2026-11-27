package httpapi

import (
	"net/http"
	"time"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Server) handleOrganizerAnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clubID := s.currentClubID(r)
	clubSlug, clubName := s.currentClubMeta(r)

	playersApproved, err := s.store.Queries.CountClubApprovedActivePlayers(ctx, clubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить аналитику")
		return
	}
	playersPending, err := s.store.Queries.CountClubPendingApplications(ctx, clubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить аналитику")
		return
	}
	placeholders, err := s.store.Queries.CountClubPlaceholderPlayers(ctx, clubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить аналитику")
		return
	}
	membersActive, err := s.store.Queries.CountClubActiveMembers(ctx, clubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить аналитику")
		return
	}

	tournamentsTotal, err := s.store.Queries.CountClubTournaments(ctx, clubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить аналитику")
		return
	}
	tournamentRows, err := s.store.Queries.CountClubTournamentsByStatus(ctx, clubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить аналитику")
		return
	}
	registrationRows, err := s.store.Queries.CountClubRegistrationsByStatus(ctx, clubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить аналитику")
		return
	}
	matchRows, err := s.store.Queries.CountClubMatchesByStatus(ctx, clubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить аналитику")
		return
	}

	activeNonTerminal, err := s.store.Queries.CountActiveTournamentsByClub(ctx, clubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить аналитику")
		return
	}

	since := time.Now().UTC().AddDate(0, -11, 0)
	since = time.Date(since.Year(), since.Month(), 1, 0, 0, 0, 0, time.UTC)
	sinceTS := pgtype.Timestamptz{Time: since, Valid: true}

	playerGrowth, err := s.store.Queries.ListClubPlayerGrowthByMonth(ctx, sqlc.ListClubPlayerGrowthByMonthParams{
		ClubID:    clubID,
		CreatedAt: sinceTS,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить аналитику")
		return
	}
	registrationGrowth, err := s.store.Queries.ListClubRegistrationGrowthByMonth(ctx, sqlc.ListClubRegistrationGrowthByMonthParams{
		ClubID:    clubID,
		CreatedAt: sinceTS,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить аналитику")
		return
	}
	matchGrowth, err := s.store.Queries.ListClubCompletedMatchGrowthByMonth(ctx, sqlc.ListClubCompletedMatchGrowthByMonthParams{
		ClubID:    clubID,
		UpdatedAt: sinceTS,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить аналитику")
		return
	}

	tournamentByStatus := statusCountMap(tournamentRows, func(r sqlc.CountClubTournamentsByStatusRow) (string, int32) {
		return r.Status, r.Count
	})
	registrationByStatus := statusCountMap(registrationRows, func(r sqlc.CountClubRegistrationsByStatusRow) (string, int32) {
		return r.Status, r.Count
	})
	matchByStatus := statusCountMap(matchRows, func(r sqlc.CountClubMatchesByStatusRow) (string, int32) {
		return r.Status, r.Count
	})

	completedMatches := matchByStatus["completed"] + matchByStatus["walkover"] + matchByStatus["disqualified"]
	openPlayable := matchByStatus["scheduled"] + matchByStatus["live"]

	writeJSON(w, http.StatusOK, map[string]any{
		"club_id":   uuidString(clubID),
		"club_slug": clubSlug,
		"club_name": clubName,
		"players": map[string]any{
			"approved_active":      playersApproved,
			"pending_applications": playersPending,
			"placeholders":         placeholders,
			"members_active":       membersActive,
		},
		"tournaments": map[string]any{
			"total":               tournamentsTotal,
			"by_status":           tournamentByStatus,
			"active_non_terminal": activeNonTerminal,
		},
		"registrations": map[string]any{
			"by_status": registrationByStatus,
			"pending":   registrationByStatus["pending"],
			"confirmed": registrationByStatus["confirmed"],
			"waitlist":  registrationByStatus["waitlist"],
			"cancelled": registrationByStatus["cancelled"],
			"rejected":  registrationByStatus["rejected"],
		},
		"matches": map[string]any{
			"by_status":     matchByStatus,
			"scheduled":     matchByStatus["scheduled"],
			"live":          matchByStatus["live"],
			"completed":     completedMatches,
			"open_playable": openPlayable,
		},
		"growth": map[string]any{
			"since":         since.Format("2006-01"),
			"players":       monthSeries(playerGrowth, func(r sqlc.ListClubPlayerGrowthByMonthRow) (string, int32) { return r.Month, r.Count }),
			"registrations": monthSeries(registrationGrowth, func(r sqlc.ListClubRegistrationGrowthByMonthRow) (string, int32) { return r.Month, r.Count }),
			"matches":       monthSeries(matchGrowth, func(r sqlc.ListClubCompletedMatchGrowthByMonthRow) (string, int32) { return r.Month, r.Count }),
		},
	})
}

func statusCountMap[T any](rows []T, pick func(T) (string, int32)) map[string]int32 {
	out := map[string]int32{}
	for _, row := range rows {
		status, count := pick(row)
		out[status] = count
	}
	return out
}

func monthSeries[T any](rows []T, pick func(T) (string, int32)) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		month, count := pick(row)
		out = append(out, map[string]any{"month": month, "count": count})
	}
	return out
}
