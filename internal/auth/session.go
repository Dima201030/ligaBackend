package auth

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/Dima201030/ligaBackend/internal/crypto"
	"github.com/Dima201030/ligaBackend/internal/db"
	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// Absolute and idle TTLs match so a stored cookie remains valid for the full
	// browser lifetime as long as the user returns within that window.
	SessionAbsoluteTTL = 30 * 24 * time.Hour
	SessionIdleTTL     = 30 * 24 * time.Hour
	SessionTokenBytes  = 32
)

type contextKey string

const userContextKey contextKey = "auth_user"

// SessionUser is the authenticated principal attached to the request context.
type SessionUser struct {
	ID              pgtype.UUID
	Email           string
	FirstName       string
	LastName        string
	Role            sqlc.UserRole
	Status          sqlc.UserStatus
	EmailVerifiedAt pgtype.Timestamptz
	Level           string
	Phone           *string
	SessionID       pgtype.UUID
}

// CreateSessionToken issues a new session cookie value and persists only its hash.
func CreateSessionToken(
	ctx context.Context,
	store *db.Store,
	userID pgtype.UUID,
	r *http.Request,
) (token string, session sqlc.Session, err error) {
	raw, err := crypto.RandomBytes(SessionTokenBytes)
	if err != nil {
		return "", sqlc.Session{}, err
	}
	sum := crypto.HashToken(raw)
	now := time.Now().UTC()
	sess, err := store.Queries.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:        userID,
		TokenHash:     sum[:],
		ExpiresAt:     pgtype.Timestamptz{Time: now.Add(SessionAbsoluteTTL), Valid: true},
		IdleExpiresAt: pgtype.Timestamptz{Time: now.Add(SessionIdleTTL), Valid: true},
		Ip:            clientIP(r),
		UserAgent:     textOrNull(r.UserAgent()),
	})
	if err != nil {
		return "", sqlc.Session{}, err
	}
	return base64.RawURLEncoding.EncodeToString(raw), sess, nil
}

// LoadSessionUser resolves a cookie token to an active user session.
func LoadSessionUser(ctx context.Context, store *db.Store, token string) (*SessionUser, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != SessionTokenBytes {
		return nil, pgx.ErrNoRows
	}
	sum := crypto.HashToken(raw)
	sess, err := store.Queries.GetSessionByTokenHash(ctx, sum[:])
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if sess.RevokedAt.Valid {
		return nil, pgx.ErrNoRows
	}
	if !sess.ExpiresAt.Valid || !sess.ExpiresAt.Time.After(now) {
		return nil, pgx.ErrNoRows
	}
	if !sess.IdleExpiresAt.Valid || !sess.IdleExpiresAt.Time.After(now) {
		return nil, pgx.ErrNoRows
	}

	user, err := store.Queries.GetUserByID(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}
	profile, err := store.Queries.GetProfileByUserID(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}

	_, _ = store.Queries.TouchSession(ctx, sqlc.TouchSessionParams{
		ID:            sess.ID,
		IdleExpiresAt: pgtype.Timestamptz{Time: now.Add(SessionIdleTTL), Valid: true},
	})

	var phone *string
	if profile.Phone.Valid {
		p := profile.Phone.String
		phone = &p
	}
	return &SessionUser{
		ID:              user.ID,
		Email:           user.Email,
		FirstName:       user.FirstName,
		LastName:        user.LastName,
		Role:            user.Role,
		Status:          user.Status,
		EmailVerifiedAt: user.EmailVerifiedAt,
		Level:           profile.Level,
		Phone:           phone,
		SessionID:       sess.ID,
	}, nil
}

// SetUser stores the session user on the request context.
func SetUser(ctx context.Context, user *SessionUser) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext returns the authenticated user, if any.
func UserFromContext(ctx context.Context) *SessionUser {
	user, _ := ctx.Value(userContextKey).(*SessionUser)
	return user
}

// IsOrganizerOrAbove reports whether the role may manage applications.
func IsOrganizerOrAbove(role sqlc.UserRole) bool {
	switch role {
	case sqlc.UserRoleOrganizer, sqlc.UserRoleOwner, sqlc.UserRoleSuperadmin:
		return true
	default:
		return false
	}
}

func clientIP(r *http.Request) *netip.Addr {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	if xff := r.Header.Get("X-Real-IP"); xff != "" {
		host = strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return nil
	}
	return &addr
}

func textOrNull(s string) pgtype.Text {
	s = strings.TrimSpace(s)
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// ClientIPAddr exports client IP parsing for handlers.
func ClientIPAddr(r *http.Request) *netip.Addr {
	return clientIP(r)
}

// TextOrNull exports nullable text helper for handlers.
func TextOrNull(s string) pgtype.Text {
	return textOrNull(s)
}
