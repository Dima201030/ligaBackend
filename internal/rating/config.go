package rating

import (
	"encoding/json"
	"fmt"
	"strconv"
)

type rawCoefficients struct {
	StartRating          float64            `json:"start_rating"`
	MinRating            float64            `json:"min_rating"`
	WindowWeeks          int                `json:"window_weeks"`
	BestSingles          int                `json:"best_singles"`
	BestDoubles          int                `json:"best_doubles"`
	BaseByWins           map[string]float64 `json:"base_by_wins"`
	Category             map[string]float64 `json:"category"`
	TournamentType       map[string]float64 `json:"tournament_type"`
	EloK                 EloKBands          `json:"elo_k"`
	WalkoverPointsFactor float64            `json:"walkover_points_factor"`
	WalkoverElo          bool               `json:"walkover_elo"`
	ConsolationFactor    float64            `json:"consolation_factor"`
	TeamsShare           float64            `json:"teams_share"`
	RetireEloFactor      float64            `json:"retire_elo_factor"`
}

// ParseCoefficients decodes rating_configs.coefficients jsonb.
func ParseCoefficients(raw []byte) (Coefficients, error) {
	if len(raw) == 0 {
		return DefaultCoeffs, nil
	}
	var r rawCoefficients
	if err := json.Unmarshal(raw, &r); err != nil {
		return Coefficients{}, fmt.Errorf("coefficients: %w", err)
	}
	c := Coefficients{
		StartRating:          r.StartRating,
		MinRating:            r.MinRating,
		WindowWeeks:          r.WindowWeeks,
		BestSingles:          r.BestSingles,
		BestDoubles:          r.BestDoubles,
		BaseByWins:           map[int]float64{},
		Category:             r.Category,
		TournamentType:       r.TournamentType,
		EloK:                 r.EloK,
		WalkoverPointsFactor: r.WalkoverPointsFactor,
		WalkoverElo:          r.WalkoverElo,
		ConsolationFactor:    r.ConsolationFactor,
		TeamsShare:           r.TeamsShare,
		RetireEloFactor:      r.RetireEloFactor,
	}
	for k, v := range r.BaseByWins {
		n, err := strconv.Atoi(k)
		if err != nil {
			return Coefficients{}, fmt.Errorf("base_by_wins key %q: %w", k, err)
		}
		c.BaseByWins[n] = v
	}
	if c.StartRating == 0 {
		c.StartRating = DefaultCoeffs.StartRating
	}
	if c.MinRating == 0 {
		c.MinRating = DefaultCoeffs.MinRating
	}
	if len(c.BaseByWins) == 0 {
		c.BaseByWins = DefaultCoeffs.BaseByWins
	}
	if c.Category == nil {
		c.Category = DefaultCoeffs.Category
	}
	if c.TournamentType == nil {
		c.TournamentType = DefaultCoeffs.TournamentType
	}
	if c.WalkoverPointsFactor <= 0 {
		c.WalkoverPointsFactor = DefaultCoeffs.WalkoverPointsFactor
	}
	if c.ConsolationFactor <= 0 {
		c.ConsolationFactor = DefaultCoeffs.ConsolationFactor
	}
	if c.TeamsShare <= 0 {
		c.TeamsShare = DefaultCoeffs.TeamsShare
	}
	if c.RetireEloFactor <= 0 {
		c.RetireEloFactor = DefaultCoeffs.RetireEloFactor
	}
	return c, nil
}
