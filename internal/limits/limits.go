package limits

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ClubNameMin = 3
	ClubNameMax = 80
	ClubSlugMin = 3
	ClubSlugMax = 40

	MaxActiveTournamentsPerClub = 50
	MaxClubInvitesPerDay        = 100
	MaxNewsPerDayPerClub        = 20

	MaxUploadBytes = 5 << 20 // 5 MiB

	MinAgeYears = 5
	MaxAgeYears = 100

	MaxSetGames = 99
)

var clubSlugRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ValidateClubName returns a Russian validation message, or "" if ok.
func ValidateClubName(name string) string {
	n := strings.TrimSpace(name)
	lenN := utf8.RuneCountInString(n)
	if lenN < ClubNameMin {
		return fmt.Sprintf("Название клуба слишком короткое — минимум %d символа", ClubNameMin)
	}
	if lenN > ClubNameMax {
		return fmt.Sprintf("Название клуба слишком длинное — максимум %d символов", ClubNameMax)
	}
	return ""
}

// ValidateClubSlug returns a Russian validation message for a normalized slug, or "".
func ValidateClubSlug(slug string) string {
	s := strings.ToLower(strings.TrimSpace(slug))
	if s == "" || !clubSlugRE.MatchString(s) {
		return "Укажите slug латиницей: my-club"
	}
	lenS := utf8.RuneCountInString(s)
	if lenS < ClubSlugMin {
		return fmt.Sprintf("Slug слишком короткий — минимум %d символа", ClubSlugMin)
	}
	if lenS > ClubSlugMax {
		return fmt.Sprintf("Slug слишком длинный — максимум %d символов", ClubSlugMax)
	}
	return ""
}

// AgeYears returns full years between birth (date) and asOf.
func AgeYears(birth, asOf time.Time) int {
	years := asOf.Year() - birth.Year()
	anniversary := time.Date(asOf.Year(), birth.Month(), birth.Day(), 0, 0, 0, 0, time.UTC)
	if asOf.Before(anniversary) {
		years--
	}
	return years
}

// ValidateAge returns a Russian validation message, or "" if age is within MinAgeYears…MaxAgeYears.
func ValidateAge(birth, asOf time.Time) string {
	age := AgeYears(birth, asOf)
	if age < MinAgeYears || age > MaxAgeYears {
		return fmt.Sprintf("Возраст должен быть от %d до %d лет", MinAgeYears, MaxAgeYears)
	}
	return ""
}
