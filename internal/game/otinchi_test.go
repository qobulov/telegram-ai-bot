package game

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// woodcutter chops a play of seed gap ms apart: from the side neither the
// piece in reach nor the one above has a branch on, until it has chopped
// chops pieces, and then, at the first branch near, from that branch's
// side, which ends the play. It gives the sides, the times and the score.
func woodcutter(seed uint32, chops, gap int) (string, []int, int) {
	tree := &otinchiTree{random: seed}
	var sides strings.Builder
	var times []int
	score := 0
	for i := 0; ; i++ {
		here, golden := tree.at(i)
		above, _ := tree.at(i + 1)
		side := sideLeft
		if here == sideLeft || above == sideLeft {
			side = sideRight
		}
		if i >= chops && above != sideNone {
			side = above
		} else if i >= chops && here != sideNone {
			side = here
		}
		sides.WriteByte(map[int]byte{sideLeft: 'L', sideRight: 'R'}[side])
		times = append(times, i*gap)
		if here == side || above == side {
			return sides.String(), times, score
		}
		if golden {
			score += otinchiGolden
		} else {
			score++
		}
	}
}

// A play is scored by chopping it again: golden pieces count five, and the
// branch that ends it is the last chop.
func TestOtinchiPlaysAreScoredAgain(t *testing.T) {
	golden := 0
	for i := uint32(1); i < 40; i += 2 {
		seed := i*2654435761 | 1
		sides, times, want := woodcutter(seed, 150, 120)
		score, err := ScoreOtinchi(seed, sides, times)
		if err != nil || score != want || score < 150 {
			t.Fatalf("seed %d: scored %d, %v; the play reached %d", seed, score, err, want)
		}
		if score > len(sides)-1 {
			golden++
		}
	}
	if golden == 0 {
		t.Error("no play chopped a golden piece")
	}
}

// Chops quicker than a hand, after the time ran out, after the branch that
// ended the play, or not L or R, are refused.
func TestOtinchiRefusesPlaysThatDidNotHappen(t *testing.T) {
	seed := uint32(4242)
	sides, times, _ := woodcutter(seed, 30, 150)
	end := times[len(times)-1]
	quick := append([]int(nil), times...)
	quick[5] = quick[4] + otinchiMinGap - 1
	slow := append([]int(nil), times...)
	for i := 10; i < len(slow); i++ {
		slow[i] += 20000
	}
	for name, play := range map[string]struct {
		sides string
		times []int
	}{
		"too quick":         {sides, quick},
		"after the time":    {sides, slow},
		"after the branch":  {sides + "LR", append(append([]int(nil), times...), end+150, end+300)},
		"not a side":        {strings.Replace(sides, "L", "X", 1), times},
		"not from zero":     {sides, append([]int{1}, times[1:]...)},
		"one time too many": {sides, append(append([]int(nil), times...), end+150)},
		"nothing":           {"", nil},
	} {
		if _, err := ScoreOtinchi(seed, play.sides, play.times); !errors.Is(err, ErrImplausible) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

// A play sent in sooner than its chops took is refused; one played out is
// scored, for the session's player, and only as O'tinchi's.
func TestFinishOtinchiWantsTheTimeTheChopsTake(t *testing.T) {
	g, c := newTestGame()
	link := playURL(t, g, OtinchiShortName, player)
	if !strings.HasPrefix(link, "https://example.com/game/otinchi/?s=") {
		t.Fatalf("O'tinchi's page is %s", link)
	}
	session := sessionOf(t, link)
	if _, _, err := g.StartIlon(session); !errors.Is(err, ErrToken) {
		t.Errorf("Kosmik ilon started from O'tinchi's session: %v", err)
	}
	run, play, err := g.StartOtinchi(session)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.FinishIlon(run, nil, 1); !errors.Is(err, ErrToken) {
		t.Errorf("O'tinchi's run finished as Kosmik ilon's: %v", err)
	}
	sides, times, want := woodcutter(play.Seed, 40, 200)
	took := time.Duration(times[len(times)-1]) * time.Millisecond
	c.now = c.now.Add(took / 3)
	if _, err := g.FinishOtinchi(run, sides, times); !errors.Is(err, ErrImplausible) {
		t.Fatalf("a third of the time: %v", err)
	}
	c.now = c.now.Add(took)
	result, err := g.FinishOtinchi(run, sides, times)
	if err != nil || result.Score != want || result.Player != player {
		t.Fatalf("got %+v, %v; want %d for %+v", result, err, want, player)
	}
}

// The page plays the same rules: sim.js scores the woodcutter's plays as
// the server does, and refuses what the server refuses.
func TestOtinchiPageScoresAsTheServer(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	type play struct {
		Seed  uint32 `json:"seed"`
		Sides string `json:"sides"`
		Times []int  `json:"times"`
		Score *int   `json:"score"`
	}
	var plays []play
	for i := uint32(3); i < 80; i += 7 {
		seed := i*2246822519 | 1
		sides, times, want := woodcutter(seed, 120+int(i), 110)
		plays = append(plays, play{seed, sides, times, &want})
	}
	first := plays[0]
	late := append([]int(nil), first.Times...)
	for i := 3; i < len(late); i++ {
		late[i] += 30000
	}
	end := first.Times[len(first.Times)-1]
	plays = append(plays,
		play{first.Seed, first.Sides, late, nil},
		play{first.Seed, first.Sides + "L", append(append([]int(nil), first.Times...), end+150), nil})
	cases, _ := json.Marshal(map[string]any{"rules": otinchiRules, "plays": plays})
	input := filepath.Join(t.TempDir(), "plays.json")
	if err := os.WriteFile(input, cases, 0o600); err != nil {
		t.Fatal(err)
	}
	sim, _ := filepath.Abs("../../public/game/otinchi/sim.js")
	script := `const Game = require(process.argv[1]); const c = JSON.parse(require("fs").readFileSync(process.argv[2]));
console.log(JSON.stringify(c.plays.map(p => Game.play(p.seed, c.rules, p.sides, p.times))));`
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
			t.Errorf("play %d: the page scored %d a play the server refuses", i, *scores[i])
		case p.Score != nil && (scores[i] == nil || *scores[i] != *p.Score):
			t.Errorf("play %d (seed %d, %d chops): the page scored %v, the server %d", i, p.Seed, len(p.Sides), scores[i], *p.Score)
		}
	}
}
