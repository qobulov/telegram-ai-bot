// Package shashka is Russian draughts, shashki, as the bot plays it: men
// that step forward and take both ways, flying kings, capture that must be
// made and, once begun, finished, with the pieces taken lifted only when
// the move is over. A Position is the whole game, written as a short string
// by Encode, so a store or a button can keep it and any instance can play
// the next move.
package shashka

import (
	"errors"
	"strconv"
	"strings"
)

// Piece is what a square holds: its sign is the side, 2 is a king.
type Piece int8

const (
	Empty     Piece = 0
	WhiteMan  Piece = 1
	WhiteKing Piece = 2
	BlackMan  Piece = -1
	BlackKing Piece = -2
)

// Side is a player; its value is the sign of its pieces.
type Side int8

const (
	White Side = 1
	Black Side = -1
)

// Result is how a game stands.
type Result int8

const (
	Ongoing Result = iota
	WhiteWins
	BlackWins
	Draw
)

// DrawPlies is how many plies in a row of king moves without a capture,
// fifteen moves a side, end the game in a draw.
const DrawPlies = 30

var (
	ErrIllegal = errors.New("shashka: not a legal move")
	ErrInvalid = errors.New("shashka: not a position")
)

// Position is a game between moves. Squares are the 32 dark squares, 0..31,
// row-major from the top of the board as White sees it: row = i/4, and
// col = 2*(i%4)+1 on even rows, 2*(i%4) on odd ones, so square 28 is a1.
// Black's men start on 0..11 and move down, crowned on row 7; White's start
// on 20..31, move up and first, and are crowned on row 0.
type Position struct {
	Squares [32]Piece
	Turn    Side
	// Quiet counts consecutive plies in which only kings moved and nothing
	// was captured (draw rule).
	Quiet int
}

// Move is one whole move: Path is the squares visited, from, then each
// landing square, so a simple move is 2 long; Captured is the squares of
// the pieces taken, in order. Path alone tells moves apart: with each step
// along one diagonal over one piece, the landings fix what was taken.
type Move struct {
	Path     []int
	Captured []int
}

// Start is the opening position, White to move.
func Start() Position {
	p := Position{Turn: White}
	for i := range 12 {
		p.Squares[i] = BlackMan
		p.Squares[31-i] = WhiteMan
	}
	return p
}

func (piece Piece) side() Side {
	switch {
	case piece > 0:
		return White
	case piece < 0:
		return Black
	}
	return 0
}

func (piece Piece) king() bool {
	return piece == WhiteKing || piece == BlackKing
}

// dirs are the four diagonals as (row, col) steps; White's men go the first
// two, Black's the last two.
var dirs = [4][2]int{{-1, -1}, {-1, 1}, {1, -1}, {1, 1}}

func forward(side Side, dir int) bool {
	return (side == White) == (dir < 2)
}

// rays[sq][dir] are the squares from sq along dir to the edge, nearest
// first, worked out once so that moving never does geometry.
var rays = func() (r [32][4][]int) {
	for sq := range 32 {
		row := sq / 4
		col := 2*(sq%4) + 1 - row%2
		for d, dir := range dirs {
			for k := 1; ; k++ {
				next, ok := square(row+k*dir[0], col+k*dir[1])
				if !ok {
					break
				}
				r[sq][d] = append(r[sq][d], next)
			}
		}
	}
	return r
}()

func square(row, col int) (int, bool) {
	if row < 0 || row > 7 || col < 0 || col > 7 || (row+col)%2 == 0 {
		return 0, false
	}
	return row*4 + col/2, true
}

// crowns reports whether sq is on the row where side's men become kings.
func crowns(side Side, sq int) bool {
	if side == White {
		return sq < 4
	}
	return sq >= 28
}

// move is a Move without allocation, as the search makes millions: path
// holds plen squares and caps the ncap squares taken.
type move struct {
	path [32]int8
	caps [32]int8
	plen int8
	ncap int8
}

func (m *move) public() Move {
	out := Move{Path: make([]int, m.plen)}
	for i := range out.Path {
		out.Path[i] = int(m.path[i])
	}
	for i := range m.ncap {
		out.Captured = append(out.Captured, int(m.caps[i]))
	}
	return out
}

func (m *move) follows(path []int) bool {
	if len(path) != int(m.plen) {
		return false
	}
	for i, sq := range path {
		if int(m.path[i]) != sq {
			return false
		}
	}
	return true
}

// Moves are all the legal moves for the side to move: its captures, every
// complete sequence of them, when it has any, else its simple moves.
func (p Position) Moves() []Move {
	internal := p.moves(nil)
	out := make([]Move, len(internal))
	for i := range internal {
		out[i] = internal[i].public()
	}
	return out
}

// moves writes p's legal moves over buf. Each comes once without a check:
// sequences part at a different direction or landing, so their paths do.
func (p *Position) moves(buf []move) []move {
	buf = buf[:0]
	for sq, piece := range p.Squares {
		if piece != Empty && piece.side() == p.Turn {
			buf = p.captures(sq, buf)
		}
	}
	if len(buf) > 0 {
		return buf
	}
	for sq, piece := range p.Squares {
		if piece != Empty && piece.side() == p.Turn {
			buf = p.steps(sq, buf)
		}
	}
	return buf
}

// steps are the simple moves of the piece on from: a man one square
// forward, a king any distance along a free diagonal.
func (p *Position) steps(from int, buf []move) []move {
	piece := p.Squares[from]
	for d := range dirs {
		if !piece.king() && !forward(p.Turn, d) {
			continue
		}
		for _, to := range rays[from][d] {
			if p.Squares[to] != Empty {
				break
			}
			m := move{plen: 2}
			m.path[0], m.path[1] = int8(from), int8(to)
			buf = append(buf, m)
			if !piece.king() {
				break
			}
		}
	}
	return buf
}

// captures are the complete capture sequences of the piece on from.
func (p *Position) captures(from int, buf []move) []move {
	c := chase{board: p.Squares, side: p.Turn, out: buf}
	// The piece is lifted, so it may cross or come back to where it began.
	c.board[from] = Empty
	c.m.path[0], c.m.plen = int8(from), 1
	c.jumps(from, p.Squares[from].king())
	return c.out
}

// chase follows one piece through its captures. The pieces it takes stay
// on the board, marked in taken, until the move is over (the Turkish
// strike rule): they block, and none is taken twice.
type chase struct {
	board [32]Piece
	side  Side
	taken uint32
	m     move
	out   []move
}

// jumps adds to out every complete sequence going on from sq. A capture
// must be continued while one is there: a man crowned on the way goes on
// as a king, and a king that could land where it takes again must land
// there.
func (c *chase) jumps(sq int, king bool) {
	for d := range dirs {
		over, lands := c.target(sq, d, king)
		if len(lands) == 0 {
			continue
		}
		c.taken |= 1 << over
		c.m.caps[c.m.ncap] = int8(over)
		c.m.ncap++
		var further [8]bool
		onward := false
		for i, land := range lands {
			further[i] = c.canJump(land, king || crowns(c.side, land))
			onward = onward || further[i]
		}
		for i, land := range lands {
			if onward && !further[i] {
				continue
			}
			c.m.path[c.m.plen] = int8(land)
			c.m.plen++
			if further[i] {
				c.jumps(land, king || crowns(c.side, land))
			} else {
				c.out = append(c.out, c.m)
			}
			c.m.plen--
		}
		c.m.ncap--
		c.taken &^= 1 << over
	}
}

func (c *chase) canJump(sq int, king bool) bool {
	for d := range dirs {
		if _, lands := c.target(sq, d, king); len(lands) > 0 {
			return true
		}
	}
	return false
}

// target is the enemy piece that a piece on sq takes along dir, and the
// squares it may land on beyond: next to it for a man, any free one before
// the next piece for a king. No squares when there is nothing to take.
func (c *chase) target(sq, dir int, king bool) (int, []int) {
	ray := rays[sq][dir]
	i := 0
	for king && i < len(ray) && c.board[ray[i]] == Empty {
		i++
	}
	if i+1 >= len(ray) || c.board[ray[i]].side() != -c.side || c.taken&(1<<ray[i]) != 0 {
		return 0, nil
	}
	j := i + 1
	for j < len(ray) && c.board[ray[j]] == Empty {
		j++
	}
	if !king {
		j = min(j, i+2)
	}
	return ray[i], ray[i+1 : j]
}

// apply makes m, a legal move of p, and gives the position after it.
func (p Position) apply(m *move) Position {
	from, to := int(m.path[0]), int(m.path[m.plen-1])
	piece := p.Squares[from]
	p.Squares[from] = Empty
	for _, sq := range m.caps[:m.ncap] {
		p.Squares[sq] = Empty
	}
	p.Squares[to] = piece
	if !piece.king() && crowned(p.Turn, m) {
		p.Squares[to] = 2 * piece
	}
	p.Quiet++
	if m.ncap > 0 || !piece.king() {
		p.Quiet = 0
	}
	p.Turn = -p.Turn
	return p
}

// crowned reports whether m lands on side's crowning row on the way or at
// the end: a man crowned in the middle of a capture stays a king even
// where it leaves that row to go on taking.
func crowned(side Side, m *move) bool {
	for _, sq := range m.path[1:m.plen] {
		if crowns(side, int(sq)) {
			return true
		}
	}
	return false
}

// Play makes the legal move whose Path is path, and gives the position
// after it and the move; ErrIllegal, and p unchanged, when there is none.
func (p Position) Play(path []int) (Position, Move, error) {
	for _, m := range p.moves(nil) {
		if m.follows(path) {
			return p.apply(&m), m.public(), nil
		}
	}
	return p, Move{}, ErrIllegal
}

// Result is how the game stands: a side to move with no legal move, as
// blocked as it is without pieces, has lost; DrawPlies quiet plies are a
// draw.
func (p Position) Result() Result {
	if len(p.moves(nil)) == 0 {
		if p.Turn == White {
			return BlackWins
		}
		return WhiteWins
	}
	if p.Quiet >= DrawPlies {
		return Draw
	}
	return Ongoing
}

const symbols = ".wWbB"

func symbol(piece Piece) byte {
	switch piece {
	case WhiteMan:
		return 'w'
	case WhiteKing:
		return 'W'
	case BlackMan:
		return 'b'
	case BlackKing:
		return 'B'
	}
	return '.'
}

var pieces = [...]Piece{Empty, WhiteMan, WhiteKing, BlackMan, BlackKing}

// Board is the 32 squares as Encode writes them: '.' empty, 'w' and 'W' a
// white man and king, 'b' and 'B' black ones.
func (p Position) Board() string {
	var b [32]byte
	for i, piece := range p.Squares {
		b[i] = symbol(piece)
	}
	return string(b[:])
}

// Encode writes the position as "board:turn:quiet", the board as Board
// writes it and the turn "w" or "b": short enough for a column or a
// button's data.
func (p Position) Encode() string {
	turn := "w"
	if p.Turn == Black {
		turn = "b"
	}
	return p.Board() + ":" + turn + ":" + strconv.Itoa(p.Quiet)
}

// Decode reads what Encode wrote. Anything Encode could not have written,
// a man on the row that crowns it, more than 12 pieces a side, a quiet
// count it would not spell so, is ErrInvalid: the text may come from
// outside.
func Decode(s string) (Position, error) {
	fields := strings.Split(s, ":")
	if len(fields) != 3 || len(fields[0]) != len(Position{}.Squares) {
		return Position{}, ErrInvalid
	}
	var p Position
	for i := range p.Squares {
		at := strings.IndexByte(symbols, fields[0][i])
		if at < 0 {
			return Position{}, ErrInvalid
		}
		p.Squares[i] = pieces[at]
	}
	switch fields[1] {
	case "w":
		p.Turn = White
	case "b":
		p.Turn = Black
	default:
		return Position{}, ErrInvalid
	}
	quiet, err := strconv.Atoi(fields[2])
	if err != nil || quiet < 0 || strconv.Itoa(quiet) != fields[2] {
		return Position{}, ErrInvalid
	}
	p.Quiet = quiet
	if !p.valid() {
		return Position{}, ErrInvalid
	}
	return p, nil
}

func (p Position) valid() bool {
	whites, blacks := 0, 0
	for sq, piece := range p.Squares {
		if piece == WhiteMan && crowns(White, sq) || piece == BlackMan && crowns(Black, sq) {
			return false
		}
		switch piece.side() {
		case White:
			whites++
		case Black:
			blacks++
		}
	}
	return whites <= 12 && blacks <= 12
}
