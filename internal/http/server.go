package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"time"

	"log"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/config"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/files"
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
	files   *files.DiskStore
}

// Options configures the HTTP server.
type Options struct {
	Store *db.Store
	Cfg   config.Config
	Files *files.DiskStore
}

// New creates an API server. store may be nil in unit tests (DB check → unknown).
func New(store *db.Store) *Server {
	return NewWithOptions(Options{Store: store, Cfg: config.Config{
		AppEnv: "development",
	}})
}

// NewWithOptions creates an API server with explicit configuration.
func NewWithOptions(opts Options) *Server {
	fileStore := opts.Files
	if fileStore == nil && opts.Cfg.UploadsDir != "" {
		fs, err := files.NewDiskStore(opts.Cfg.UploadsDir, opts.Cfg.MaxUploadBytes)
		if err != nil {
			log.Printf("uploads: disabled (%v)", err)
		} else {
			fileStore = fs
		}
	}
	s := &Server{
		router:  chi.NewRouter(),
		store:   opts.Store,
		cfg:     opts.Cfg,
		limiter: auth.NewRateLimiter(5, 15*time.Minute),
		files:   fileStore,
	}
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Recoverer)
	s.router.Use(s.withSession)
	s.router.Use(s.withClub)

	s.router.Get("/api/health", s.handleHealth)
	s.router.Get("/status", s.handleStatus)

	s.router.Get("/api/clubs", s.handleListClubs)
	s.router.Get("/api/clubs/{slug}", s.handleGetClub)

	s.router.Route("/api/auth", func(r chi.Router) {
		r.Post("/register", s.handleRegister)
		r.Post("/login", s.handleLogin)
		r.Post("/logout", s.handleLogout)
		r.Get("/confirm-email", s.handleConfirmEmail)
		r.Post("/confirm-email", s.handleConfirmEmail)
		r.Post("/resend-confirm", s.handleResendConfirm)
		r.Post("/forgot-password", s.handleForgotPassword)
		r.Post("/reset-password", s.handleResetPassword)
		r.With(s.requireAuth).Post("/sso/issue", s.handleSSOIssue)
		r.Get("/sso/consume", s.handleSSOConsume)
		r.With(s.requireAuth).Get("/me", s.handleMe)
		r.With(s.requireAuth).Get("/me/clubs", s.handleMyClubs)
		r.With(s.requireAuth, s.requireApproved).Get("/me/registrations", s.handleMyRegistrations)
		r.With(s.requireAuth, s.requireApproved).Get("/me/matches", s.handleMyMatches)
	})

	s.router.Get("/api/tournaments", s.handleListPublicTournaments)
	s.router.Get("/api/tournaments/{slug}", s.handleGetPublicTournament)
	s.router.Get("/api/tournaments/{slug}/bracket", s.handleGetPublicBracket)
	s.router.Get("/api/tournaments/{slug}/matches", s.handleGetPublicTournamentMatches)
	s.router.With(s.requireAuth, s.requireApproved, s.requireProfileVerified).Post("/api/tournaments/{slug}/registrations", s.handleCreateRegistration)
	s.router.With(s.requireAuth, s.requireApproved).Delete("/api/tournaments/{slug}/registrations/me", s.handleCancelRegistration)

	s.router.Get("/api/players", s.handleListPublicPlayers)
	s.router.Get("/api/players/{slug}", s.handleGetPublicPlayer)
	s.router.Get("/api/players/{slug}/rating-events", s.handlePlayerRatingEvents)
	s.router.Get("/api/rankings", s.handleListRankings)

	s.router.Get("/api/news", s.handleListPublicNews)
	s.router.Get("/api/news/{slug}", s.handleGetPublicNews)
	s.router.Get("/api/events", s.handleListPublicEvents)
	s.router.Get("/api/events/{slug}", s.handleGetPublicEvent)
	s.router.Get("/api/pages", s.handleListPages)
	s.router.Get("/api/pages/{slug}", s.handleGetPage)
	s.router.Get("/api/files/{id}", s.handleGetFile)

	s.router.Route("/api/organizer", func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Use(s.requireOrganizer)
		r.Post("/clubs", s.handleOnboardClub)
		r.Get("/applications", s.handleListApplications)
		r.Post("/applications/{id}/approve", s.handleApproveApplication)
		r.Post("/applications/{id}/reject", s.handleRejectApplication)
		r.Get("/tournaments", s.handleListOrganizerTournaments)
		r.Post("/tournaments", s.handleCreateTournament)
		r.Get("/tournaments/{id}", s.handleGetOrganizerTournament)
		r.Patch("/tournaments/{id}", s.handleUpdateTournament)
		r.Post("/tournaments/{id}/registrations", s.handleOrganizerAddRegistration)
		r.Patch("/tournaments/{id}/registrations/{regId}", s.handleUpdateOrganizerRegistrationStatus)
		r.Get("/tournaments/{id}/bracket", s.handleGetOrganizerBracket)
		r.Post("/tournaments/{id}/bracket/generate", s.handleGenerateBracket)
		r.Post("/tournaments/{id}/bracket/publish", s.handlePublishBracket)
		r.Get("/tournaments/{id}/matches", s.handleListOrganizerTournamentMatches)
		r.Post("/tournaments/{id}/ratings/apply", s.handleApplyTournamentRatings)
		r.Get("/matches", s.handleListOrganizerMatches)
		r.Patch("/matches/{id}", s.handleUpdateMatchSchedule)
		r.Post("/matches/{id}/score", s.handleSubmitScore)
		r.Post("/matches/{id}/walkover", s.handleWalkover)
		r.Post("/matches/{id}/retire", s.handleRetireMatch)
		r.Post("/matches/{id}/disqualify", s.handleDisqualifyMatch)
		r.Post("/matches/{id}/cancel", s.handleCancelMatch)
		r.Get("/players", s.handleListOrganizerPlayers)
		r.Post("/players", s.handleCreatePlaceholderPlayer)
		r.Get("/players/placeholders", s.handleListPlaceholderPlayers)
		r.Patch("/players/{id}", s.handleUpdateOrganizerPlayer)
		r.Post("/players/{id}/verify", s.handleConfirmPlayerVerification)
		r.Post("/players/{id}/merge", s.handleMergePlaceholderPlayer)
		r.Post("/ratings/manual", s.handleManualRating)
		r.Post("/ratings/snapshot", s.handleRatingSnapshot)
		r.Get("/news", s.handleListOrganizerNews)
		r.Post("/news", s.handleCreateNews)
		r.Patch("/news/{id}", s.handleUpdateNews)
		r.Get("/events", s.handleListOrganizerEvents)
		r.Post("/events", s.handleCreateEvent)
		r.Patch("/events/{id}", s.handleUpdateEvent)
		r.Put("/pages/{slug}", s.handleUpsertPage)
		r.Post("/files", s.handleUploadFile)
		r.Post("/tournaments/{id}/cover", s.handleAttachTournamentCover)
		r.Post("/news/{id}/cover", s.handleAttachNewsCover)
		r.Post("/events/{id}/cover", s.handleAttachEventCover)
		r.Post("/players/{id}/avatar", s.handleAttachPlayerAvatar)
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
