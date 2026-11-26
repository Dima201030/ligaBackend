package httpapi

import (
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

var (
	// Synthetic / placeholder phones from seeds and agent smokes.
	unreliablePhoneRe = regexp.MustCompile(`(?i)(\+?7?9{3,}|000[- ]?00|111[- ]?11|123[- ]?45|999[- ]?\d{2})`)
)

// publicPhone hides unreliable legacy/test phone values from public payloads.
func publicPhone(phone pgtype.Text) *string {
	if !phone.Valid {
		return nil
	}
	v := strings.TrimSpace(phone.String)
	if v == "" || unreliablePhone(v) {
		return nil
	}
	return &v
}

func publicPhoneString(phone string) *string {
	v := strings.TrimSpace(phone)
	if v == "" || unreliablePhone(v) {
		return nil
	}
	return &v
}

func unreliablePhone(phone string) bool {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	if len(digits) < 10 {
		return true
	}
	// Agent/test range +7999… and obvious placeholders.
	if strings.HasPrefix(digits, "7999") || strings.HasPrefix(digits, "7000") {
		return true
	}
	if unreliablePhoneRe.MatchString(phone) {
		return true
	}
	// All same digit / sequential junk.
	if digits == strings.Repeat(string(digits[0]), len(digits)) {
		return true
	}
	return false
}
