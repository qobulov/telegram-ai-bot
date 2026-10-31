package game

import (
	"time"
)

// OtinchiShortName is O'tinchi's short name, as it must be registered with
// @BotFather's /newgame.
const OtinchiShortName = "otinchi"

// O'tinchi is a woodcutter at an old plane tree: each tap chops the trunk's
// lowest piece from the left or the right, and the trunk comes down a
// piece. Branches grow from its pieces; the woodcutter must not stand on
// the side of a branch at their height, nor step into one. A time bar runs
// down, quicker as more is chopped, and each chop fills it a little, less
// as more is chopped. A golden piece is worth five.
//
// The page plays the same rules (public/game/otinchi/sim.js) and sends the
// side of each chop and when it was made, from the first chop; the server
// plays them again on the trunk the play's seed grows, which gives the
// score, refuses chops quicker than a hand makes them or after the time ran
// out, and wants the play to have taken as long as its chops did.
const (
	otinchiStartMs    = 7000
	otinchiMostMs     = 8000
	otinchiFirstBonus = 500
	otinchiLeastBonus = 260
	otinchiDrainCap   = 500
	otinchiMinGap     = 70
	otinchiGolden     = 5
	otinchiSafe       = 3
	otinchiMaxChops   = 5000
	otinchiRunAge     = time.Hour
)

// The sides of a branch and of the woodcutter.
const (
	sideNone = iota
	sideLeft
	sideRight
)

// OtinchiRules is what the page needs to play as the server does.
type OtinchiRules struct {
	StartMs    int `json:"start_ms"`
	MostMs     int `json:"most_ms"`
	FirstBonus int `json:"first_bonus"`
	LeastBonus int `json:"least_bonus"`
	DrainCap   int `json:"drain_cap"`
	MinGap     int `json:"min_gap"`
	Golden     int `json:"golden"`
	Safe       int `json:"safe"`
	MaxChops   int `json:"max_chops"`
}

var otinchiRules = OtinchiRules{
	StartMs: otinchiStartMs, MostMs: otinchiMostMs, FirstBonus: otinchiFirstBonus, LeastBonus: otinchiLeastBonus,
	DrainCap: otinchiDrainCap, MinGap: otinchiMinGap, Golden: otinchiGolden, Safe: otinchiSafe, MaxChops: otinchiMaxChops,
}

// OtinchiPlay is a play of O'tinchi as the page gets it.
type OtinchiPlay struct {
	Seed  uint32       `json:"seed"`
	Rules OtinchiRules `json:"rules"`
}

// StartOtinchi begins a play of O'tinchi for a session: its seed and rules,
// and the run token its chops are sent in with.
func (g *Game) StartOtinchi(sessionToken string) (string, OtinchiPlay, error) {
	s, err := g.session(sessionToken)
	if err != nil || s.Game != OtinchiShortName {
		return "", OtinchiPlay{}, ErrToken
	}
	r := run{Player: s.Player, Game: OtinchiShortName, Started: g.now().UnixMilli(), Seed: newSeed()}
	return g.sign(kindRun, r), OtinchiPlay{Seed: ilonSeed(r.Seed), Rules: otinchiRules}, nil
}

// FinishOtinchi scores a play of O'tinchi: sides has an L or an R for each
// chop, and times when each was made, in milliseconds from the first.
func (g *Game) FinishOtinchi(runToken, sides string, times []int) (Result, error) {
	r, elapsed, err := g.run(runToken, OtinchiShortName, otinchiRunAge)
	if err != nil {
		return Result{}, err
	}
	if len(times) > 0 && time.Duration(times[len(times)-1])*time.Millisecond > elapsed+skew {
		// The page says it took longer than the server saw pass.
		return Result{Player: r.Player}, ErrImplausible
	}
	score, err := ScoreOtinchi(ilonSeed(r.Seed), sides, times)
	if err != nil {
		return Result{Player: r.Player}, err
	}
	return Result{Player: r.Player, Score: score}, nil
}

// ScoreOtinchi plays a play's chops again on the trunk seed grows and gives
// its score. Only the last chop may be the one a branch ended the play in;
// chops quicker than a hand, after the time ran out, or not L or R, are
// refused.
func ScoreOtinchi(seed uint32, sides string, times []int) (int, error) {
	if len(sides) == 0 || len(sides) != len(times) || len(sides) > otinchiMaxChops || times[0] != 0 {
		return 0, ErrImplausible
	}
	tree := &otinchiTree{random: seed}
	left, chops, score := otinchiStartMs, 0, 0
	last := len(sides) - 1
	for i := range len(sides) {
		side := map[byte]int{'L': sideLeft, 'R': sideRight}[sides[i]]
		if side == sideNone {
			return 0, ErrImplausible
		}
		if i > 0 {
			gap := times[i] - times[i-1]
			if gap < otinchiMinGap {
				return 0, ErrImplausible
			}
			if left -= otinchiDrain(gap, chops); left <= 0 {
				return 0, ErrImplausible
			}
		}
		branch, golden := tree.at(chops)
		if branch == side {
			// The woodcutter stepped into a branch.
			if i != last {
				return 0, ErrImplausible
			}
			break
		}
		chops++
		if fallen, _ := tree.at(chops); fallen == side {
			// The branch above came down on the woodcutter.
			if i != last {
				return 0, ErrImplausible
			}
			break
		}
		if golden {
			score += otinchiGolden
		} else {
			score++
		}
		left = min(otinchiMostMs, left+max(otinchiLeastBonus, otinchiFirstBonus-chops))
	}
	return score, nil
}

// otinchiDrain is how much of the time bar gap milliseconds take after
// chops chops: as much as they last at first, twice that from
// otinchiDrainCap chops on.
func otinchiDrain(gap, chops int) int {
	return gap * (otinchiDrainCap + min(chops, otinchiDrainCap)) / otinchiDrainCap
}

// otinchiTree is the trunk a seed grows: each piece's branch, and whether
// it is golden. A branch is never followed by one on the other side, which
// no woodcutter could pass.
type otinchiTree struct {
	random   uint32
	branches []int
	golden   []bool
}

// at is the piece at i, grown if it was not yet.
func (t *otinchiTree) at(i int) (int, bool) {
	for len(t.branches) <= i {
		n := len(t.branches)
		if n < otinchiSafe {
			t.branches, t.golden = append(t.branches, sideNone), append(t.golden, false)
			continue
		}
		prev := t.branches[n-1]
		roll, shine := t.next()%100, t.next()%10
		branch := sideNone
		switch {
		case prev == sideNone && roll >= 70:
			branch = sideRight
		case prev == sideNone && roll >= 40:
			branch = sideLeft
		case prev != sideNone && roll >= 55:
			branch = prev
		}
		t.branches = append(t.branches, branch)
		t.golden = append(t.golden, branch == sideNone && shine == 0)
	}
	return t.branches[i], t.golden[i]
}

// next is the next random number, xorshift32 as the page has it.
func (t *otinchiTree) next() uint32 {
	x := t.random
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	t.random = x
	return x
}
