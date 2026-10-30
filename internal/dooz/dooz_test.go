package dooz

import (
	"errors"
	"testing"
)

func first(int) int { return 0 }

func board(s string) [9]byte {
	var b [9]byte
	copy(b[:], s)
	return b
}

func TestPlayCountsTheWin(t *testing.T) {
	g := New(11, 22, Human)
	for _, cell := range []int{0, 3, 1, 4} {
		if err := g.Play(cell); err != nil {
			t.Fatalf("Play(%d) = %v", cell, err)
		}
	}
	if err := g.Play(3); !errors.Is(err, ErrTaken) {
		t.Fatalf("Play on a taken cell = %v, want ErrTaken", err)
	}
	if err := g.Play(2); err != nil {
		t.Fatal(err)
	}
	mark, line := g.Winner()
	if mark != X || line != [3]int{0, 1, 2} || !g.Over() || g.ScoreX != 1 || g.ScoreO != 0 {
		t.Fatalf("after the top row: winner %c %v, over %v, score %d:%d", mark, line, g.Over(), g.ScoreX, g.ScoreO)
	}
	if err := g.Play(8); !errors.Is(err, ErrOver) {
		t.Fatalf("Play after the win = %v, want ErrOver", err)
	}
	next := g.Rematch()
	if next.X != 22 || next.O != 11 || next.ScoreX != 0 || next.ScoreO != 1 || next.Over() || next.Turn() != X {
		t.Fatalf("Rematch = %+v: the players should change marks and keep their scores", next)
	}
}

func TestEncodeRoundTrips(t *testing.T) {
	g := New(7_912_345_678, 0, Hard)
	g.Board = board("xo_x_o___")
	g.ScoreX, g.ScoreO = 12, 99
	data := g.Encode()
	if len("dz:8:")+len(data) > 64 {
		t.Fatalf("%q does not fit in a button's 64 bytes", data)
	}
	back, err := Decode(data)
	if err != nil || back != g {
		t.Fatalf("Decode(%q) = %+v, %v; want %+v", data, back, err, g)
	}
}

func TestDecodeRefusesForgedGames(t *testing.T) {
	for _, data := range []string{
		"",
		"xxxx_____:b:c:-:0:0",   // X played twice more than O
		"o________:b:c:-:0:0",   // O first
		"_________:b:b:-:0:0",   // one person on both sides
		"_________:b:0:-:0:0",   // a game of people with the bot in it
		"_________:b:c:h:0:0",   // a bot game without the bot
		"_________:0:0:h:0:0",   // the bot against itself
		"_________:b:0:z:0:0",   // no such level
		"_________:b:0:h:100:0", // a score past the cap
		"_________:b:0:h:-1:0",
		"____a____:b:0:h:0:0",
		"_________:-b:0:h:0:0",
	} {
		if g, err := Decode(data); !errors.Is(err, ErrInvalid) {
			t.Errorf("Decode(%q) = %+v, %v; want ErrInvalid", data, g, err)
		}
	}
}

func TestEasyAndMediumTakeAWin(t *testing.T) {
	for _, level := range []Level{Easy, Medium} {
		g := New(5, 0, level)
		g.Board = board("xx_oo____")
		g.Board[8] = X // X has moved three times, O twice: O's turn
		if cell := g.Move(first); cell != 5 {
			t.Errorf("level %c played %d, want the win at 5", level, cell)
		}
	}
}

func TestMediumBlocks(t *testing.T) {
	g := New(5, 0, Medium)
	g.Board = board("xx__o____")
	if cell := g.Move(func(n int) int { return n - 1 }); cell != 2 {
		t.Fatalf("medium played %d, want the block at 2", cell)
	}
}

// Hard never loses, whichever side it plays and whatever the other does.
func TestHardNeverLoses(t *testing.T) {
	for _, bot := range []byte{X, O} {
		g := New(5, 0, Hard)
		if bot == X {
			g = New(0, 5, Hard)
		}
		var games int
		var play func(g Game)
		play = func(g Game) {
			if g.Over() {
				games++
				if mark, _ := g.Winner(); mark == other(bot) {
					t.Fatalf("the bot playing %c lost: %s", bot, g.Board[:])
				}
				return
			}
			if g.Bots(g.Turn()) {
				if err := g.Play(g.Move(first)); err != nil {
					t.Fatal(err)
				}
				play(g)
				return
			}
			for _, cell := range freeCells(g.Board) {
				next := g
				if err := next.Play(cell); err != nil {
					t.Fatal(err)
				}
				play(next)
			}
		}
		play(g)
		if games == 0 {
			t.Fatal("no game was played")
		}
	}
}
