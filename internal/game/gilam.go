package game

import (
	"time"
)

// GilamShortName is Uchar gilam's short name, as it must be registered
// with @BotFather's /newgame.
const GilamShortName = "gilam"

// Uchar gilam is a flying carpet over Samarkand at night, played in ticks
// of gilamTickMs: it falls, and each tap lifts it. Minarets rise from the
// ground and storm clouds hang from the sky, a gap between each pair; the
// carpet flies through the gaps, picking up coins and now and then a Humo
// feather, which saves it from one crash. Every gilamPerLevel gaps passed
// it flies quicker, and the gaps narrow.
//
// The page plays the same rules (public/game/gilam/sim.js) and sends the
// ticks it was tapped in and how many ticks went by; the server plays them
// again from the play's seed, which gives the score, and the ticks give the
// least time the play took. All lengths are in sixteenths of the 360 by
// 640 field's units, so that both play it to the same integer.
const (
	gilamUnit      = 16
	gilamWidth     = 360 * gilamUnit
	gilamGround    = 600 * gilamUnit
	gilamCarpetX0  = 72 * gilamUnit
	gilamCarpetX1  = 120 * gilamUnit
	gilamCarpetH   = 9 * gilamUnit // half the carpet's height
	gilamStartY    = 300 * gilamUnit
	gilamGravity   = 9
	gilamFlap      = -150
	gilamMaxFall   = 176
	gilamSpacing   = 220 * gilamUnit
	gilamColumnW   = 60 * gilamUnit
	gilamMargin    = 60 * gilamUnit
	gilamFirstGap  = 200 * gilamUnit
	gilamGapStep   = 8 * gilamUnit
	gilamLeastGap  = 136 * gilamUnit
	gilamFirstPace = 48
	gilamPaceStep  = 5
	gilamTopPace   = 96
	gilamItemW     = 22 * gilamUnit // half an item's reach, across
	gilamItemH     = 20 * gilamUnit // and up and down
	gilamGhost     = 30
	gilamPerLevel  = 10
	gilamTickMs    = 20
	gilamMaxTicks  = 90000
	gilamRunAge    = time.Hour
)

// What a gap holds.
const (
	itemNone = iota
	itemCoin
	itemFeather
)

// GilamRules is what the page needs to play as the server does.
type GilamRules struct {
	Unit      int `json:"unit"`
	Width     int `json:"width"`
	Ground    int `json:"ground"`
	CarpetX0  int `json:"carpet_x0"`
	CarpetX1  int `json:"carpet_x1"`
	CarpetH   int `json:"carpet_h"`
	StartY    int `json:"start_y"`
	Gravity   int `json:"gravity"`
	Flap      int `json:"flap"`
	MaxFall   int `json:"max_fall"`
	Spacing   int `json:"spacing"`
	ColumnW   int `json:"column_w"`
	Margin    int `json:"margin"`
	FirstGap  int `json:"first_gap"`
	GapStep   int `json:"gap_step"`
	LeastGap  int `json:"least_gap"`
	FirstPace int `json:"first_pace"`
	PaceStep  int `json:"pace_step"`
	TopPace   int `json:"top_pace"`
	ItemW     int `json:"item_w"`
	ItemH     int `json:"item_h"`
	Ghost     int `json:"ghost"`
	PerLevel  int `json:"per_level"`
	TickMs    int `json:"tick_ms"`
	MaxTicks  int `json:"max_ticks"`
}

var gilamRules = GilamRules{
	Unit: gilamUnit, Width: gilamWidth, Ground: gilamGround, CarpetX0: gilamCarpetX0, CarpetX1: gilamCarpetX1,
	CarpetH: gilamCarpetH, StartY: gilamStartY, Gravity: gilamGravity, Flap: gilamFlap, MaxFall: gilamMaxFall,
	Spacing: gilamSpacing, ColumnW: gilamColumnW, Margin: gilamMargin, FirstGap: gilamFirstGap, GapStep: gilamGapStep,
	LeastGap: gilamLeastGap, FirstPace: gilamFirstPace, PaceStep: gilamPaceStep, TopPace: gilamTopPace,
	ItemW: gilamItemW, ItemH: gilamItemH, Ghost: gilamGhost, PerLevel: gilamPerLevel, TickMs: gilamTickMs,
	MaxTicks: gilamMaxTicks,
}

// GilamPlay is a play of Uchar gilam as the page gets it.
type GilamPlay struct {
	Seed  uint32     `json:"seed"`
	Rules GilamRules `json:"rules"`
}

// StartGilam begins a play of Uchar gilam for a session: its seed and
// rules, and the run token its taps are sent in with.
func (g *Game) StartGilam(sessionToken string) (string, GilamPlay, error) {
	s, err := g.session(sessionToken)
	if err != nil || s.Game != GilamShortName {
		return "", GilamPlay{}, ErrToken
	}
	r := run{Player: s.Player, Game: GilamShortName, Started: g.now().UnixMilli(), Seed: newSeed()}
	return g.sign(kindRun, r), GilamPlay{Seed: ilonSeed(r.Seed), Rules: gilamRules}, nil
}

// FinishGilam scores a play of Uchar gilam: taps are the ticks the carpet
// was lifted in, and ticks how many went by, the last the one it crashed
// in.
func (g *Game) FinishGilam(runToken string, taps []int, ticks int) (Result, error) {
	r, elapsed, err := g.run(runToken, GilamShortName, gilamRunAge)
	if err != nil {
		return Result{}, err
	}
	score, err := ScoreGilam(ilonSeed(r.Seed), taps, ticks)
	if err != nil {
		return Result{Player: r.Player}, err
	}
	// The page waits out each tick, so the play took at least as long as
	// its ticks; a little is allowed for the clocks' drift.
	took := time.Duration(ticks*gilamTickMs) * time.Millisecond
	if elapsed*103/100+skew < took {
		return Result{Player: r.Player}, ErrImplausible
	}
	return Result{Player: r.Player, Score: score}, nil
}

// ScoreGilam plays a play's taps again from its seed and gives its score.
// Taps out of order or after the end, and a play whose carpet did not
// crash in its last tick, are refused.
func ScoreGilam(seed uint32, taps []int, ticks int) (int, error) {
	if ticks < 1 || ticks > gilamMaxTicks || len(taps) > ticks {
		return 0, ErrImplausible
	}
	sim := newGilamSim(seed)
	next := 0
	for tick := range ticks {
		if next < len(taps) && taps[next] == tick {
			sim.tap()
			next++
		}
		if next < len(taps) && taps[next] <= tick {
			return 0, ErrImplausible
		}
		if sim.step() != (tick == ticks-1) {
			return 0, ErrImplausible
		}
	}
	if next != len(taps) {
		return 0, ErrImplausible
	}
	return sim.score, nil
}

// gilamColumn is a minaret and the cloud above it, the gap between them
// from top to top+gap, and what the gap holds.
type gilamColumn struct {
	x, top, gap int
	passed      bool
	item        int
	itemY       int
}

// gilamSim is a play of Uchar gilam.
type gilamSim struct {
	random   uint32
	y, vy    int
	tick     int
	level    int
	passed   int
	coins    int
	score    int
	shield   bool
	ghostEnd int
	columns  []gilamColumn
}

func newGilamSim(seed uint32) *gilamSim {
	return &gilamSim{random: seed, y: gilamStartY, vy: gilamFlap, level: 1}
}

// next is the next random number, xorshift32 as the page has it.
func (s *gilamSim) next() uint32 {
	x := s.random
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	s.random = x
	return x
}

func (s *gilamSim) tap() {
	s.vy = gilamFlap
}

func (s *gilamSim) pace() int {
	return min(gilamFirstPace+gilamPaceStep*(s.level-1), gilamTopPace)
}

// spawn adds columns until there is one off the field's right edge.
func (s *gilamSim) spawn() {
	for {
		x := gilamWidth + 40*gilamUnit
		if n := len(s.columns); n > 0 {
			if s.columns[n-1].x+gilamSpacing > x {
				return
			}
			x = s.columns[n-1].x + gilamSpacing
		}
		gap := max(gilamFirstGap-gilamGapStep*(s.level-1), gilamLeastGap)
		room := (gilamGround - 2*gilamMargin - gap) / gilamUnit
		top := gilamMargin + int(s.next()%uint32(room+1))*gilamUnit
		c := gilamColumn{x: x, top: top, gap: gap, itemY: top + gap/2}
		switch roll := s.next() % 8; {
		case roll == 0:
			c.item = itemFeather
		case roll < 5:
			c.item = itemCoin
		}
		s.columns = append(s.columns, c)
	}
}

// step plays a tick and reports whether the carpet crashed in it.
func (s *gilamSim) step() bool {
	s.vy = min(s.vy+gilamGravity, gilamMaxFall)
	s.y += s.vy
	if s.y < gilamCarpetH {
		s.y, s.vy = gilamCarpetH, 0
	}
	pace := s.pace()
	kept := s.columns[:0]
	for _, c := range s.columns {
		c.x -= pace
		if c.x+gilamColumnW > 0 {
			kept = append(kept, c)
		}
	}
	s.columns = kept
	s.spawn()
	crashed := false
	for i := range s.columns {
		c := &s.columns[i]
		if !c.passed && c.x+gilamColumnW < gilamCarpetX0 {
			c.passed = true
			s.passed++
			s.score += 10 * s.level
			if s.passed%gilamPerLevel == 0 {
				s.score += 50 * s.level
				s.level++
			}
		}
		center := c.x + gilamColumnW/2
		if c.item != itemNone && abs(center-(gilamCarpetX0+gilamCarpetX1)/2) < gilamItemW && abs(c.itemY-s.y) < gilamItemH {
			if c.item == itemCoin {
				s.coins++
				s.score += 5 * s.level
			} else {
				s.shield = true
			}
			c.item = itemNone
		}
		inside := gilamCarpetX1 > c.x && gilamCarpetX0 < c.x+gilamColumnW
		if inside && (s.y-gilamCarpetH < c.top || s.y+gilamCarpetH > c.top+c.gap) {
			crashed = true
		}
	}
	grounded := s.y+gilamCarpetH >= gilamGround
	if grounded {
		s.y = gilamGround - gilamCarpetH
	}
	switch {
	case (crashed || grounded) && s.tick >= s.ghostEnd:
		if !s.shield {
			return true
		}
		// The feather takes the crash: the carpet passes through for a
		// while, lifted off the ground if it was there.
		s.shield = false
		s.ghostEnd = s.tick + 1 + gilamGhost
		s.vy = gilamFlap
	case grounded:
		s.vy = gilamFlap
	}
	s.tick++
	return false
}

func abs(n int) int {
	return max(n, -n)
}
