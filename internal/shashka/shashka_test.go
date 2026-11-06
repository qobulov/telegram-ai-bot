package shashka

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// sq is the square a name like "c3" stands for, files a to h from White's
// left, ranks 1 to 8 from White's edge.
func sq(name string) int {
	col, row := int(name[0]-'a'), 8-int(name[1]-'0')
	i, ok := square(row, col)
	if !ok {
		panic("not a dark square: " + name)
	}
	return i
}

func name(i int) string {
	row := i / 4
	col := 2*(i%4) + 1 - row%2
	return string([]byte{byte('a' + col), byte('8' - row)})
}

func path(names ...string) []int {
	out := make([]int, len(names))
	for i, n := range names {
		out[i] = sq(n)
	}
	return out
}

// setup is a position from pieces written like "wc3 Wa1 bd4 Bh8": the
// piece as Encode writes it, then its square.
func setup(turn Side, placed string) Position {
	p := Position{Turn: turn}
	for _, f := range strings.Fields(placed) {
		at := strings.IndexByte(symbols, f[0])
		p.Squares[sq(f[1:])] = pieces[at]
	}
	return p
}

func spell(m Move) string {
	var b strings.Builder
	for i, s := range m.Path {
		if i > 0 {
			b.WriteByte('-')
			if len(m.Captured) > 0 {
				b.WriteString("x")
			}
		}
		b.WriteString(name(s))
	}
	return b.String()
}

func spellAll(moves []Move) []string {
	out := make([]string, len(moves))
	for i, m := range moves {
		out[i] = spell(m)
	}
	slices.Sort(out)
	return out
}

func wantMoves(t *testing.T, p Position, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := spellAll(p.Moves()); !slices.Equal(got, want) {
		t.Fatalf("moves = %v, want %v", got, want)
	}
}

func find(t *testing.T, p Position, spelled string) Move {
	t.Helper()
	for _, m := range p.Moves() {
		if spell(m) == spelled {
			return m
		}
	}
	t.Fatalf("no move %s among %v", spelled, spellAll(p.Moves()))
	return Move{}
}

func TestGeometry(t *testing.T) {
	if sq("a1") != 28 || sq("b8") != 0 || sq("h8") != 3 || sq("h2") != 27 {
		t.Fatalf("a1=%d b8=%d h8=%d h2=%d", sq("a1"), sq("b8"), sq("h8"), sq("h2"))
	}
	for i := range 32 {
		if sq(name(i)) != i {
			t.Fatalf("square %d is named %s", i, name(i))
		}
	}
}

func TestStart(t *testing.T) {
	p := Start()
	whites, blacks := 0, 0
	for _, piece := range p.Squares {
		switch piece {
		case WhiteMan:
			whites++
		case BlackMan:
			blacks++
		}
	}
	if whites != 12 || blacks != 12 || p.Turn != White || p.Result() != Ongoing {
		t.Fatalf("whites %d blacks %d turn %d result %d", whites, blacks, p.Turn, p.Result())
	}
	wantMoves(t, p, "a3-b4", "c3-b4", "c3-d4", "e3-d4", "e3-f4", "g3-f4", "g3-h4")
}

func TestManCapturesBackwardAndMust(t *testing.T) {
	// The black man on c3 is behind White's d4, and g3 could step freely,
	// but taking is not a choice.
	p := setup(White, "wd4 wg3 bc3 bh8")
	wantMoves(t, p, "d4-xb2")
	if m := find(t, p, "d4-xb2"); !slices.Equal(m.Captured, path("c3")) {
		t.Fatalf("captured %v", m.Captured)
	}
	// Black's men take backward too.
	wantMoves(t, setup(Black, "be5 wd6 wa1"), "e5-xc7")
}

func TestMultiJumpIsOneMove(t *testing.T) {
	p := setup(White, "wa3 bb4 bd6")
	wantMoves(t, p, "a3-xc5-xe7")
	m := find(t, p, "a3-xc5-xe7")
	if !slices.Equal(m.Path, path("a3", "c5", "e7")) || !slices.Equal(m.Captured, path("b4", "d6")) {
		t.Fatalf("move %+v", m)
	}
	after, _, err := p.Play(m.Path)
	if err != nil || after.Result() != WhiteWins || after.Squares[sq("e7")] != WhiteMan {
		t.Fatalf("after: %v %v %s", err, after.Result(), after.Board())
	}
}

func TestFreeChoiceOfCaptures(t *testing.T) {
	// Taking one on b4 or two on d4 and f6 are both allowed (no rule to
	// take the most), but stopping on e5 halfway is not.
	p := setup(White, "wc3 bb4 bd4 bf6")
	wantMoves(t, p, "c3-xa5", "c3-xe5-xg7")
}

func TestCrownedMidCaptureGoesOnAsKing(t *testing.T) {
	// The man takes c7 to be crowned on d8; as a man it could take no
	// more, but as a king it takes f6 from afar and lands on g5 or h4.
	p := setup(White, "wb6 bc7 bf6 ba5")
	wantMoves(t, p, "b6-xd8-xg5", "b6-xd8-xh4")
	after, m, err := p.Play(path("b6", "d8", "h4"))
	if err != nil || !slices.Equal(m.Captured, path("c7", "f6")) {
		t.Fatalf("play: %v %+v", err, m)
	}
	if after.Squares[sq("h4")] != WhiteKing || after.Squares[sq("c7")] != Empty || after.Squares[sq("f6")] != Empty {
		t.Fatalf("after: %s", after.Board())
	}
}

func TestManCrownedAtEnd(t *testing.T) {
	after, _, err := setup(White, "wc7 bh2").Play(path("c7", "d8"))
	if err != nil || after.Squares[sq("d8")] != WhiteKing || after.Quiet != 0 {
		t.Fatalf("%v %s quiet %d", err, after.Board(), after.Quiet)
	}
	after, _, err = setup(Black, "bf2 wa1").Play(path("f2", "g1"))
	if err != nil || after.Squares[sq("g1")] != BlackKing {
		t.Fatalf("%v %s", err, after.Board())
	}
}

func TestFlyingKing(t *testing.T) {
	// A king flies along the long diagonal.
	wantMoves(t, setup(White, "Wa1 bh6"),
		"a1-b2", "a1-c3", "a1-d4", "a1-e5", "a1-f6", "a1-g7", "a1-h8")
	// It takes from afar and lands on any free square beyond.
	wantMoves(t, setup(White, "Wa1 bd4"), "a1-xe5", "a1-xf6", "a1-xg7", "a1-xh8")
	// But where it can take again from one of them it must land there:
	// only f6 sees g5.
	p := setup(White, "Wa1 bd4 bg5")
	wantMoves(t, p, "a1-xf6-xh4")
	if m := find(t, p, "a1-xf6-xh4"); !slices.Equal(m.Captured, path("d4", "g5")) {
		t.Fatalf("captured %v", m.Captured)
	}
	// Two pieces in a row, or a piece with no square behind it, cannot be
	// taken.
	wantMoves(t, setup(White, "Wa1 bd4 be5 bh2"), "a1-b2", "a1-c3")
	// King moves count toward the draw; a man's move does not.
	after, _, _ := setup(White, "Wa1 bh6").Play(path("a1", "h8"))
	if after.Quiet != 1 {
		t.Fatalf("quiet %d", after.Quiet)
	}
}

func TestTurkishStrike(t *testing.T) {
	// The man goes round the square and comes back to c3, where d4, taken
	// already but still on the board, may not be taken again, though e5
	// beyond it is empty by then. Either way round is a move of its own.
	p := setup(White, "wc3 bd4 bf4 bf2 bd2")
	wantMoves(t, p, "c3-xe5-xg3-xe1-xc3", "c3-xe1-xg3-xe5-xc3")
	after, m, err := p.Play(path("c3", "e5", "g3", "e1", "c3"))
	if err != nil || !slices.Equal(m.Captured, path("d4", "f4", "f2", "d2")) || after.Result() != WhiteWins {
		t.Fatalf("%v %+v %s", err, m, after.Board())
	}

	// The king takes e3, e5, b4 and b2 round a rectangle back to c1, where
	// e3, taken but not lifted, stands between it and g5 with h6 free
	// behind: it may not cross e3, so the move ends on c1. Going the other
	// way round, from c5 it reaches e3 instead and the loop is broken.
	p = setup(White, "Wc1 be3 be5 bb4 bb2 bg5")
	wantMoves(t, p,
		"c1-xf4-xd6-xa3-xc1", "c1-xf4-xh6",
		"c1-xa3-xc5-xf2", "c1-xa3-xc5-xg1",
		"c1-xa3-xd6-xf4-xc1", "c1-xa3-xd6-xf4-xd2", "c1-xa3-xd6-xf4-xh6",
		"c1-xa3-xe7-xh4")
	loop := find(t, p, "c1-xf4-xd6-xa3-xc1")
	if !slices.Equal(loop.Captured, path("e3", "e5", "b4", "b2")) {
		t.Fatalf("captured %v", loop.Captured)
	}
	for _, m := range p.Moves() {
		checkMove(t, p, m)
	}
}

// checkMove fails unless each step of m runs along one diagonal over
// exactly one piece, the enemy one it lists as captured and not taken
// before, onto a free square: the rules a move must keep, checked apart
// from the code that makes them.
func checkMove(t *testing.T, p Position, m Move) {
	t.Helper()
	board := p.Squares
	board[m.Path[0]] = Empty
	taken := map[int]bool{}
	for i := 1; i < len(m.Path); i++ {
		between, ok := diagonal(m.Path[i-1], m.Path[i])
		if !ok || board[m.Path[i]] != Empty {
			t.Fatalf("%s: step %d is not to a free square on a diagonal", spell(m), i)
		}
		var pieces []int
		for _, s := range between {
			if board[s] != Empty {
				pieces = append(pieces, s)
			}
		}
		if len(m.Captured) == 0 {
			if len(pieces) > 0 {
				t.Fatalf("%s: a simple move passes over a piece", spell(m))
			}
			continue
		}
		c := m.Captured[i-1]
		if len(pieces) != 1 || pieces[0] != c || taken[c] || board[c].side() == p.Turn {
			t.Fatalf("%s: step %d passes over %v, captures %s", spell(m), i, pieces, name(c))
		}
		taken[c] = true
	}
}

// diagonal is the squares strictly between from and to, when they are on
// one diagonal.
func diagonal(from, to int) ([]int, bool) {
	for d := range dirs {
		for k, s := range rays[from][d] {
			if s == to {
				return rays[from][d][:k], true
			}
		}
	}
	return nil, false
}

func TestMovesAreUnique(t *testing.T) {
	for _, p := range []Position{
		Start(),
		setup(White, "Wc1 be3 be5 bb4 bb2 bg5 bh8"),
		setup(White, "Wa1 Wh2 bc3 bf6 bd6 bb6 bg3 be7"),
		setup(Black, "Bb8 Bh8 wc7 wf6 wd4 wb4 wf2 wc3"),
	} {
		seen := map[string]bool{}
		for _, m := range p.Moves() {
			if s := spell(m); seen[s] {
				t.Fatalf("%s listed twice in %s", s, p.Encode())
			}
			seen[spell(m)] = true
			if len(m.Captured) > 0 && len(m.Captured) != len(m.Path)-1 {
				t.Fatalf("%s: path %v captured %v", spell(m), m.Path, m.Captured)
			}
			checkMove(t, p, m)
		}
	}
}

func TestPlayRejectsIllegal(t *testing.T) {
	p := setup(White, "wc3 bb4 bd4 bf6 wh2")
	for _, bad := range [][]int{
		nil,
		path("c3"),
		path("h2", "g3"), // a capture is due
		path("c3", "e5"), // a capture left half done
		path("c3", "e5", "g7", "h8"),
		path("b4", "a3"), // not White's piece
		{sq("c3"), 99},
	} {
		after, _, err := p.Play(bad)
		if !errors.Is(err, ErrIllegal) || after != p {
			t.Fatalf("Play(%v) = %v", bad, err)
		}
	}
	if _, _, err := Start().Play(path("c3", "c5")); !errors.Is(err, ErrIllegal) {
		t.Fatalf("straight move: %v", err)
	}
	if _, _, err := Start().Play(path("c3", "d4")); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeDecode(t *testing.T) {
	if got := Start().Encode(); got != "bbbbbbbbbbbb........wwwwwwwwwwww:w:0" {
		t.Fatalf("start = %s", got)
	}
	for _, p := range []Position{
		Start(),
		{Squares: setup(White, "Wa1 bh6 Bc5 wg3").Squares, Turn: Black, Quiet: 17},
	} {
		back, err := Decode(p.Encode())
		if err != nil || back != p {
			t.Fatalf("Decode(%s) = %+v, %v", p.Encode(), back, err)
		}
		if p.Board() != strings.Split(p.Encode(), ":")[0] {
			t.Fatalf("board %s", p.Board())
		}
	}
	empty := strings.Repeat(".", 32)
	for _, bad := range []string{
		"",
		empty,
		empty + ":w",
		empty + ":w:0:0",
		empty[1:] + ":w:0",
		empty + ".:w:0",
		"x" + empty[1:] + ":w:0",
		empty + ":x:0",
		empty + ":W:0",
		empty + ":w:-1",
		empty + ":w:+1",
		empty + ":w:01",
		empty + ":w:",
		empty + ":w:1e3",
		"w" + empty[1:] + ":w:0",               // a white man on row 0
		empty[:31] + "b:b:0",                   // a black man on row 7
		"bbbbbbbbbbbbb.......wwwwwwwwwwww:w:0", // thirteen black pieces
		"BBBBBBBBBBBB.......Wwwwwwwwwwwww:w:0", // thirteen white pieces
	} {
		if _, err := Decode(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Decode(%q) = %v", bad, err)
		}
	}
}

func TestResult(t *testing.T) {
	if r := setup(White, "bd4").Result(); r != BlackWins {
		t.Fatalf("no white pieces: %d", r)
	}
	if r := setup(Black, "wd4").Result(); r != WhiteWins {
		t.Fatalf("no black pieces: %d", r)
	}
	// White's man on a1 is blocked by b2, which c3 guards.
	if r := setup(White, "wa1 bb2 bc3").Result(); r != BlackWins {
		t.Fatalf("blocked: %d", r)
	}
	quiet := setup(White, "Wa1 Bh2")
	quiet.Quiet = DrawPlies - 1
	if quiet.Result() != Ongoing {
		t.Fatal("drawn a ply early")
	}
	after, _, _ := quiet.Play(path("a1", "b2"))
	if after.Result() != Draw {
		t.Fatalf("quiet %d result %d", after.Quiet, after.Result())
	}
	// A capture, or a man's move, starts the count again.
	p := setup(White, "Wa1 bc3 wh2 Bh8")
	p.Quiet = 20
	if after, _, _ := p.Play(path("a1", "d4")); after.Quiet != 0 {
		t.Fatalf("after capture quiet %d", after.Quiet)
	}
	p = setup(White, "Wa1 wh2 Bh6")
	p.Quiet = 20
	if after, _, _ := p.Play(path("h2", "g3")); after.Quiet != 0 {
		t.Fatalf("after man move quiet %d", after.Quiet)
	}
}
