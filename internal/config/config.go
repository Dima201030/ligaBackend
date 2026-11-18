package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// Config holds process configuration loaded from the environment.
type Config struct {
	HTTPAddr           string
	DatabaseURL        string
	AppEncryptionKey   []byte
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:    envOr("HTTP_ADDR", "0.0.0.0:8080"),
		DatabaseURL: strings.TrimSpace(os.Getenv("DATABASE_URL")),
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
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
