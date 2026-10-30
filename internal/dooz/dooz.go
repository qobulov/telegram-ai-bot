// Package dooz is noughts and crosses, dooz in Uzbek, as the bot plays it
// under a message: two members of a chat, or one and the bot, take turns on
// nine buttons. The whole game, its board, players, level and the series'
// score, is written into the buttons' data (see Encode), so no store keeps
// it and any instance can answer the next press.
package dooz

import (
	"errors"
	"strconv"
	"strings"
)

// The marks a cell holds.
const (
	Empty byte = '_'
	X     byte = 'x'
	O     byte = 'o'
)

// Level is how the bot plays, or Human when both players are people.
type Level byte

const (
	Human  Level = '-'
	Easy   Level = 'e'
	Medium Level = 'm'
	Hard   Level = 'h'
)

// maxScore caps a series' score, which must fit in a button's data.
const maxScore = 99

var (
	ErrTaken   = errors.New("dooz: the cell is taken")
	ErrOver    = errors.New("dooz: the game is over")
	ErrInvalid = errors.New("dooz: not a game")
)

// lines are the rows, columns and diagonals that win.
var lines = [8][3]int{
	{0, 1, 2}, {3, 4, 5}, {6, 7, 8},
	{0, 3, 6}, {1, 4, 7}, {2, 5, 8},
	{0, 4, 8}, {2, 4, 6},
}

// Game is a game in play or over. X moves first.
type Game struct {
	Board [9]byte
	// X and O are the players' user IDs; the bot's side is 0.
	X, O  int64
	Level Level
	// ScoreX and ScoreO are the games won in this series by whoever holds
	// X and O now.
	ScoreX, ScoreO int
}

// New is an empty board for x and o; a side the bot plays is 0, at level.
func New(x, o int64, level Level) Game {
	g := Game{X: x, O: o, Level: level}
	for i := range g.Board {
		g.Board[i] = Empty
	}
	return g
}

// Turn is the mark that moves next.
func (g Game) Turn() byte {
	return turn(g.Board)
}

func turn(board [9]byte) byte {
	xs, os := 0, 0
	for _, c := range board {
		switch c {
		case X:
			xs++
		case O:
			os++
		}
	}
	if xs > os {
		return O
	}
	return X
}

// Player is the user who holds mark, 0 for the bot.
func (g Game) Player(mark byte) int64 {
	if mark == X {
		return g.X
	}
	return g.O
}

// Bots reports whether the bot holds mark.
func (g Game) Bots(mark byte) bool {
	return g.Level != Human && g.Player(mark) == 0
}

// Winner is the mark that has a line, and the line; Empty when none has.
func (g Game) Winner() (byte, [3]int) {
	return winner(g.Board)
}

func winner(board [9]byte) (byte, [3]int) {
	for _, line := range lines {
		if c := board[line[0]]; c != Empty && c == board[line[1]] && c == board[line[2]] {
			return c, line
		}
	}
	return Empty, [3]int{}
}

// Over reports a game won or with no cell left.
func (g Game) Over() bool {
	mark, _ := g.Winner()
	return mark != Empty || full(g.Board)
}

func full(board [9]byte) bool {
	for _, c := range board {
		if c == Empty {
			return false
		}
	}
	return true
}

// Play puts the mark whose turn it is in cell, and counts the win it makes.
func (g *Game) Play(cell int) error {
	switch {
	case g.Over():
		return ErrOver
	case cell < 0 || cell >= len(g.Board):
		return ErrInvalid
	case g.Board[cell] != Empty:
		return ErrTaken
	}
	g.Board[cell] = g.Turn()
	switch mark, _ := g.Winner(); mark {
	case X:
		g.ScoreX = min(g.ScoreX+1, maxScore)
	case O:
		g.ScoreO = min(g.ScoreO+1, maxScore)
	}
	return nil
}

// Rematch is the next game of the series: the players change marks, so
// whoever moved second moves first, and keep their scores.
func (g Game) Rematch() Game {
	next := New(g.O, g.X, g.Level)
	next.ScoreX, next.ScoreO = g.ScoreO, g.ScoreX
	return next
}

// Encode writes the game as "board:x:o:level:scoreX:scoreO", the IDs in
// base 36: at most 45 bytes, which leaves room in a button's 64.
func (g Game) Encode() string {
	return strings.Join([]string{
		string(g.Board[:]),
		strconv.FormatInt(g.X, 36),
		strconv.FormatInt(g.O, 36),
		string(g.Level),
		strconv.Itoa(g.ScoreX),
		strconv.Itoa(g.ScoreO),
	}, ":")
}

// Decode reads what Encode wrote. A board no game could reach, or a bot
// game without the bot, is ErrInvalid: the data comes from a button, which
// anyone can send forged.
func Decode(s string) (Game, error) {
	fields := strings.Split(s, ":")
	if len(fields) != 6 || len(fields[0]) != 9 || len(fields[3]) != 1 {
		return Game{}, ErrInvalid
	}
	g := Game{Level: Level(fields[3][0])}
	copy(g.Board[:], fields[0])
	x, errX := strconv.ParseInt(fields[1], 36, 64)
	o, errO := strconv.ParseInt(fields[2], 36, 64)
	scoreX, errSX := strconv.Atoi(fields[4])
	scoreO, errSO := strconv.Atoi(fields[5])
	if err := errors.Join(errX, errO, errSX, errSO); err != nil {
		return Game{}, ErrInvalid
	}
	g.X, g.O, g.ScoreX, g.ScoreO = x, o, scoreX, scoreO
	if !g.valid() {
		return Game{}, ErrInvalid
	}
	return g, nil
}

func (g Game) valid() bool {
	xs, os := 0, 0
	for _, c := range g.Board {
		switch c {
		case X:
			xs++
		case O:
			os++
		case Empty:
		default:
			return false
		}
	}
	if g.X < 0 || g.O < 0 || g.ScoreX < 0 || g.ScoreO < 0 || g.ScoreX > maxScore || g.ScoreO > maxScore {
		return false
	}
	switch g.Level {
	case Human:
		if g.X == 0 || g.O == 0 || g.X == g.O {
			return false
		}
	case Easy, Medium, Hard:
		if (g.X == 0) == (g.O == 0) {
			return false
		}
	default:
		return false
	}
	return xs == os || xs == os+1
}
