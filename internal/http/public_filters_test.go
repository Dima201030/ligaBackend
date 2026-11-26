package httpapi

import (
	"testing"

	"github.com/Dima201030/ligaBackend/internal/db/sqlc"
)

func publicTournament(status sqlc.TournamentStatus) sqlc.Tournament {
	return sqlc.Tournament{
		Status:   status,
		Category: "challenger",
		Type:     sqlc.TournamentTypeSingles,
		Season:   2026,
		Title:    "Зимний кубок",
	}
}

func TestMatchPublicFiltersDefaultExcludesFinished(t *testing.T) {
	visible := []sqlc.TournamentStatus{
		sqlc.TournamentStatusRegistrationSoon,
		sqlc.TournamentStatusRegistrationOpen,
		sqlc.TournamentStatusRegistrationClosed,
		sqlc.TournamentStatusScheduled,
		sqlc.TournamentStatusLive,
	}
	for _, status := range visible {
		if !matchPublicFilters(publicTournament(status), "", "", "", 0, "") {
			t.Fatalf("status %q must be visible by default", status)
		}
	}
	hidden := []sqlc.TournamentStatus{
		sqlc.TournamentStatusDraft,
		sqlc.TournamentStatusCompleted,
		sqlc.TournamentStatusCancelled,
	}
	for _, status := range hidden {
		if matchPublicFilters(publicTournament(status), "", "", "", 0, "") {
			t.Fatalf("status %q must be hidden by default", status)
		}
	}
}

func TestMatchPublicFiltersUpcomingAlias(t *testing.T) {
	if matchPublicFilters(publicTournament(sqlc.TournamentStatusCompleted), "upcoming", "", "", 0, "") {
		t.Fatal("upcoming must not include completed")
	}
	if !matchPublicFilters(publicTournament(sqlc.TournamentStatusLive), "upcoming", "", "", 0, "") {
		t.Fatal("upcoming must include live")
	}
}

func TestMatchPublicFiltersExplicitStatus(t *testing.T) {
	cases := []struct {
		filter string
		status sqlc.TournamentStatus
		want   bool
	}{
		{"completed", sqlc.TournamentStatusCompleted, true},
		{"completed", sqlc.TournamentStatusLive, false},
		{"live", sqlc.TournamentStatusLive, true},
		{"registration_open", sqlc.TournamentStatusRegistrationOpen, true},
		{"registration_open", sqlc.TournamentStatusRegistrationSoon, false},
		{"cancelled", sqlc.TournamentStatusCancelled, true},
	}
	for _, tc := range cases {
		if got := matchPublicFilters(publicTournament(tc.status), tc.filter, "", "", 0, ""); got != tc.want {
			t.Fatalf("filter %q status %q = %v, want %v", tc.filter, tc.status, got, tc.want)
		}
	}
}

func TestMatchPublicFiltersAllIncludesEverything(t *testing.T) {
	statuses := []sqlc.TournamentStatus{
		sqlc.TournamentStatusDraft,
		sqlc.TournamentStatusRegistrationOpen,
		sqlc.TournamentStatusLive,
		sqlc.TournamentStatusCompleted,
		sqlc.TournamentStatusCancelled,
	}
	for _, status := range statuses {
		if !matchPublicFilters(publicTournament(status), "all", "", "", 0, "") {
			t.Fatalf("all must include %q", status)
		}
	}
}

func TestMatchPublicFiltersCategoryTypeYear(t *testing.T) {
	tour := publicTournament(sqlc.TournamentStatusLive)
	tour.Category = "masters"
	tour.Type = sqlc.TournamentTypeDoubles
	tour.Season = 2025

	if !matchPublicFilters(tour, "all", "masters", "doubles", 2025, "") {
		t.Fatal("matching category/type/year must pass")
	}
	if matchPublicFilters(tour, "all", "futures", "doubles", 2025, "") {
		t.Fatal("wrong category must fail")
	}
	if matchPublicFilters(tour, "all", "masters", "singles", 2025, "") {
		t.Fatal("wrong type must fail")
	}
	if matchPublicFilters(tour, "all", "masters", "doubles", 2026, "") {
		t.Fatal("wrong year must fail")
	}
}

func TestMatchPublicFiltersQueryIsCaseInsensitive(t *testing.T) {
	tour := publicTournament(sqlc.TournamentStatusLive)
	tour.Title = "Кубок Парли"

	if !matchPublicFilters(tour, "all", "", "", 0, "кубок") {
		t.Fatal("lowercase query must match")
	}
	if !matchPublicFilters(tour, "all", "", "", 0, "ПАРЛИ") {
		t.Fatal("uppercase query must match")
	}
	if matchPublicFilters(tour, "all", "", "", 0, "лето") {
		t.Fatal("non-matching query must be filtered out")
	}
}

func TestPublicVenueHidesLegacyBranding(t *testing.T) {
	cases := []venueJSON{
		{Name: "Крытые корты Mr. Pushkin", Address: "Волхонское шоссе, 79А"},
		{Name: "Корты Пушкин", Address: "Санкт-Петербург, Пушкинский район"},
		{Name: "MISTER PUSHKIN", Address: "Pushkin district"},
		{Name: "Pairly", Address: "Pairly Arena"},
	}
	for _, in := range cases {
		got := publicVenue(in)
		if got.Name != "Корты" {
			t.Fatalf("%q: name = %q, want Корты", in.Name, got.Name)
		}
		if got.Address != "" {
			t.Fatalf("%q: address = %q, want empty", in.Address, got.Address)
		}
	}
}

func TestPublicVenueKeepsNeutralValues(t *testing.T) {
	in := venueJSON{Name: "Корт 1", Address: "Тестовая улица, 1"}
	got := publicVenue(in)
	if got != in {
		t.Fatalf("neutral venue changed: %+v -> %+v", in, got)
	}
}
