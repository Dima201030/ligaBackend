package httpapi

import (
	"strings"
	"testing"
	"time"
)

func baseTournamentWrite() tournamentWriteRequest {
	start := time.Now().UTC().AddDate(0, 0, 14).Truncate(time.Minute)
	closeAt := start.AddDate(0, 0, -2)
	return tournamentWriteRequest{
		Title:                "Winter Cup",
		Description:          "Короткое описание",
		Type:                 "singles",
		Category:             "challenger",
		Surface:              "hard",
		Format:               "olympic",
		Status:               "draft",
		StartsAt:             start.Format(time.RFC3339),
		RegistrationClosesAt: closeAt.Format(time.RFC3339),
		VenueName:            "Корт 1",
		VenueAddress:         "Москва",
		Regulations:          strings.Repeat("р", 40),
	}
}

func TestParseTournamentWriteRejectsLongText(t *testing.T) {
	req := baseTournamentWrite()
	req.Title = strings.Repeat("т", tournamentTitleMax+1)
	if _, msg := parseTournamentWrite(req, true); msg == "" || !strings.Contains(msg, "120") {
		t.Fatalf("title max: got %q", msg)
	}

	req = baseTournamentWrite()
	req.Description = strings.Repeat("д", tournamentDescriptionMax+1)
	if _, msg := parseTournamentWrite(req, true); msg == "" || !strings.Contains(msg, "2000") {
		t.Fatalf("description max: got %q", msg)
	}

	req = baseTournamentWrite()
	req.Regulations = strings.Repeat("р", tournamentRegulationsMax+1)
	if _, msg := parseTournamentWrite(req, true); msg == "" || !strings.Contains(msg, "10000") {
		t.Fatalf("regulations max: got %q", msg)
	}
}

func TestParseTournamentWriteRejectsFarStart(t *testing.T) {
	req := baseTournamentWrite()
	far := time.Now().UTC().AddDate(0, 7, 0)
	req.StartsAt = far.Format(time.RFC3339)
	req.RegistrationClosesAt = far.AddDate(0, 0, -2).Format(time.RFC3339)
	if _, msg := parseTournamentWrite(req, true); msg == "" || !strings.Contains(msg, "6 месяцев") {
		t.Fatalf("start max ahead: got %q", msg)
	}
}

func TestParseTournamentWriteRejectsEndBeforeStart(t *testing.T) {
	req := baseTournamentWrite()
	start, _ := time.Parse(time.RFC3339, req.StartsAt)
	end := start.Add(-time.Hour).Format(time.RFC3339)
	req.EndsAt = &end
	if _, msg := parseTournamentWrite(req, true); msg == "" || !strings.Contains(msg, "раньше старта") {
		t.Fatalf("end before start: got %q", msg)
	}
}

func TestParseTournamentWriteAcceptsSanePayload(t *testing.T) {
	req := baseTournamentWrite()
	if _, msg := parseTournamentWrite(req, true); msg != "" {
		t.Fatalf("expected ok, got %q", msg)
	}
}

func TestValidateNewsBodyMax(t *testing.T) {
	ok := []string{strings.Repeat("а", 100), strings.Repeat("б", 100)}
	if err := validateNewsBody(ok); err != nil {
		t.Fatalf("expected ok: %v", err)
	}
	tooBig := []string{strings.Repeat("а", newsBodyTotalMax+1)}
	if err := validateNewsBody(tooBig); err == nil {
		t.Fatal("expected body too long")
	}
}
