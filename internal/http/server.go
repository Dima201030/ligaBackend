package httpapi

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed status.html
var statusFS embed.FS

// Server wires HTTP routes for the API.
type Server struct {
	router chi.Router
	store  *db.Store
}

// New creates an API server. store may be nil in unit tests (DB check → unknown).
func New(store *db.Store) *Server {
	s := &Server{
		router: chi.NewRouter(),
		store:  store,
	}
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Recoverer)
	s.router.Get("/api/health", s.handleHealth)
	s.router.Get("/status", s.handleStatus)
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
