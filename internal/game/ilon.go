package game

import (
	"slices"
	"time"
)

// IlonShortName is Kosmik ilon's short name, as it must be registered with
// @BotFather's /newgame.
const IlonShortName = "ilon"

// Kosmik ilon is a snake on a field that wraps at its edges, played in
// ticks: each tick the snake moves one cell, after turning if the player
// asked. Eating a planet adds points and makes it grow; every ilonPerLevel
// planets it reaches the next level, whose field has other walls and whose
// ticks are quicker. Now and then a bonus shows for a while: a star of
// points, a clock that slows the ticks, or a ghost that lets the snake pass
// through walls and itself. Running into a wall or itself ends the play.
//
// The page plays the same rules (public/game/ilon/sim.js) and sends the
// turns, each with its tick, and how many ticks went by; the server plays
// them again from the play's seed. That gives the score, and the ticks'
// lengths say how long the play must have taken, so it cannot be played
// quicker than the page lets a person.
const (
	ilonCols     = 15
	ilonRows     = 21
	ilonPerLevel = 10
	ilonGrowth   = 2
	// A planet eaten within ilonComboTicks of the one before raises the
	// combo, which multiplies its points up to ilonMaxCombo.
	ilonComboTicks = 30
	ilonMaxCombo   = 3
	ilonBonusShare = 3
	ilonBonusTicks = 50
	ilonSlowTicks  = 60
	ilonGhostTicks = 40
	// A tick takes ilonFirstTick ms on the first level, ilonTickStep less
	// on each next one, never under ilonFastestTick; half as long again
	// while slowed.
	ilonFirstTick   = 150
	ilonTickStep    = 14
	ilonFastestTick = 60
	ilonMaxTicks    = 30000
	ilonRunAge      = time.Hour
)

// The bonuses.
const (
	bonusStar = iota
	bonusSlow
	bonusGhost
	bonusKinds
)

// The directions, as the page numbers them: up, right, down, left.
var (
	ilonDX = [4]int{0, 1, 0, -1}
	ilonDY = [4]int{-1, 0, 1, 0}
)

// ilonStart is where the snake starts each level, head first, going up.
var ilonStart = []int{14*ilonCols + 7, 15*ilonCols + 7, 16*ilonCols + 7}

// ilonLevels are the fields' walls, as cells, taken in turn; the column
// the snake starts in is left clear above it for a few cells.
var ilonLevels = func() [][]int {
	type rect struct{ x0, y0, x1, y1 int }
	plans := [][]rect{
		{},
		{{1, 1, 3, 1}, {1, 1, 1, 3}, {11, 1, 13, 1}, {13, 1, 13, 3}, {1, 19, 3, 19}, {1, 17, 1, 19}, {11, 19, 13, 19}, {13, 17, 13, 19}},
		{{2, 5, 5, 5}, {9, 5, 12, 5}, {2, 10, 5, 10}, {9, 10, 12, 10}, {2, 15, 5, 15}, {9, 15, 12, 15}},
		{{0, 0, 5, 0}, {9, 0, 14, 0}, {0, 20, 5, 20}, {9, 20, 14, 20}, {0, 0, 0, 7}, {0, 13, 0, 20}, {14, 0, 14, 7}, {14, 13, 14, 20}},
		{{2, 2, 3, 3}, {11, 2, 12, 3}, {2, 9, 3, 10}, {11, 9, 12, 10}, {2, 17, 3, 18}, {11, 17, 12, 18}, {6, 5, 8, 6}},
		{{3, 3, 3, 8}, {3, 3, 6, 3}, {11, 12, 11, 17}, {8, 17, 11, 17}, {8, 8, 12, 8}, {2, 12, 6, 12}},
	}
	levels := make([][]int, len(plans))
	for i, plan := range plans {
		levels[i] = []int{}
		for _, r := range plan {
			for y := r.y0; y <= r.y1; y++ {
				for x := r.x0; x <= r.x1; x++ {
					if cell := y*ilonCols + x; !slices.Contains(levels[i], cell) {
						levels[i] = append(levels[i], cell)
					}
				}
			}
		}
	}
	return levels
}()

// IlonRules is what the page needs to play as the server does.
type IlonRules struct {
	Cols        int     `json:"cols"`
	Rows        int     `json:"rows"`
	Levels      [][]int `json:"levels"`
	Start       []int   `json:"start"`
	PerLevel    int     `json:"per_level"`
	Growth      int     `json:"growth"`
	ComboTicks  int     `json:"combo_ticks"`
	MaxCombo    int     `json:"max_combo"`
	BonusShare  int     `json:"bonus_share"`
	BonusTicks  int     `json:"bonus_ticks"`
	SlowTicks   int     `json:"slow_ticks"`
	GhostTicks  int     `json:"ghost_ticks"`
	FirstTick   int     `json:"first_tick"`
	TickStep    int     `json:"tick_step"`
	FastestTick int     `json:"fastest_tick"`
	MaxTicks    int     `json:"max_ticks"`
}

var ilonRules = IlonRules{
	Cols: ilonCols, Rows: ilonRows, Levels: ilonLevels, Start: ilonStart, PerLevel: ilonPerLevel,
	Growth: ilonGrowth, ComboTicks: ilonComboTicks, MaxCombo: ilonMaxCombo, BonusShare: ilonBonusShare,
	BonusTicks: ilonBonusTicks, SlowTicks: ilonSlowTicks, GhostTicks: ilonGhostTicks,
	FirstTick: ilonFirstTick, TickStep: ilonTickStep, FastestTick: ilonFastestTick, MaxTicks: ilonMaxTicks,
}

// IlonPlay is a play of Kosmik ilon as the page gets it.
type IlonPlay struct {
	Seed  uint32    `json:"seed"`
	Rules IlonRules `json:"rules"`
}

// StartIlon begins a play of Kosmik ilon for a session: its seed and rules,
// and the run token its turns are sent in with.
func (g *Game) StartIlon(sessionToken string) (string, IlonPlay, error) {
	s, err := g.session(sessionToken)
	if err != nil || s.Game != IlonShortName {
		return "", IlonPlay{}, ErrToken
	}
	r := run{Player: s.Player, Game: IlonShortName, Started: g.now().UnixMilli(), Seed: newSeed()}
	return g.sign(kindRun, r), IlonPlay{Seed: ilonSeed(r.Seed), Rules: ilonRules}, nil
}

// FinishIlon scores a play of Kosmik ilon: turns are pairs of a tick and
// the direction taken at its start, and ticks how many went by, the last
// the one the snake crashed in.
func (g *Game) FinishIlon(runToken string, turns []int, ticks int) (Result, error) {
	r, elapsed, err := g.run(runToken, IlonShortName, ilonRunAge)
	if err != nil {
		return Result{}, err
	}
	score, took, err := ScoreIlon(ilonSeed(r.Seed), turns, ticks)
	if err != nil {
		return Result{Player: r.Player}, err
	}
	// The page waits out each tick, so the play took at least as long as
	// its ticks; a little is allowed for the clocks' drift.
	if elapsed*103/100+skew < took {
		return Result{Player: r.Player}, ErrImplausible
	}
	return Result{Player: r.Player, Score: score}, nil
}

// ScoreIlon plays a play's turns again from its seed and gives its score
// and how long its ticks took. Turns out of order or after the end, and a
// play whose snake did not crash in its last tick, are refused.
func ScoreIlon(seed uint32, turns []int, ticks int) (int, time.Duration, error) {
	if ticks < 1 || ticks > ilonMaxTicks || len(turns)%2 != 0 || len(turns)/2 > ticks {
		return 0, 0, ErrImplausible
	}
	sim := newIlonSim(seed)
	took, next := 0, 0
	for tick := range ticks {
		if next < len(turns) && turns[next] == tick {
			dir := turns[next+1]
			if dir < 0 || dir > 3 {
				return 0, 0, ErrImplausible
			}
			sim.turn(dir)
			next += 2
		}
		if next < len(turns) && turns[next] <= tick {
			return 0, 0, ErrImplausible
		}
		took += sim.tickMs()
		if sim.step() != (tick == ticks-1) {
			return 0, 0, ErrImplausible
		}
	}
	if next != len(turns) {
		return 0, 0, ErrImplausible
	}
	return sim.score, time.Duration(took) * time.Millisecond, nil
}

// ilonSeed is the page's random numbers' seed: xorshift needs one not 0.
func ilonSeed(seed uint64) uint32 {
	return uint32(seed) | 1
}

// ilonSim is a play of Kosmik ilon. Cells are numbered row by row.
type ilonSim struct {
	random   uint32
	walls    []bool
	body     []int // head first
	dir      int
	growing  int
	tick     int
	level    int
	eaten    int
	score    int
	combo    int
	lastMeal int
	planet   int
	bonus    int // -1 when there is none
	kind     int
	bonusEnd int
	slowEnd  int
	ghostEnd int
}

func newIlonSim(seed uint32) *ilonSim {
	s := &ilonSim{random: seed, level: 1, lastMeal: -ilonComboTicks - 1}
	s.enterLevel()
	return s
}

// next is the next random number, xorshift32 as the page has it.
func (s *ilonSim) next() uint32 {
	x := s.random
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	s.random = x
	return x
}

// enterLevel lays out the level's field and puts the snake at the start.
func (s *ilonSim) enterLevel() {
	s.walls = make([]bool, ilonCols*ilonRows)
	for _, cell := range ilonLevels[(s.level-1)%len(ilonLevels)] {
		s.walls[cell] = true
	}
	s.body = slices.Clone(ilonStart)
	s.dir, s.growing, s.combo = 0, 0, 0
	s.planet, s.bonus, s.slowEnd, s.ghostEnd = -1, -1, 0, 0
	s.planet = s.free()
}

// free is a random cell no wall, snake, planet or bonus is on, -1 when
// there is none.
func (s *ilonSim) free() int {
	taken := slices.Clone(s.walls)
	for _, cell := range s.body {
		taken[cell] = true
	}
	for _, cell := range []int{s.planet, s.bonus} {
		if cell >= 0 {
			taken[cell] = true
		}
	}
	count := 0
	for _, t := range taken {
		if !t {
			count++
		}
	}
	if count == 0 {
		return -1
	}
	pick := int(s.next() % uint32(count))
	for cell, t := range taken {
		if t {
			continue
		}
		if pick == 0 {
			return cell
		}
		pick--
	}
	return -1
}

// turn takes dir, unless it goes straight back.
func (s *ilonSim) turn(dir int) {
	if dir != (s.dir+2)%4 {
		s.dir = dir
	}
}

// tickMs is how long the coming tick lasts.
func (s *ilonSim) tickMs() int {
	ms := max(ilonFirstTick-ilonTickStep*(s.level-1), ilonFastestTick)
	if s.tick < s.slowEnd {
		ms = ms * 3 / 2
	}
	return ms
}

// step plays a tick and reports whether the snake crashed in it.
func (s *ilonSim) step() bool {
	head := s.body[0]
	x := (head%ilonCols + ilonDX[s.dir] + ilonCols) % ilonCols
	y := (head/ilonCols + ilonDY[s.dir] + ilonRows) % ilonRows
	cell := y*ilonCols + x
	body := s.body
	if s.growing == 0 {
		body = body[:len(body)-1] // the tail moves out of the way
	}
	if s.tick >= s.ghostEnd && (s.walls[cell] || slices.Contains(body, cell)) {
		return true
	}
	s.body = append([]int{cell}, s.body...)
	if s.growing > 0 {
		s.growing--
	} else {
		s.body = s.body[:len(s.body)-1]
	}
	switch cell {
	case s.planet:
		s.eat()
	case s.bonus:
		s.collect()
	}
	if s.bonus >= 0 && s.tick >= s.bonusEnd {
		s.bonus = -1
	}
	s.tick++
	return false
}

// eat scores the planet the head is on, and lays out the next level or the
// next planet, and sometimes a bonus.
func (s *ilonSim) eat() {
	if s.tick-s.lastMeal <= ilonComboTicks {
		s.combo = min(s.combo+1, ilonMaxCombo-1)
	} else {
		s.combo = 0
	}
	s.lastMeal = s.tick
	s.score += 10 * s.level * (1 + s.combo)
	s.growing += ilonGrowth
	s.eaten++
	if s.eaten%ilonPerLevel == 0 {
		s.score += 100 * s.level
		s.level++
		s.enterLevel()
		return
	}
	s.planet = -1
	s.planet = s.free()
	if s.bonus < 0 && s.next()%ilonBonusShare == 0 {
		s.kind = int(s.next() % bonusKinds)
		s.bonus = s.free()
		s.bonusEnd = s.tick + ilonBonusTicks
	}
}

// collect applies the bonus the head is on.
func (s *ilonSim) collect() {
	switch s.kind {
	case bonusStar:
		s.score += 50 * s.level
	case bonusSlow:
		s.slowEnd = s.tick + 1 + ilonSlowTicks
	case bonusGhost:
		s.ghostEnd = s.tick + 1 + ilonGhostTicks
	}
	s.bonus = -1
}
