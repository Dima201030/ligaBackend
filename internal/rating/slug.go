package rating

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5/pgtype"
)

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "i", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "h", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

// PlayerSlug builds a unique-ish slug from last/first name + user id suffix.
func PlayerSlug(lastName, firstName string, userID pgtype.UUID) string {
	base := slugify(lastName + "-" + firstName)
	if base == "" {
		base = "player"
	}
	suffix := "xxxxxxxx"
	if userID.Valid {
		b := userID.Bytes
		suffix = fmt.Sprintf("%x", b[0:4])
	}
	out := base + "-" + suffix
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if repl, ok := translit[r]; ok {
			b.WriteString(repl)
			prevDash = false
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if unicode.IsSpace(r) || r == '-' || r == '_' {
			if b.Len() > 0 && !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
