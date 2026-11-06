package dooz

// Move is the cell the bot plays at its level, for the mark whose turn it
// is; pick(n) chooses in [0, n), rand.IntN in play and fixed in tests. Easy
// takes a win it sees and half the time blocks one, else plays anywhere;
// Medium takes and blocks wins, and a third of its other moves are
// anywhere; Hard does not lose. The game must not be over.
func (g Game) Move(pick func(n int) int) int {
	mark := g.Turn()
	free := freeCells(g.Board)
	if cell, ok := completing(g.Board, mark); ok && g.Level != Hard {
		return cell
	}
	switch g.Level {
	case Easy:
		if cell, ok := completing(g.Board, other(mark)); ok && pick(2) == 0 {
			return cell
		}
		return free[pick(len(free))]
	case Medium:
		if cell, ok := completing(g.Board, other(mark)); ok {
			return cell
		}
		if pick(3) == 0 {
			return free[pick(len(free))]
		}
	}
	return bestMove(g.Board, pick)
}

func other(mark byte) byte {
	if mark == X {
		return O
	}
	return X
}

func freeCells(board [9]byte) []int {
	var free []int
	for i, c := range board {
		if c == Empty {
			free = append(free, i)
		}
	}
	return free
}

// completing is a cell that gives mark a line.
func completing(board [9]byte, mark byte) (int, bool) {
	for _, cell := range freeCells(board) {
		board[cell] = mark
		won, _ := winner(board)
		board[cell] = Empty
		if won == mark {
			return cell, true
		}
	}
	return 0, false
}

// bestMove is one of the moves that do best against any reply, the
// quickest win or the longest loss, chosen among equals by pick so that
// the bot does not play every game the same.
func bestMove(board [9]byte, pick func(n int) int) int {
	mark := turn(board)
	best, moves := -100, []int(nil)
	for _, cell := range freeCells(board) {
		board[cell] = mark
		value := -negamax(board)
		board[cell] = Empty
		switch {
		case value > best:
			best, moves = value, []int{cell}
		case value == best:
			moves = append(moves, cell)
		}
	}
	return moves[pick(len(moves))]
}

// negamax is what the board is worth to the mark that moves next: less
// than 0 when the other has won, more the sooner it was, 0 for a draw.
func negamax(board [9]byte) int {
	free := freeCells(board)
	if won, _ := winner(board); won != Empty {
		return -(1 + len(free))
	}
	if len(free) == 0 {
		return 0
	}
	mark := turn(board)
	best := -100
	for _, cell := range free {
		board[cell] = mark
		best = max(best, -negamax(board))
		board[cell] = Empty
	}
	return best
}
