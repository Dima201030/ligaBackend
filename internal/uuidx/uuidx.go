// Package uuidx is the Pairly UUID contract for Go services.
//
// Wire format: canonical lowercase UUID string (8-4-4-4-12 hex).
// DB: native uuid / uuid[]. JSON API: string. Never JSON number / []byte in public payloads.
// SQL arrays: bind []string (canonical) and cast explicitly — ANY($n::uuid[]), $n::uuid[].
package uuidx

import (
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// String returns the canonical lowercase UUID, or "" if invalid.
func String(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	b := id.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Normalize trims space and lowercases a UUID string. Does not validate.
func Normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Parse trims and scans a UUID string into pgtype.UUID.
func Parse(s string) (pgtype.UUID, error) {
	s = Normalize(s)
	if s == "" {
		return pgtype.UUID{}, fmt.Errorf("uuid: empty")
	}
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return pgtype.UUID{}, fmt.Errorf("uuid: %w", err)
	}
	return id, nil
}

// ParseOrZero parses s; on error or empty returns an invalid UUID.
func ParseOrZero(s string) pgtype.UUID {
	id, err := Parse(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}

// Equal reports whether both UUIDs are valid and byte-identical.
func Equal(a, b pgtype.UUID) bool {
	return a.Valid && b.Valid && a.Bytes == b.Bytes
}

// EqualString compares two UUID strings after Normalize (case/trim).
func EqualString(a, b string) bool {
	na, nb := Normalize(a), Normalize(b)
	if na == "" || nb == "" {
		return false
	}
	return na == nb
}

// New generates a random UUID v4 as pgtype.UUID.
func New() (pgtype.UUID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return pgtype.UUID{}, fmt.Errorf("uuid new: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return pgtype.UUID{Bytes: b, Valid: true}, nil
}

// TextArray returns unique canonical strings for SQL binds that cast to uuid[]
// (e.g. ANY($n::uuid[]), VALUES (…, $n::uuid[])). Prefer this over []pgtype.UUID
// for raw Query/Exec array parameters — pgx has failed on Pi without the cast.
func TextArray(ids []pgtype.UUID) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		key := String(id)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}
