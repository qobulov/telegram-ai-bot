package shashka

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// last picks the last choice, which keeps Easy from playing at random.
func last(n int) int { return n - 1 }

func first(int) int { return 0 }

func seeded(seed uint64) func(n int) int {
	return rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)).IntN
}

func TestBotTakesTheWinningCapture(t *testing.T) {
	// Both captures are legal; only c3 taking d4 and f6 leaves Black
	// nothing, while g5 taking f6 loses c3 to d4.
	p := setup(White, "wc3 wg5 bd4 bf6")
	for _, level := range []Level{Easy, Medium, Hard} {
		picks := []func(int) int{last, seeded(1), seeded(2)}
		if level == Easy {
			// A quarter of Easy's moves are at random, by design.
			picks = picks[:1]
		}
		for _, pick := range picks {
			if got := spell(BestMove(p, level, pick)); got != "c3-xe5-xg7" {
				t.Fatalf("level %d plays %s", level, got)
			}
		}
	}
}

func TestBotPlaysAForcedMove(t *testing.T) {
	p := setup(White, "wd4 wg3 bc3 bh8")
	if got := spell(BestMove(p, Hard, first)); got != "d4-xb2" {
		t.Fatalf("plays %s", got)
	}
}

func TestBotSacrificesToWinAKing(t *testing.T) {
	// c3-d4 gives the man away, but Black's king must take it and lands
	// on c3 or b2, where White's king on a1 takes it in turn.
	p := setup(White, "Wa1 wc3 Bh8 bb6")
	for _, level := range []Level{Medium, Hard} {
		for seed := range uint64(3) {
			if got := spell(BestMove(p, level, seeded(seed))); got != "c3-d4" {
				t.Fatalf("level %d plays %s", level, got)
			}
		}
	}
}

func TestBotDoesNotHangPieces(t *testing.T) {
	// e3-d4 and g3-f4 give a man to e5 for nothing; e3-f4, covered by
	// g3, and g3-h4 are safe.
	p := setup(White, "we3 wg3 be5 bh8")
	for seed := range uint64(4) {
		m := BestMove(p, Hard, seeded(seed))
		after, _, _ := p.Play(m.Path)
		for _, reply := range after.Moves() {
			if slices.Contains(reply.Captured, m.Path[len(m.Path)-1]) {
				t.Fatalf("%s hangs the man to %s", spell(m), spell(reply))
			}
		}
	}
}

func TestBotMovesAreLegal(t *testing.T) {
	for seed := range uint64(6) {
		pick := seeded(seed)
		p := Start()
		for ply := 0; ply < 200 && p.Result() == Ongoing; ply++ {
			m := BestMove(p, Level(1+ply%3), pick)
			var err error
			if p, _, err = p.Play(m.Path); err != nil {
				t.Fatalf("seed %d ply %d: %s is illegal", seed, ply, spell(m))
			}
		}
	}
}

// play is a game from the start between white and black at their levels,
// to its end or 300 plies.
func play(white, black Level, pick func(int) int) Result {
	p := Start()
	for ply := 0; ply < 300; ply++ {
		if r := p.Result(); r != Ongoing {
			return r
		}
		level := white
		if p.Turn == Black {
			level = black
		}
		p, _, _ = p.Play(BestMove(p, level, pick).Path)
	}
	return Draw
}

func TestHardBeatsEasy(t *testing.T) {
	if testing.Short() {
		t.Skip("self-play takes seconds")
	}
	wins := 0
	for game := range uint64(4) {
		hardIsWhite := game%2 == 0
		var r Result
		if hardIsWhite {
			r = play(Hard, Easy, seeded(game))
		} else {
			r = play(Easy, Hard, seeded(game))
		}
		switch {
		case r == WhiteWins && hardIsWhite, r == BlackWins && !hardIsWhite:
			wins++
		case r != Draw:
			t.Fatalf("game %d: Hard lost to Easy", game)
		}
	}
	if wins < 3 {
		t.Fatalf("Hard won %d of 4 against Easy", wins)
	}
}

func BenchmarkBestMoveHard(b *testing.B) {
	for _, bc := range []struct {
		name string
		p    Position
	}{
		{"start", Start()},
		{"middle", setup(White, "wa1 wc1 we1 wb2 wd2 wf2 wc3 we3 wg3 wd4 bf6 bc5 bh6 bb6 bd6 bc7 be7 bg7 bd8 bf8")},
		{"kings", setup(White, "Wa1 Wc1 We1 wg3 Bh8 Bf8 Bb8 bc7")},
	} {
		b.Run(bc.name, func(b *testing.B) {
			for b.Loop() {
				BestMove(bc.p, Hard, first)
			}
		})
	}
}
