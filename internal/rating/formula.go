package rating

import (
	"math"
	"strings"
)

const (
	DisciplineSingles = "singles"
	DisciplineDoubles = "doubles"
)

// DefaultCoeffs is revision-1 proposal from docs/RATING.md until club lock.
var DefaultCoeffs = Coefficients{
	StartRating: 1200,
	MinRating:   800,
	WindowWeeks: 52,
	BestSingles: 10,
	BestDoubles: 5,
	BaseByWins: map[int]float64{
		0: 30, 1: 75, 2: 135, 3: 210, 4: 300, 5: 375, 6: 435,
	},
	Category: map[string]float64{
		"masters": 1.6, "challenger": 1.3, "futures": 1.0,
		"rookies": 0.8, "beginners": 0.6, "newcomers": 0.4,
	},
	TournamentType: map[string]float64{
		"singles": 1.0, "doubles": 0.8, "mixed": 0.8,
		"cup": 1.4, "final": 1.5, "teams": 0.9,
	},
	EloK: EloKBands{
		Singles: EloKTier{Lt10: 32, Lt30: 24, Default: 16},
		Doubles: EloKTier{Lt10: 16, Lt30: 12, Default: 8},
	},
	WalkoverPointsFactor: 0.5,
	WalkoverElo:          false,
	ConsolationFactor:    0.5,
	TeamsShare:           0.6,
	RetireEloFactor:      0.5,
}

// Coefficients mirrors rating_configs.coefficients jsonb.
type Coefficients struct {
	StartRating          float64            `json:"start_rating"`
	MinRating            float64            `json:"min_rating"`
	WindowWeeks          int                `json:"window_weeks"`
	BestSingles          int                `json:"best_singles"`
	BestDoubles          int                `json:"best_doubles"`
	BaseByWins           map[int]float64    `json:"-"`
	Category             map[string]float64 `json:"category"`
	TournamentType       map[string]float64 `json:"tournament_type"`
	EloK                 EloKBands          `json:"elo_k"`
	WalkoverPointsFactor float64            `json:"walkover_points_factor"`
	WalkoverElo          bool               `json:"walkover_elo"`
	ConsolationFactor    float64            `json:"consolation_factor"`
	TeamsShare           float64            `json:"teams_share"`
	RetireEloFactor      float64            `json:"retire_elo_factor"` // mid-match retire → K × factor
}

// EloKBands holds singles/doubles K tiers by match count.
type EloKBands struct {
	Singles EloKTier `json:"singles"`
	Doubles EloKTier `json:"doubles"`
}

// EloKTier is provisional → mid → stable K.
type EloKTier struct {
	Lt10    float64 `json:"lt10"`
	Lt30    float64 `json:"lt30"`
	Default float64 `json:"default"`
}

// SizeFactor clamps sqrt(n/16) into [0.6, 1.6].
func SizeFactor(participants int) float64 {
	if participants < 1 {
		participants = 1
	}
	f := math.Sqrt(float64(participants) / 16.0)
	return clamp(f, 0.6, 1.6)
}

// BasePoints returns base points for wins in the main draw.
func BasePoints(wins int, c Coefficients) float64 {
	if wins < 0 {
		wins = 0
	}
	if c.BaseByWins == nil {
		c = DefaultCoeffs
	}
	if v, ok := c.BaseByWins[wins]; ok {
		return v
	}
	// Extrapolate gently beyond table: +45 per extra win after 6.
	maxKnown := 6
	base := c.BaseByWins[maxKnown]
	return base + float64(wins-maxKnown)*45
}

// TournamentPointsInput describes one player's result in a tournament.
type TournamentPointsInput struct {
	Wins         int
	Participants int
	Category     string
	Type         string // singles|doubles|mixed|cup|final|teams
	Consolation  bool
	WalkoverOnly bool // technical wins only → half points, no Elo elsewhere
	Disqualified bool // DQ'd in tournament → 0 points
}

// WinStats is primary vs consolation win breakdown for one player in a tournament.
type WinStats struct {
	PrimaryWins             int
	PrimaryWalkoverWins     int
	ConsolationWins         int
	ConsolationWalkoverWins int
	Disqualified            bool // lost a disqualified match → 0 tournament points
}

// AwardResult is the tournament points input derived from win stats.
type AwardResult struct {
	Wins         int
	Consolation  bool
	WalkoverOnly bool
	Disqualified bool
}

// AwardFromWinStats picks the counting draw and flags for TournamentPoints.
// Disqualified players get 0 points. Otherwise primary wins if any; else consolation.
// WalkoverOnly when every counting win is a walkover (anti-farm).
func AwardFromWinStats(stats WinStats) AwardResult {
	if stats.Disqualified {
		return AwardResult{Disqualified: true}
	}
	if stats.PrimaryWins > 0 {
		return AwardResult{
			Wins:         stats.PrimaryWins,
			WalkoverOnly: stats.PrimaryWalkoverWins == stats.PrimaryWins,
		}
	}
	if stats.ConsolationWins > 0 {
		return AwardResult{
			Wins:         stats.ConsolationWins,
			Consolation:  true,
			WalkoverOnly: stats.ConsolationWalkoverWins == stats.ConsolationWins,
		}
	}
	return AwardResult{}
}

// TournamentPoints computes rounded points for one tournament result.
func TournamentPoints(in TournamentPointsInput, c Coefficients) int {
	if in.Disqualified {
		return 0
	}
	if c.Category == nil {
		c = DefaultCoeffs
	}
	base := BasePoints(in.Wins, c)
	size := SizeFactor(in.Participants)
	cat := c.Category[in.Category]
	if cat == 0 {
		cat = 1
	}
	typ := c.TournamentType[in.Type]
	if typ == 0 {
		typ = 1
	}
	raw := base * size * cat * typ
	if in.Consolation {
		raw *= c.ConsolationFactor
	}
	if in.Type == "teams" {
		raw *= c.TeamsShare
	}
	if in.WalkoverOnly {
		raw *= c.WalkoverPointsFactor
	}
	return int(math.Round(raw))
}

// ExpectedScore is classic Elo expectation for rating A vs B.
func ExpectedScore(ratingA, ratingB float64) float64 {
	return 1.0 / (1.0 + math.Pow(10, (ratingB-ratingA)/400.0))
}

// KFactor picks K by experience and singles/doubles.
func KFactor(matchesPlayed int, doubles bool, c Coefficients) float64 {
	tier := c.EloK.Singles
	if doubles {
		tier = c.EloK.Doubles
	}
	switch {
	case matchesPlayed < 10:
		return tier.Lt10
	case matchesPlayed < 30:
		return tier.Lt30
	default:
		return tier.Default
	}
}

// EffectiveK applies mid-match retire factor (default ×0.5) when retire is set.
func EffectiveK(matchesPlayed int, doubles, retire bool, c Coefficients) float64 {
	k := KFactor(matchesPlayed, doubles, c)
	if !retire {
		return k
	}
	f := c.RetireEloFactor
	if f <= 0 {
		f = DefaultCoeffs.RetireEloFactor
	}
	return k * f
}

// EloDelta returns rating change for score S (1 win / 0 loss / 0.5 draw).
func EloDelta(rating, opponent, k, score float64) float64 {
	e := ExpectedScore(rating, opponent)
	return k * (score - e)
}

// ApplyEloFloor clamps rating to minimum.
func ApplyEloFloor(rating, min float64) float64 {
	if min <= 0 {
		min = 800
	}
	if rating < min {
		return min
	}
	return rating
}

// SideAverage is mean of partner ratings (doubles team strength).
func SideAverage(ratings []float64) float64 {
	if len(ratings) == 0 {
		return DefaultCoeffs.StartRating
	}
	var sum float64
	for _, r := range ratings {
		sum += r
	}
	return sum / float64(len(ratings))
}

// SelectBestSums keeps the best N results and returns their sum.
func SelectBestSums(values []int, n int) int {
	if n <= 0 || len(values) == 0 {
		return 0
	}
	cp := append([]int(nil), values...)
	// simple insertion sort descending — lists are tiny (≤52)
	for i := 1; i < len(cp); i++ {
		j := i
		for j > 0 && cp[j-1] < cp[j] {
			cp[j-1], cp[j] = cp[j], cp[j-1]
			j--
		}
	}
	if n > len(cp) {
		n = len(cp)
	}
	sum := 0
	for i := 0; i < n; i++ {
		sum += cp[i]
	}
	return sum
}

// DisciplineFromTournamentType maps tournament type → singles/doubles pool.
// doubles/mixed/teams → doubles; singles/cup/final (and unknown) → singles.
func DisciplineFromTournamentType(tournamentType string) string {
	switch strings.ToLower(strings.TrimSpace(tournamentType)) {
	case "doubles", "mixed", "teams":
		return DisciplineDoubles
	default:
		return DisciplineSingles
	}
}

// NormalizeDiscipline returns singles or doubles.
func NormalizeDiscipline(d string) string {
	if strings.EqualFold(strings.TrimSpace(d), DisciplineDoubles) {
		return DisciplineDoubles
	}
	return DisciplineSingles
}

// WindowPointsBreakdown is best-N singles + best-N doubles from a rolling window.
type WindowPointsBreakdown struct {
	Singles int
	Doubles int
}

// Total is singles + doubles.
func (b WindowPointsBreakdown) Total() int { return b.Singles + b.Doubles }

// SplitBestWindowSums applies BestSingles / BestDoubles caps separately.
func SplitBestWindowSums(singlesDeltas, doublesDeltas []int, c Coefficients) WindowPointsBreakdown {
	bestS := c.BestSingles
	bestD := c.BestDoubles
	if bestS <= 0 {
		bestS = DefaultCoeffs.BestSingles
	}
	if bestD <= 0 {
		bestD = DefaultCoeffs.BestDoubles
	}
	return WindowPointsBreakdown{
		Singles: SelectBestSums(singlesDeltas, bestS),
		Doubles: SelectBestSums(doublesDeltas, bestD),
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
