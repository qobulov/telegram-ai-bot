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

// autopilot plays a play of seed: it takes the shortest way to the planet,
// or a bonus, keeping off walls and itself, until it has eaten meals
// planets and is long enough to run into itself, or finds no way for a
// while, and then turns the same way every tick until it crashes. It gives
// the turns, the ticks and the play's last state.
func autopilot(seed uint32, meals int) ([]int, int, *ilonSim) {
	sim := newIlonSim(seed)
	var turns []int
	lastMeal, eaten := 0, 0
	for tick := range ilonMaxTicks {
		if sim.eaten != eaten {
			lastMeal, eaten = tick, sim.eaten
		}
		dir := (sim.dir + 1) % 4
		if (sim.eaten < meals || len(sim.body)+sim.growing < 5) && tick-lastMeal < 500 {
			dir = steer(sim)
		}
		if dir != sim.dir {
			turns = append(turns, tick, dir)
			sim.turn(dir)
		}
		if sim.step() {
			return turns, tick + 1, sim
		}
	}
	panic("the autopilot never crashed")
}

// steer is the first step of the shortest way to the snake's target over
// cells it can enter now, or any safe way when there is none.
func steer(s *ilonSim) int {
	target := s.planet
	if s.bonus >= 0 {
		target = s.bonus
	}
	blocked := slices.Clone(s.walls)
	for _, cell := range s.body[:len(s.body)-1] {
		blocked[cell] = true
	}
	first := map[int]int{}
	var queue []int
	for _, dir := range []int{s.dir, (s.dir + 1) % 4, (s.dir + 3) % 4} {
		cell := s.ahead(dir)
		if _, seen := first[cell]; !seen && !blocked[cell] {
			first[cell] = dir
			queue = append(queue, cell)
		}
	}
	safe := s.dir
	if len(queue) > 0 {
		safe = first[queue[0]]
	}
	for len(queue) > 0 {
		cell := queue[0]
		queue = queue[1:]
		if cell == target {
			return first[cell]
		}
		for dir := range 4 {
			x := (cell%ilonCols + ilonDX[dir] + ilonCols) % ilonCols
			y := (cell/ilonCols + ilonDY[dir] + ilonRows) % ilonRows
			n := y*ilonCols + x
			if _, seen := first[n]; !seen && !blocked[n] {
				first[n] = first[cell]
				queue = append(queue, n)
			}
		}
	}
	return safe
}

func (s *ilonSim) ahead(dir int) int {
	head := s.body[0]
	x := (head%ilonCols + ilonDX[dir] + ilonCols) % ilonCols
	y := (head/ilonCols + ilonDY[dir] + ilonRows) % ilonRows
	return y*ilonCols + x
}

// A play is scored by playing its turns again: its levels, bonuses and
// combos count, and the score is the one the play reached.
func TestIlonPlaysAreScoredAgain(t *testing.T) {
	levels, extras := 0, 0
	for i := uint32(1); i < 60; i += 2 {
		seed := i*2654435761 | 1
		turns, ticks, played := autopilot(seed, 45)
		score, took, err := ScoreIlon(seed, turns, ticks)
		if err != nil || score != played.score || score == 0 {
			t.Fatalf("seed %d: scored %d, %v; the play reached %d", seed, score, err, played.score)
		}
		if took < time.Duration(ticks)*ilonFastestTick*time.Millisecond {
			t.Fatalf("seed %d: %d ticks took only %v", seed, ticks, took)
		}
		levels = max(levels, played.level)
		if played.score > 10*played.eaten*played.level {
			extras++
		}
	}
	if levels < 4 {
		t.Errorf("the autopilot reached level %d at most; the test should play several", levels)
	}
	if extras == 0 {
		t.Error("no play scored more than its planets; combos and stars are not counted")
	}
}

// Turns out of order, in a direction there is not, after the end, or a
// play said to end before or after its crash, are refused.
func TestIlonRefusesPlaysThatDidNotHappen(t *testing.T) {
	seed := uint32(12345)
	turns, ticks, _ := autopilot(seed, 12)
	if len(turns) < 4 {
		t.Fatal("the autopilot hardly turned")
	}
	swapped := slices.Clone(turns)
	swapped[0], swapped[2] = swapped[2], swapped[0]
	badDir := slices.Clone(turns)
	badDir[1] = 7
	for name, play := range map[string]struct {
		turns []int
		ticks int
	}{
		"early end":    {turns, ticks - 1},
		"late end":     {turns, ticks + 5},
		"out of order": {swapped, ticks},
		"bad dir":      {badDir, ticks},
		"after end":    {append(slices.Clone(turns), ticks+3, 1), ticks},
		"odd":          {turns[:len(turns)-1], ticks},
		"no ticks":     {nil, 0},
		"too long":     {nil, ilonMaxTicks + 1},
	} {
		if _, _, err := ScoreIlon(seed, play.turns, play.ticks); !errors.Is(err, ErrImplausible) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

// A play sent in quicker than its ticks last is refused; one that took
// them is scored, for the session's player.
func TestFinishIlonWantsTheTimeTheTicksTake(t *testing.T) {
	g, c := newTestGame()
	run, play, err := g.StartIlon(sessionOf(t, playURL(t, g, IlonShortName, player)))
	if err != nil {
		t.Fatal(err)
	}
	turns, ticks, played := autopilot(play.Seed, 20)
	_, took, _ := ScoreIlon(play.Seed, turns, ticks)

	c.now = c.now.Add(took / 2)
	if _, err := g.FinishIlon(run, turns, ticks); !errors.Is(err, ErrImplausible) {
		t.Fatalf("half the time: %v", err)
	}
	c.now = c.now.Add(took / 2)
	result, err := g.FinishIlon(run, turns, ticks)
	if err != nil || result.Score != played.score || result.Player != player {
		t.Fatalf("got %+v, %v; want %d for %+v", result, err, played.score, player)
	}
}

// A session opens only its own game, and a run is finished only as its own
// game's.
func TestIlonAndTaroziKeepToTheirOwnTokens(t *testing.T) {
	g, _ := newTestGame()
	ilonLink := playURL(t, g, IlonShortName, player)
	if !strings.HasPrefix(ilonLink, "https://example.com/game/ilon/?s=") {
		t.Fatalf("Kosmik ilon's page is %s", ilonLink)
	}
	ilonSession, taroziSession := sessionOf(t, ilonLink), sessionOf(t, playURL(t, g, ShortName, player))
	if _, _, err := g.Start(ilonSession); !errors.Is(err, ErrToken) {
		t.Errorf("Tarozi started from Kosmik ilon's session: %v", err)
	}
	if _, _, err := g.StartIlon(taroziSession); !errors.Is(err, ErrToken) {
		t.Errorf("Kosmik ilon started from Tarozi's session: %v", err)
	}
	taroziRun, _, _ := g.Start(taroziSession)
	if _, err := g.FinishIlon(taroziRun, nil, 1); !errors.Is(err, ErrToken) {
		t.Errorf("Tarozi's run finished as Kosmik ilon's: %v", err)
	}
	ilonRun, _, _ := g.StartIlon(ilonSession)
	if _, err := g.Finish(ilonRun, "", nil); !errors.Is(err, ErrToken) {
		t.Errorf("Kosmik ilon's run finished as Tarozi's: %v", err)
	}
	if _, ok := g.PlayURL("other", player); ok {
		t.Error("a page for a game that is not this package's")
	}
}

// The page plays the same rules: sim.js scores the autopilot's plays as
// the server does, and refuses a play said to end before its crash.
func TestIlonPageScoresAsTheServer(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	type play struct {
		Seed  uint32 `json:"seed"`
		Turns []int  `json:"turns"`
		Ticks int    `json:"ticks"`
		Score *int   `json:"score"`
	}
	var plays []play
	for i := uint32(3); i < 80; i += 7 {
		seed := i*2246822519 | 1
		turns, ticks, played := autopilot(seed, 35)
		plays = append(plays, play{seed, turns, ticks, &played.score})
	}
	plays = append(plays, play{plays[0].Seed, plays[0].Turns, plays[0].Ticks - 1, nil})
	cases, _ := json.Marshal(map[string]any{"rules": ilonRules, "plays": plays})
	input := filepath.Join(t.TempDir(), "plays.json")
	if err := os.WriteFile(input, cases, 0o600); err != nil {
		t.Fatal(err)
	}
	sim, _ := filepath.Abs("../../public/game/ilon/sim.js")
	script := `const Sim = require(process.argv[1]); const c = JSON.parse(require("fs").readFileSync(process.argv[2]));
console.log(JSON.stringify(c.plays.map(p => Sim.play(p.seed, c.rules, p.turns, p.ticks))));`
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
