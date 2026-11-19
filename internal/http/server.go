package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"time"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed status.html
var statusFS embed.FS

// Server wires HTTP routes for the API.
type Server struct {
	router  chi.Router
	store   *db.Store
	cfg     config.Config
	limiter *auth.RateLimiter
}

// Options configures the HTTP server.
type Options struct {
	Store *db.Store
	Cfg   config.Config
}

// New creates an API server. store may be nil in unit tests (DB check → unknown).
func New(store *db.Store) *Server {
	return NewWithOptions(Options{Store: store, Cfg: config.Config{
		AppEnv: "development",
	}})
}

// NewWithOptions creates an API server with explicit configuration.
func NewWithOptions(opts Options) *Server {
	s := &Server{
		router:  chi.NewRouter(),
		store:   opts.Store,
		cfg:     opts.Cfg,
		limiter: auth.NewRateLimiter(5, 15*time.Minute),
	}
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Recoverer)
	s.router.Use(s.withSession)

	s.router.Get("/api/health", s.handleHealth)
	s.router.Get("/status", s.handleStatus)

	s.router.Route("/api/auth", func(r chi.Router) {
		r.Post("/register", s.handleRegister)
		r.Post("/login", s.handleLogin)
		r.Post("/logout", s.handleLogout)
		r.Get("/confirm-email", s.handleConfirmEmail)
		r.Post("/confirm-email", s.handleConfirmEmail)
		r.With(s.requireAuth).Get("/me", s.handleMe)
	})

	s.router.Get("/api/tournaments", s.handleListPublicTournaments)
	s.router.Get("/api/tournaments/{slug}", s.handleGetPublicTournament)
	s.router.Get("/api/tournaments/{slug}/bracket", s.handleGetPublicBracket)
	s.router.Get("/api/tournaments/{slug}/matches", s.handleGetPublicTournamentMatches)
	s.router.With(s.requireAuth, s.requireApproved).Post("/api/tournaments/{slug}/registrations", s.handleCreateRegistration)
	s.router.With(s.requireAuth, s.requireApproved).Delete("/api/tournaments/{slug}/registrations/me", s.handleCancelRegistration)

	s.router.Get("/api/players", s.handleListPublicPlayers)
	s.router.Get("/api/players/{slug}", s.handleGetPublicPlayer)
	s.router.Get("/api/players/{slug}/rating-events", s.handlePlayerRatingEvents)
	s.router.Get("/api/rankings", s.handleListRankings)

	s.router.Route("/api/organizer", func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Use(s.requireOrganizer)
		r.Get("/applications", s.handleListApplications)
		r.Post("/applications/{id}/approve", s.handleApproveApplication)
		r.Post("/applications/{id}/reject", s.handleRejectApplication)
		r.Get("/tournaments", s.handleListOrganizerTournaments)
		r.Post("/tournaments", s.handleCreateTournament)
		r.Get("/tournaments/{id}", s.handleGetOrganizerTournament)
		r.Patch("/tournaments/{id}", s.handleUpdateTournament)
		r.Get("/tournaments/{id}/bracket", s.handleGetOrganizerBracket)
		r.Post("/tournaments/{id}/bracket/generate", s.handleGenerateBracket)
		r.Post("/tournaments/{id}/bracket/publish", s.handlePublishBracket)
		r.Get("/tournaments/{id}/matches", s.handleListOrganizerTournamentMatches)
		r.Post("/tournaments/{id}/ratings/apply", s.handleApplyTournamentRatings)
		r.Get("/matches", s.handleListOrganizerMatches)
		r.Patch("/matches/{id}", s.handleUpdateMatchSchedule)
		r.Post("/matches/{id}/score", s.handleSubmitScore)
		r.Post("/matches/{id}/walkover", s.handleWalkover)
		r.Post("/matches/{id}/cancel", s.handleCancelMatch)
		r.Get("/players", s.handleListOrganizerPlayers)
		r.Patch("/players/{id}", s.handleUpdateOrganizerPlayer)
		r.Post("/ratings/manual", s.handleManualRating)
		r.Post("/ratings/snapshot", s.handleRatingSnapshot)
	})

	return s
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(statusFS, "status.html")
	if err != nil {
		http.Error(w, "status page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.router
}
