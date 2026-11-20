package content

import (
	"strings"
	"unicode/utf8"
)

// ReadingTimeMinutes estimates reading time from paragraphs (~180 wpm RU/EN mix).
func ReadingTimeMinutes(paragraphs []string) int {
	words := 0
	for _, p := range paragraphs {
		fields := strings.Fields(strings.TrimSpace(p))
		words += len(fields)
	}
	if words == 0 {
		// fallback: rough chars if somehow no spaces
		chars := 0
		for _, p := range paragraphs {
			chars += utf8.RuneCountInString(p)
		}
		if chars == 0 {
			return 1
		}
		words = chars / 5
	}
	mins := (words + 179) / 180
	if mins < 1 {
		return 1
	}
	if mins > 120 {
		return 120
	}
	return mins
}
