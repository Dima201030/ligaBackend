package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Server wires HTTP routes for the API.
type Server struct {
	router chi.Router
}

// New creates an API server with the stage-2 route set.
func New() *Server {
	s := &Server{router: chi.NewRouter()}
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Recoverer)
	s.router.Get("/api/health", s.handleHealth)
	return s
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.router
}
