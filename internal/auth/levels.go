package auth

import "strings"

// DefaultLevel is stored when registration / placeholder create omit level.
// Kept for DB NOT NULL + CHECK; player-facing UX no longer collects it.
const DefaultLevel = "rookies"

var allowedLevels = map[string]struct{}{
	"masters":    {},
	"challenger": {},
	"futures":    {},
	"rookies":    {},
	"beginners":  {},
	"newcomers":  {},
}

// ValidLevel reports whether slug is an adult CategorySlug.
func ValidLevel(slug string) bool {
	_, ok := allowedLevels[slug]
	return ok
}

// ResolveLevel returns a valid level: empty → DefaultLevel; invalid → ("", false).
func ResolveLevel(raw string) (string, bool) {
	level := strings.TrimSpace(raw)
	if level == "" {
		return DefaultLevel, true
	}
	if !ValidLevel(level) {
		return "", false
	}
	return level, true
}
