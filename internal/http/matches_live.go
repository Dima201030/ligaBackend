package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

const (
	livePollInterval   = 2 * time.Second
	liveHeartbeatEvery = 15 * time.Second
	liveStreamMaxAge   = 25 * time.Minute
)

type liveSyncPayload struct {
	Revision         string      `json:"revision"`
	Unchanged        bool        `json:"unchanged"`
	TournamentStatus string      `json:"tournament_status"`
	BracketStatus    string      `json:"bracket_status"`
	Matches          []matchJSON `json:"matches,omitempty"`
}

func (s *Server) handleSyncPublicTournamentMatches(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	tour, err := s.store.Queries.GetTournamentBySlug(r.Context(), sqlc.GetTournamentBySlugParams{
		ClubID: s.currentClubID(r),
		Slug:   slug,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить матчи")
		return
	}
	user := auth.UserFromContext(r.Context())
	isOrg := user != nil && auth.IsOrganizerOrAbove(user.Role)
	s.writeLiveSync(w, r, tour, r.URL.Query().Get("revision"), isOrg)
}

func (s *Server) handleSyncOrganizerTournamentMatches(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}
	tour, err := s.loadClubTournament(r, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить матчи")
		return
	}
	s.writeLiveSync(w, r, tour, r.URL.Query().Get("revision"), true)
}

func (s *Server) handleStreamPublicTournamentMatches(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	tour, err := s.store.Queries.GetTournamentBySlug(r.Context(), sqlc.GetTournamentBySlugParams{
		ClubID: s.currentClubID(r),
		Slug:   slug,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось открыть поток матчей")
		return
	}
	user := auth.UserFromContext(r.Context())
	isOrg := user != nil && auth.IsOrganizerOrAbove(user.Role)
	s.streamLiveMatches(w, r, tour, isOrg)
}

func (s *Server) handleStreamOrganizerTournamentMatches(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "validation", "Некорректный идентификатор")
		return
	}
	tour, err := s.loadClubTournament(r, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Турнир не найден")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось открыть поток матчей")
		return
	}
	s.streamLiveMatches(w, r, tour, true)
}

func (s *Server) writeLiveSync(w http.ResponseWriter, r *http.Request, tour sqlc.Tournament, since string, allowUnpublished bool) {
	rev, err := s.liveRevision(r.Context(), tour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось проверить обновления")
		return
	}
	payload := liveSyncPayload{
		Revision:         rev,
		TournamentStatus: string(tour.Status),
		BracketStatus:    string(tour.BracketStatus),
	}
	if since != "" && since == rev {
		payload.Unchanged = true
		writeJSON(w, http.StatusOK, payload)
		return
	}
	if !allowUnpublished && tour.BracketStatus != sqlc.BracketStatusPublished {
		payload.Matches = []matchJSON{}
		writeJSON(w, http.StatusOK, payload)
		return
	}
	list, err := s.loadTournamentMatches(r, tour.ID, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить матчи")
		return
	}
	payload.Matches = list
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) streamLiveMatches(w http.ResponseWriter, r *http.Request, tour sqlc.Tournament, allowUnpublished bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "Поток матчей недоступен")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	deadline := time.Now().Add(liveStreamMaxAge)
	ticker := time.NewTicker(livePollInterval)
	defer ticker.Stop()
	heartbeat := time.NewTicker(liveHeartbeatEvery)
	defer heartbeat.Stop()

	lastRev := ""
	sendSnapshot := func() error {
		fresh, err := s.store.Queries.GetTournamentByID(ctx, tour.ID)
		if err != nil {
			return err
		}
		tour = fresh
		rev, err := s.liveRevision(ctx, tour)
		if err != nil {
			return err
		}
		if rev == lastRev {
			return nil
		}
		payload := liveSyncPayload{
			Revision:         rev,
			TournamentStatus: string(tour.Status),
			BracketStatus:    string(tour.BracketStatus),
		}
		if !allowUnpublished && tour.BracketStatus != sqlc.BracketStatusPublished {
			payload.Matches = []matchJSON{}
		} else {
			list, loadErr := s.loadTournamentMatches(r.WithContext(ctx), tour.ID, false)
			if loadErr != nil {
				return loadErr
			}
			payload.Matches = list
		}
		event := "snapshot"
		if lastRev != "" {
			event = "update"
		}
		if err := writeSSE(w, flusher, event, payload); err != nil {
			return err
		}
		lastRev = rev
		return nil
	}

	if err := sendSnapshot(); err != nil {
		return
	}

	for {
		if time.Now().After(deadline) {
			_ = writeSSE(w, flusher, "reconnect", map[string]string{"reason": "max_age"})
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": ping %d\n\n", time.Now().Unix()); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if err := sendSnapshot(); err != nil {
				return
			}
		}
	}
}

func (s *Server) liveRevision(ctx context.Context, tour sqlc.Tournament) (string, error) {
	fp, err := s.store.Queries.GetTournamentMatchesFingerprint(ctx, tour.ID)
	if err != nil {
		return "", err
	}
	ns := int64(0)
	if fp.MaxUpdatedAt.Valid {
		ns = fp.MaxUpdatedAt.Time.UTC().UnixNano()
	}
	tourNS := int64(0)
	if tour.UpdatedAt.Valid {
		tourNS = tour.UpdatedAt.Time.UTC().UnixNano()
	}
	return fmt.Sprintf(
		"%s:%s:%d:%d:%d:%d:%d",
		tour.Status,
		tour.BracketStatus,
		tour.BracketVersion,
		fp.MatchCount,
		fp.VersionSum,
		ns,
		tourNS,
	), nil
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
