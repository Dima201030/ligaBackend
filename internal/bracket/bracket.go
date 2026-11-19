// Package bracket ports mr-pushkin-league/src/lib/bracket.ts draw generation.
// Pure: no I/O, no randomness unless the caller shuffles entries first.
package bracket

import (
	"fmt"
	"math"
	"sort"
)

// Format mirrors tournament_format.
type Format string

const (
	FormatOlympic           Format = "olympic"
	FormatRoundRobin        Format = "round_robin"
	FormatGroupsPlayoff     Format = "groups_playoff"
	FormatDoubleElimination Format = "double_elimination"
)

// Entry is a seeded player/team or qualifier placeholder.
type Entry struct {
	PlayerID      string
	PartnerID     string
	RegistrationID string
	Name          string
	Seed          *int
	Rating        int
	IsPlaceholder bool
}

// Slot is one side of a match.
type Slot struct {
	PlayerID       string
	PartnerID      string
	RegistrationID string
	Name           string
	Seed           *int
	Placeholder    string
	IsBye          bool
}

// Match is a node in the draw tree.
type Match struct {
	ID              string
	RoundIndex      int
	Position        int
	Stage           string // main | lower | group | third_place | grand_final | playoff
	Slot1           Slot
	Slot2           Slot
	NextMatchID     string
	NextSlot        int // 1 or 2
	LoserNextMatchID string
	LoserNextSlot   int // 1 or 2
	AutomaticWinner int // 1 or 2 when bye advances
}

// Round groups matches under a label.
type Round struct {
	Index   int
	Label   string
	Matches []Match
}

// GroupStage is a round-robin group.
type GroupStage struct {
	Name    string
	Entries []Entry
	Matches []Match
}

// Draw is the serialisable bracket tree.
type Draw struct {
	Format       Format
	EntriesCount int
	BracketSize  int
	Byes         int
	Rounds       []Round
	LowerRounds  []Round
	Groups       []GroupStage
	PlayoffRounds []Round
	ThirdPlace   *Match
	GrandFinal   *Match
	MatchCount   int
}

// Options configure optional draw features.
type Options struct {
	GroupSize          int
	QualifiersPerGroup int
	ThirdPlace         bool
}

var roundNames = map[int]string{
	1:  "Финал",
	2:  "Полуфинал",
	4:  "1/4 финала",
	8:  "1/8 финала",
	16: "1/16 финала",
	32: "1/32 финала",
}

// RoundLabel returns the Russian round name for a round with n matches.
func RoundLabel(matchesInRound int) string {
	if name, ok := roundNames[matchesInRound]; ok {
		return name
	}
	return fmt.Sprintf("1/%d финала", matchesInRound*2)
}

// NextPowerOfTwo returns the smallest power of two >= value (min 2).
func NextPowerOfTwo(value int) int {
	size := 2
	for size < value {
		size *= 2
	}
	return size
}

// SeedOrder returns classic bracket seed order. Size 8 → [1,8,4,5,2,7,3,6].
func SeedOrder(size int) []int {
	order := []int{1}
	for len(order) < size {
		next := len(order)*2 + 1
		expanded := make([]int, 0, len(order)*2)
		for _, seed := range order {
			expanded = append(expanded, seed, next-seed)
		}
		order = expanded
	}
	return order
}

func seededEntries(entries []Entry) []Entry {
	type item struct {
		entry Entry
		index int
	}
	items := make([]item, len(entries))
	for i, e := range entries {
		items[i] = item{entry: e, index: i}
	}
	sort.SliceStable(items, func(i, j int) bool {
		left := 10_000 + items[i].index
		right := 10_000 + items[j].index
		if items[i].entry.Seed != nil {
			left = *items[i].entry.Seed
		}
		if items[j].entry.Seed != nil {
			right = *items[j].entry.Seed
		}
		return left < right
	})
	out := make([]Entry, len(items))
	for i, it := range items {
		out[i] = it.entry
	}
	return out
}

func entrySlot(entry *Entry) Slot {
	if entry == nil {
		return Slot{IsBye: true}
	}
	if entry.IsPlaceholder {
		return Slot{Placeholder: entry.Name, Seed: entry.Seed}
	}
	return Slot{
		PlayerID:       entry.PlayerID,
		PartnerID:      entry.PartnerID,
		RegistrationID: entry.RegistrationID,
		Name:           entry.Name,
		Seed:           entry.Seed,
	}
}

func winnerSlot(match Match, winner int) Slot {
	if winner == 1 {
		return match.Slot1
	}
	return match.Slot2
}

func hasPlayer(slot Slot) bool {
	return slot.Name != "" || slot.PlayerID != ""
}

// BuildSingleElimination builds an olympic (single-elim) draw.
func BuildSingleElimination(entries []Entry, opts Options) Draw {
	ranked := seededEntries(entries)
	if len(ranked) < 2 {
		return Draw{
			Format:       FormatOlympic,
			EntriesCount: len(ranked),
			BracketSize:  len(ranked),
		}
	}

	size := NextPowerOfTwo(len(ranked))
	order := SeedOrder(size)
	placed := make([]*Entry, size)
	for i, seedNumber := range order {
		idx := seedNumber - 1
		if idx < len(ranked) {
			e := ranked[idx]
			placed[i] = &e
		}
	}

	totalRounds := int(math.Log2(float64(size)))
	rounds := make([]Round, 0, totalRounds)

	for round := 0; round < totalRounds; round++ {
		matchCount := size / (1 << (round + 1))
		matches := make([]Match, 0, matchCount)
		for position := 0; position < matchCount; position++ {
			m := Match{
				ID:         fmt.Sprintf("R%dM%d", round+1, position+1),
				RoundIndex: round,
				Position:   position,
				Stage:      "main",
			}
			if round == 0 {
				m.Slot1 = entrySlot(placed[position*2])
				m.Slot2 = entrySlot(placed[position*2+1])
			} else {
				m.Slot1 = Slot{Placeholder: fmt.Sprintf("Победитель R%dM%d", round, position*2+1)}
				m.Slot2 = Slot{Placeholder: fmt.Sprintf("Победитель R%dM%d", round, position*2+2)}
			}
			if round < totalRounds-1 {
				m.NextMatchID = fmt.Sprintf("R%dM%d", round+2, position/2+1)
				m.NextSlot = (position % 2) + 1
			}
			if round == 0 {
				hasFirst := hasPlayer(m.Slot1)
				hasSecond := hasPlayer(m.Slot2)
				if hasFirst && !hasSecond {
					m.AutomaticWinner = 1
					m.Slot2 = Slot{IsBye: true, Placeholder: "Пропуск"}
				} else if !hasFirst && hasSecond {
					m.AutomaticWinner = 2
					m.Slot1 = Slot{IsBye: true, Placeholder: "Пропуск"}
				}
			}
			matches = append(matches, m)
		}
		rounds = append(rounds, Round{Index: round, Label: RoundLabel(matchCount), Matches: matches})
	}

	// Push bye winners into the next round.
	for round := 0; round < len(rounds)-1; round++ {
		for i := range rounds[round].Matches {
			match := &rounds[round].Matches[i]
			if match.AutomaticWinner == 0 || match.NextMatchID == "" || match.NextSlot == 0 {
				continue
			}
			next := findMatch(rounds[round+1].Matches, match.NextMatchID)
			if next == nil {
				continue
			}
			slot := winnerSlot(*match, match.AutomaticWinner)
			if match.NextSlot == 1 {
				next.Slot1 = slot
			} else {
				next.Slot2 = slot
			}
		}
	}

	var thirdPlace *Match
	if opts.ThirdPlace && size >= 4 {
		semis := rounds[len(rounds)-2].Matches
		sf1, sf2 := "SF1", "SF2"
		if len(semis) > 0 {
			sf1 = semis[0].ID
		}
		if len(semis) > 1 {
			sf2 = semis[1].ID
		}
		tp := Match{
			ID:         "R3P",
			RoundIndex: len(rounds) - 2,
			Position:   0,
			Stage:      "third_place",
			Slot1:      Slot{Placeholder: fmt.Sprintf("Проигравший %s", sf1)},
			Slot2:      Slot{Placeholder: fmt.Sprintf("Проигравший %s", sf2)},
		}
		thirdPlace = &tp
		// Wire semis losers into third-place match.
		if len(semis) >= 2 {
			rounds[len(rounds)-2].Matches[0].LoserNextMatchID = "R3P"
			rounds[len(rounds)-2].Matches[0].LoserNextSlot = 1
			rounds[len(rounds)-2].Matches[1].LoserNextMatchID = "R3P"
			rounds[len(rounds)-2].Matches[1].LoserNextSlot = 2
		}
	}

	matchCount := 0
	for _, r := range rounds {
		matchCount += len(r.Matches)
	}
	if thirdPlace != nil {
		matchCount++
	}

	return Draw{
		Format:       FormatOlympic,
		EntriesCount: len(ranked),
		BracketSize:  size,
		Byes:         size - len(ranked),
		Rounds:       rounds,
		ThirdPlace:   thirdPlace,
		MatchCount:   matchCount,
	}
}

func findMatch(matches []Match, id string) *Match {
	for i := range matches {
		if matches[i].ID == id {
			return &matches[i]
		}
	}
	return nil
}

// BuildRoundRobin builds circle-method round-robin rounds.
func BuildRoundRobin(entries []Entry, idPrefix string) []Round {
	list := seededEntries(entries)
	slots := make([]*Entry, len(list))
	for i := range list {
		e := list[i]
		slots[i] = &e
	}
	if len(slots)%2 == 1 {
		slots = append(slots, nil)
	}
	count := len(slots)
	if count < 2 {
		return nil
	}

	rounds := make([]Round, 0, count-1)
	rotation := append([]*Entry(nil), slots...)

	for round := 0; round < count-1; round++ {
		matches := make([]Match, 0, count/2)
		for index := 0; index < count/2; index++ {
			first := rotation[index]
			second := rotation[count-1-index]
			if first == nil && second == nil {
				continue
			}
			m := Match{
				ID:         fmt.Sprintf("%sR%dM%d", idPrefix, round+1, index+1),
				RoundIndex: round,
				Position:   index,
				Stage:      "main",
				Slot1:      entrySlot(first),
				Slot2:      entrySlot(second),
			}
			// Odd entry counts insert a bye slot; treat as automatic advance.
			hasFirst := hasPlayer(m.Slot1)
			hasSecond := hasPlayer(m.Slot2)
			if hasFirst && !hasSecond {
				m.AutomaticWinner = 1
				m.Slot2 = Slot{IsBye: true, Placeholder: "Пропуск"}
			} else if !hasFirst && hasSecond {
				m.AutomaticWinner = 2
				m.Slot1 = Slot{IsBye: true, Placeholder: "Пропуск"}
			}
			matches = append(matches, m)
		}
		rounds = append(rounds, Round{
			Index:   round,
			Label:   fmt.Sprintf("Круг %d", round+1),
			Matches: matches,
		})
		fixed := rotation[0]
		rest := append([]*Entry(nil), rotation[1:]...)
		if len(rest) > 0 {
			last := rest[len(rest)-1]
			rest = append([]*Entry{last}, rest[:len(rest)-1]...)
		}
		rotation = append([]*Entry{fixed}, rest...)
	}
	return rounds
}

var groupLetters = []rune("ABCDEFGH")

// BuildGroups distributes entries snake-style into round-robin groups.
func BuildGroups(entries []Entry, groupSize int) []GroupStage {
	ranked := seededEntries(entries)
	if groupSize < 2 {
		groupSize = 4
	}
	groupCount := int(math.Max(1, math.Ceil(float64(len(ranked))/float64(groupSize))))
	groups := make([]GroupStage, groupCount)
	for i := range groups {
		letter := "?"
		if i < len(groupLetters) {
			letter = string(groupLetters[i])
		}
		groups[i] = GroupStage{Name: fmt.Sprintf("Группа %s", letter), Entries: []Entry{}, Matches: []Match{}}
	}

	for index, entry := range ranked {
		cycle := index / groupCount
		offset := index % groupCount
		target := offset
		if cycle%2 != 0 {
			target = groupCount - 1 - offset
		}
		groups[target].Entries = append(groups[target].Entries, entry)
	}

	for i := range groups {
		letter := "?"
		if i < len(groupLetters) {
			letter = string(groupLetters[i])
		}
		rr := BuildRoundRobin(groups[i].Entries, fmt.Sprintf("G%s", letter))
		var flat []Match
		for _, round := range rr {
			for _, m := range round.Matches {
				m.ID = fmt.Sprintf("%s-%s", letter, m.ID)
				m.Stage = "group"
				flat = append(flat, m)
			}
		}
		groups[i].Matches = flat
	}
	return groups
}

// BuildGroupsPlayoff builds groups then a single-elim playoff of placeholders.
func BuildGroupsPlayoff(entries []Entry, opts Options) Draw {
	groupSize := opts.GroupSize
	if groupSize == 0 {
		groupSize = 4
	}
	qualifiers := opts.QualifiersPerGroup
	if qualifiers < 1 {
		qualifiers = 2
	}
	groups := BuildGroups(entries, groupSize)

	var qualified []Entry
	for _, group := range groups {
		letter := group.Name
		if len(letter) > 0 {
			// "Группа A" → "A"
			runes := []rune(letter)
			letter = string(runes[len(runes)-1])
		}
		for place := 1; place <= qualifiers; place++ {
			qualified = append(qualified, Entry{
				Name:          fmt.Sprintf("%s%d", letter, place),
				IsPlaceholder: true,
			})
		}
	}
	for i := range qualified {
		seed := i + 1
		qualified[i].Seed = &seed
	}

	playoff := BuildSingleElimination(qualified, Options{ThirdPlace: opts.ThirdPlace})
	for i := range playoff.Rounds {
		for j := range playoff.Rounds[i].Matches {
			playoff.Rounds[i].Matches[j].Stage = "playoff"
		}
	}
	if playoff.ThirdPlace != nil {
		playoff.ThirdPlace.Stage = "third_place"
	}

	groupMatchCount := 0
	for _, g := range groups {
		groupMatchCount += len(g.Matches)
	}

	return Draw{
		Format:        FormatGroupsPlayoff,
		EntriesCount:  len(entries),
		BracketSize:   playoff.BracketSize,
		Byes:          playoff.Byes,
		Groups:        groups,
		PlayoffRounds: playoff.Rounds,
		ThirdPlace:    playoff.ThirdPlace,
		Rounds:        nil,
		MatchCount:    groupMatchCount + playoff.MatchCount,
	}
}

// BuildDoubleElimination builds upper + lower bracket + grand final.
func BuildDoubleElimination(entries []Entry) Draw {
	upper := BuildSingleElimination(entries, Options{})
	if upper.BracketSize < 2 {
		upper.Format = FormatDoubleElimination
		return upper
	}

	size := upper.BracketSize
	log := int(math.Log2(float64(size)))
	lowerRounds := make([]Round, 0, 2*log-2)

	for round := 0; round < 2*log-2; round++ {
		matchCount := size / (1 << (round/2 + 2))
		matches := make([]Match, 0, matchCount)
		for position := 0; position < matchCount; position++ {
			var slot1, slot2 Slot
			if round == 0 {
				slot1 = Slot{Placeholder: fmt.Sprintf("Проигравший ВС R1 M%d", position*2+1)}
				slot2 = Slot{Placeholder: fmt.Sprintf("Проигравший ВС R1 M%d", position*2+2)}
			} else if round%2 == 0 {
				slot1 = Slot{Placeholder: fmt.Sprintf("Победитель НС R%d M%d", round, position*2+1)}
				slot2 = Slot{Placeholder: fmt.Sprintf("Победитель НС R%d M%d", round, position*2+2)}
			} else {
				slot1 = Slot{Placeholder: fmt.Sprintf("Победитель НС R%d M%d", round, position+1)}
				slot2 = Slot{Placeholder: fmt.Sprintf("Проигравший ВС R%d M%d", (round+1)/2+1, position+1)}
			}
			m := Match{
				ID:         fmt.Sprintf("LB%dM%d", round+1, position+1),
				RoundIndex: round,
				Position:   position,
				Stage:      "lower",
				Slot1:      slot1,
				Slot2:      slot2,
			}
			if round < 2*log-3 {
				m.NextMatchID = fmt.Sprintf("LB%dM%d", round+2, position/2+1)
				m.NextSlot = (position % 2) + 1
			}
			matches = append(matches, m)
		}
		lowerRounds = append(lowerRounds, Round{
			Index:   round,
			Label:   fmt.Sprintf("Нижняя сетка · %d", round+1),
			Matches: matches,
		})
	}

	gf := Match{
		ID:         "GF",
		RoundIndex: 0,
		Position:   0,
		Stage:      "grand_final",
		Slot1:      Slot{Placeholder: "Победитель верхней сетки"},
		Slot2:      Slot{Placeholder: "Победитель нижней сетки"},
	}

	lowerMatchCount := 0
	for _, r := range lowerRounds {
		lowerMatchCount += len(r.Matches)
	}

	return Draw{
		Format:      FormatDoubleElimination,
		EntriesCount: upper.EntriesCount,
		BracketSize: upper.BracketSize,
		Byes:        upper.Byes,
		Rounds:      upper.Rounds,
		LowerRounds: lowerRounds,
		GrandFinal:  &gf,
		MatchCount:  upper.MatchCount + lowerMatchCount + 1,
	}
}

// Generate dispatches to the format-specific builder.
func Generate(format Format, entries []Entry, opts Options) Draw {
	switch format {
	case FormatRoundRobin:
		rounds := BuildRoundRobin(entries, "RR")
		count := 0
		for _, r := range rounds {
			count += len(r.Matches)
		}
		return Draw{
			Format:       FormatRoundRobin,
			EntriesCount: len(entries),
			BracketSize:  len(entries),
			Rounds:       rounds,
			MatchCount:   count,
		}
	case FormatGroupsPlayoff:
		return BuildGroupsPlayoff(entries, opts)
	case FormatDoubleElimination:
		return BuildDoubleElimination(entries)
	case FormatOlympic:
		fallthrough
	default:
		return BuildSingleElimination(entries, Options{ThirdPlace: opts.ThirdPlace})
	}
}

// FlattenMatches returns all matches from a draw in stable order for persistence.
func FlattenMatches(draw Draw) []Match {
	var out []Match
	for _, g := range draw.Groups {
		out = append(out, g.Matches...)
	}
	for _, r := range draw.Rounds {
		out = append(out, r.Matches...)
	}
	for _, r := range draw.PlayoffRounds {
		out = append(out, r.Matches...)
	}
	for _, r := range draw.LowerRounds {
		out = append(out, r.Matches...)
	}
	if draw.ThirdPlace != nil {
		out = append(out, *draw.ThirdPlace)
	}
	if draw.GrandFinal != nil {
		out = append(out, *draw.GrandFinal)
	}
	return out
}
