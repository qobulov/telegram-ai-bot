package game

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// pilot flies a play of seed: it taps whenever the carpet sinks below the
// middle of the next gap, until it has passed gates, and then stops
// tapping until it crashes. It gives the taps, the ticks and the play's
// last state.
func pilot(seed uint32, gates int) ([]int, int, *gilamSim) {
	sim := newGilamSim(seed)
	var taps []int
	for tick := range gilamMaxTicks {
		if sim.passed < gates && sim.y > sim.aim()+6*gilamUnit && sim.vy >= 0 {
			taps = append(taps, tick)
			sim.tap()
		}
		if sim.step() {
			return taps, tick + 1, sim
		}
	}
	panic("the pilot never crashed")
}

// aim is the middle of the next gap the carpet has to pass, a little low,
// as a tap lifts it.
func (s *gilamSim) aim() int {
	for _, c := range s.columns {
		if c.x+gilamColumnW >= gilamCarpetX0 {
			return c.top + c.gap*3/5
		}
	}
	return gilamGround / 2
}

// A play is scored by playing its taps again: gaps, levels and coins count,
// and the score is the one the play reached.
func TestGilamPlaysAreScoredAgain(t *testing.T) {
	levels, coins, saved := 0, 0, 0
	for i := uint32(1); i < 40; i += 2 {
		seed := i*2654435761 | 1
		taps, ticks, played := pilot(seed, 45)
		score, err := ScoreGilam(seed, taps, ticks)
		if err != nil || score != played.score || score == 0 {
			t.Fatalf("seed %d: scored %d, %v; the play reached %d", seed, score, err, played.score)
		}
		levels = max(levels, played.level)
		coins += played.coins
		if played.ghostEnd > 0 {
			saved++
		}
	}
	if levels < 4 {
		t.Errorf("the pilot reached level %d at most; the test should fly several", levels)
	}
	if coins == 0 || saved == 0 {
		t.Errorf("%d coins and %d feathers taken; the test should take both", coins, saved)
	}
}

// Taps out of order or after the end, or a play said to end before or
// after its crash, are refused.
func TestGilamRefusesPlaysThatDidNotHappen(t *testing.T) {
	seed := uint32(98765)
	taps, ticks, _ := pilot(seed, 8)
	if len(taps) < 4 {
		t.Fatal("the pilot hardly tapped")
	}
	swapped := slices.Clone(taps)
	swapped[0], swapped[1] = swapped[1], swapped[0]
	for name, play := range map[string]struct {
		taps  []int
		ticks int
	}{
		"early end":    {taps, ticks - 1},
		"late end":     {taps, ticks + 5},
		"out of order": {swapped, ticks},
		"twice a tick": {append([]int{taps[0]}, taps...), ticks},
		"after end":    {append(slices.Clone(taps), ticks+3), ticks},
		"no ticks":     {nil, 0},
		"too long":     {nil, gilamMaxTicks + 1},
	} {
		if _, err := ScoreGilam(seed, play.taps, play.ticks); !errors.Is(err, ErrImplausible) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

// A play sent in quicker than its ticks last is refused; one that took
// them is scored, for the session's player, and only as Uchar gilam's.
func TestFinishGilamWantsTheTimeTheTicksTake(t *testing.T) {
	g, c := newTestGame()
	link := playURL(t, g, GilamShortName, player)
	if !strings.HasPrefix(link, "https://example.com/game/gilam/?s=") {
		t.Fatalf("Uchar gilam's page is %s", link)
	}
	session := sessionOf(t, link)
	if _, _, err := g.StartIlon(session); !errors.Is(err, ErrToken) {
		t.Errorf("Kosmik ilon started from Uchar gilam's session: %v", err)
	}
	run, play, err := g.StartGilam(session)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.FinishIlon(run, nil, 1); !errors.Is(err, ErrToken) {
		t.Errorf("Uchar gilam's run finished as Kosmik ilon's: %v", err)
	}
	taps, ticks, played := pilot(play.Seed, 15)
	took := time.Duration(ticks*gilamTickMs) * time.Millisecond

	c.now = c.now.Add(took / 2)
	if _, err := g.FinishGilam(run, taps, ticks); !errors.Is(err, ErrImplausible) {
		t.Fatalf("half the time: %v", err)
	}
	c.now = c.now.Add(took / 2)
	result, err := g.FinishGilam(run, taps, ticks)
	if err != nil || result.Score != played.score || result.Player != player {
		t.Fatalf("got %+v, %v; want %d for %+v", result, err, played.score, player)
	}
}

// The page plays the same rules: sim.js scores the pilot's plays as the
// server does, and refuses a play said to end before its crash.
func TestGilamPageScoresAsTheServer(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	type play struct {
		Seed  uint32 `json:"seed"`
		Taps  []int  `json:"taps"`
		Ticks int    `json:"ticks"`
		Score *int   `json:"score"`
	}
	var plays []play
	for i := uint32(3); i < 80; i += 7 {
		seed := i*2246822519 | 1
		taps, ticks, played := pilot(seed, 35)
		plays = append(plays, play{seed, taps, ticks, &played.score})
	}
	plays = append(plays, play{plays[0].Seed, plays[0].Taps, plays[0].Ticks - 1, nil})
	cases, _ := json.Marshal(map[string]any{"rules": gilamRules, "plays": plays})
	input := filepath.Join(t.TempDir(), "plays.json")
	if err := os.WriteFile(input, cases, 0o600); err != nil {
		t.Fatal(err)
	}
	sim, _ := filepath.Abs("../../public/game/gilam/sim.js")
	script := `const Sim = require(process.argv[1]); const c = JSON.parse(require("fs").readFileSync(process.argv[2]));
console.log(JSON.stringify(c.plays.map(p => Sim.play(p.seed, c.rules, p.taps, p.ticks))));`
	out, err := exec.Command(node, "-e", script, sim, input).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var scores []*int
	if err := json.Unmarshal(out, &scores); err != nil || len(scores) != len(plays) {
		t.Fatalf("node said %s", out)
	}
	for i, p := range plays {
		switch {
		case p.Score == nil && scores[i] != nil:
			t.Errorf("play %d: the page scored %d a play said to end before its crash", i, *scores[i])
		case p.Score != nil && (scores[i] == nil || *scores[i] != *p.Score):
			t.Errorf("play %d (seed %d, %d ticks): the page scored %v, the server %d", i, p.Seed, p.Ticks, scores[i], *p.Score)
		}
	}
}
