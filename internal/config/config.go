package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds process configuration loaded from the environment.
type Config struct {
	HTTPAddr                 string
	DatabaseURL              string
	AppEncryptionKey         []byte
	AppEnv                   string
	CookieSecure             bool
	RequireEmailVerification bool
	PublicBaseURL            string
	FrontBaseURL             string
	SMTPHost                 string
	SMTPPort                 int
	SMTPUser                 string
	SMTPPassword             string
	SMTPFrom                 string
	UploadsDir               string
	MaxUploadBytes           int64
	DefaultClubSlug          string
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:                 envOr("HTTP_ADDR", "0.0.0.0:8080"),
		DatabaseURL:              strings.TrimSpace(os.Getenv("DATABASE_URL")),
		AppEnv:                   strings.ToLower(envOr("APP_ENV", "development")),
		RequireEmailVerification: envBool("REQUIRE_EMAIL_VERIFICATION", false),
		PublicBaseURL:            strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")), "/"),
		FrontBaseURL:             strings.TrimRight(strings.TrimSpace(os.Getenv("FRONT_BASE_URL")), "/"),
		SMTPHost:                 strings.TrimSpace(os.Getenv("SMTP_HOST")),
		SMTPUser:                 strings.TrimSpace(os.Getenv("SMTP_USER")),
		SMTPPassword:             os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:                 strings.TrimSpace(os.Getenv("SMTP_FROM")),
	}

	keyB64 := strings.TrimSpace(os.Getenv("APP_ENCRYPTION_KEY"))
	if keyB64 == "" {
		return Config{}, fmt.Errorf("APP_ENCRYPTION_KEY is required")
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return Config{}, fmt.Errorf("APP_ENCRYPTION_KEY must be base64: %w", err)
	}
	if len(key) != 32 {
		return Config{}, fmt.Errorf("APP_ENCRYPTION_KEY must decode to 32 bytes, got %d", len(key))
	}
	cfg.AppEncryptionKey = key

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	if v := strings.TrimSpace(os.Getenv("COOKIE_SECURE")); v != "" {
		cfg.CookieSecure = envBool("COOKIE_SECURE", false)
	} else {
		cfg.CookieSecure = cfg.AppEnv == "production"
	}

	if port := strings.TrimSpace(os.Getenv("SMTP_PORT")); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil {
			return Config{}, fmt.Errorf("SMTP_PORT: %w", err)
		}
		cfg.SMTPPort = n
	} else {
		cfg.SMTPPort = 587
	}

	cfg.UploadsDir = envOr("UPLOADS_DIR", "./data/uploads")
	cfg.DefaultClubSlug = strings.ToLower(envOr("DEFAULT_CLUB_SLUG", "mr-pushkin"))
	if v := strings.TrimSpace(os.Getenv("MAX_UPLOAD_BYTES")); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1024 {
			return Config{}, fmt.Errorf("MAX_UPLOAD_BYTES: invalid %q", v)
		}
		cfg.MaxUploadBytes = n
	} else {
		cfg.MaxUploadBytes = 5 << 20 // 5 MiB
	}

	return cfg, nil
}

// SessionCookieName returns the session cookie name for the current environment.
func (c Config) SessionCookieName() string {
	if c.CookieSecure && c.AppEnv == "production" {
		return "__Host-mpl_session"
	}
	return "mpl_session"
}

// HasSMTP reports whether outbound SMTP is configured.
func (c Config) HasSMTP() bool {
	return c.SMTPHost != "" && c.SMTPFrom != ""
}

// EmailConfirmURL builds the link placed in confirm_email outbox payloads.
// Prefer the Next.js page when FRONT_BASE_URL is set; otherwise fall back to the API route.
func (c Config) EmailConfirmURL(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if c.FrontBaseURL != "" {
		return c.FrontBaseURL + "/confirm-email?token=" + token
	}
	if c.PublicBaseURL != "" {
		return c.PublicBaseURL + "/api/auth/confirm-email?token=" + token
	}
	return ""
}

// PasswordResetURL builds the link placed in password_reset outbox payloads.
// Prefer the Next.js page when FRONT_BASE_URL is set; otherwise fall back to the API route.
func (c Config) PasswordResetURL(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if c.FrontBaseURL != "" {
		return c.FrontBaseURL + "/reset-password?token=" + token
	}
	if c.PublicBaseURL != "" {
		return c.PublicBaseURL + "/api/auth/reset-password?token=" + token
	}
	return ""
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
