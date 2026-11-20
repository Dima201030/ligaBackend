package httpapi

import (
	"net/http"

	"github.com/Dima201030/ligaBackend/internal/auth"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
)

func (s *Server) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.cfg.SessionCookieName())
		if err == nil && cookie.Value != "" && s.store != nil {
			user, loadErr := auth.LoadSessionUser(r.Context(), s.store, cookie.Value)
			if loadErr == nil && user != nil {
				r = r.WithContext(auth.SetUser(r.Context(), user))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.UserFromContext(r.Context()) == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireOrganizer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
			return
		}
		if !auth.IsOrganizerOrAbove(user.Role) {
			writeError(w, http.StatusForbidden, "forbidden", "Недостаточно прав")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireApproved(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Требуется вход")
			return
		}
		if user.Status != sqlc.UserStatusApproved {
			writeError(w, http.StatusForbidden, "forbidden", "Заявка доступна только подтверждённым игрокам")
			return
		}
		next.ServeHTTP(w, r)
	})
}
