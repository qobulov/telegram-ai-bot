package shashka

// Level is how well the bot plays.
type Level int

const (
	Easy   Level = 1
	Medium Level = 2
	Hard   Level = 3
)

// depths are how many plies each level looks ahead. A capture that must be
// made is always searched on past the depth, since stopping before it
// misjudges the position by a piece.
var depths = map[Level]int{Easy: 2, Medium: 4, Hard: 7}

// nodeBudget caps a search, so that a reply stays quick when kings fly
// everywhere: there seven plies can be 650,000 positions, while the budget
// is some 40 ms on an Apple M3 (see BenchmarkBestMoveHard).
const nodeBudget = 150_000

// maxPly bounds a line of the search, forced captures included.
const maxPly = 64

// win is what a won position scores, less a point for each ply it takes,
// so the bot wins as soon as it can and loses as late as it can.
const (
	win      = 1_000_000
	infinity = 2 * win
	decided  = win - maxPly
)

// BestMove is the move the bot plays at level for the side to move; pick(n)
// chooses in [0, n), rand.IntN in play and fixed in tests. Easy looks two
// plies ahead and a quarter of its moves are anywhere; Medium looks four;
// Hard looks seven, or as deep as the node budget lets it. Among moves
// that score the same the bot picks, so it does not play every game alike.
// p must be Ongoing.
func BestMove(p Position, level Level, pick func(n int) int) Move {
	level = min(max(level, Easy), Hard)
	moves := p.moves(nil)
	if len(moves) == 1 {
		return moves[0].public()
	}
	if level == Easy && pick(4) == 0 {
		return moves[pick(len(moves))].public()
	}
	var s searcher
	best := s.deepen(p, moves, depths[level])
	return best[pick(len(best))].public()
}

// searcher is one alpha-beta search; bufs keeps each ply's moves so that
// searching does not allocate.
type searcher struct {
	nodes   int
	limited bool
	aborted bool
	bufs    [maxPly + 1][]move
}

// deepen searches one ply deeper at a time and gives the best moves of the
// deepest search that finished within the budget; the first always does.
// Each search tries the last one's best moves first, which prunes more.
func (s *searcher) deepen(p Position, moves []move, depth int) []move {
	var best []move
	for d := 1; d <= depth; d++ {
		s.limited = d > 1
		top, score, ok := s.root(p, moves, d)
		if !ok {
			break
		}
		best = top
		moves = append(append([]move(nil), top...), without(moves, top)...)
		if score >= decided || score <= -decided {
			break
		}
	}
	return best
}

func without(moves, drop []move) []move {
	var rest []move
	for _, m := range moves {
		kept := true
		for _, d := range drop {
			if m == d {
				kept = false
				break
			}
		}
		if kept {
			rest = append(rest, m)
		}
	}
	return rest
}

// root scores every move to depth and gives those that score best. Each is
// searched with alpha one below the best so far, so a move that ties it
// gets its exact score rather than a bound and is kept among the best.
func (s *searcher) root(p Position, moves []move, depth int) ([]move, int, bool) {
	best, top := -infinity, []move(nil)
	for i := range moves {
		score := -s.negamax(p.apply(&moves[i]), depth-1, -infinity, 1-best, 1)
		if s.aborted {
			return nil, 0, false
		}
		switch {
		case score > best:
			best, top = score, []move{moves[i]}
		case score == best:
			top = append(top, moves[i])
		}
	}
	return top, best, true
}

// negamax is what p is worth to the side to move, searched depth plies on
// and through any capture that must be made, within (alpha, beta).
func (s *searcher) negamax(p Position, depth, alpha, beta, ply int) int {
	s.nodes++
	if s.limited && s.nodes > nodeBudget {
		s.aborted = true
		return 0
	}
	moves := p.moves(s.bufs[ply])
	s.bufs[ply] = moves
	switch {
	case len(moves) == 0:
		return ply - win
	case p.Quiet >= DrawPlies:
		return 0
	case depth <= 0 && moves[0].ncap == 0, ply >= maxPly:
		return evaluate(p)
	}
	for i := range moves {
		score := -s.negamax(p.apply(&moves[i]), depth-1, -beta, -alpha, ply+1)
		if s.aborted {
			return 0
		}
		if score > alpha {
			alpha = score
			if alpha >= beta {
				break
			}
		}
	}
	return alpha
}

// centre are the four middle squares, which a piece holds best from.
var centre = [32]int{13: 1, 14: 1, 17: 1, 18: 1}

// evaluate is p's worth to the side to move without looking ahead: a king
// is worth three men, a man more the nearer it is to crowning, and a piece
// in the centre a little more.
func evaluate(p Position) int {
	score := 0
	for sq, piece := range p.Squares {
		if piece == Empty {
			continue
		}
		value := 300 + 6*centre[sq]
		if !piece.king() {
			value = 100 + 4*advance(piece.side(), sq) + 6*centre[sq]
		}
		if piece.side() == p.Turn {
			score += value
		} else {
			score -= value
		}
	}
	return score
}

// advance is how many rows a man of side on sq has come from its own edge.
func advance(side Side, sq int) int {
	if side == White {
		return 7 - sq/4
	}
	return sq / 4
}
