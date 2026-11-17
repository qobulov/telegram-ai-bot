package tools

import (
	"context"
	"fmt"

	"telegram-ai-bot/internal/ranks"
)

// checkAsker lets the caller ask for action when their rank opens it, and
// says that rank; otherwise, with rank 0, it checks the Telegram right as
// before. A ranked member whose rank is too low is told the rank it needs.
func (m *Moderation) checkAsker(ctx context.Context, caller Caller, action, right string) (int, error) {
	rank, need, err := m.rankFor(ctx, caller, action)
	if err != nil {
		return 0, err
	}
	if rank > 0 && rank >= need {
		return rank, nil
	}
	if err := m.checkRequester(ctx, caller, right); err != nil {
		if rank > 0 {
			return 0, &DeniedError{Message: fmt.Sprintf("their rank is %d, and this needs rank %d here", rank, need)}
		}
		return 0, err
	}
	return 0, nil
}

// rankFor is the caller's rank in their group and the rank action needs
// there; 0 and 0 without ranks or for an action ranks do not open.
func (m *Moderation) rankFor(ctx context.Context, caller Caller, action string) (rank, need int, err error) {
	code := ranks.CodeOf(action)
	if m.opts.Ranks == nil || code == "" {
		return 0, 0, nil
	}
	if rank, err = m.opts.Ranks.RankOf(ctx, caller.ChatID, caller.UserID); err != nil || rank == 0 {
		return 0, 0, err
	}
	overrides, err := m.opts.Ranks.CommandRanks(ctx, caller.ChatID)
	if err != nil {
		return 0, 0, err
	}
	return rank, ranks.Need(code, overrides), nil
}

// checkRankedTarget keeps a moderator acting by rank off a member of the
// same rank or higher.
func (m *Moderation) checkRankedTarget(ctx context.Context, o order, rank int) error {
	if rank == 0 || o.target.UserID == 0 || o.target.UserID == o.caller.UserID {
		// One's own title or tag is no act on a peer.
		return nil
	}
	theirs, err := m.opts.Ranks.RankOf(ctx, o.caller.ChatID, o.target.UserID)
	if err != nil {
		return err
	}
	if theirs >= rank {
		return &DeniedError{Message: fmt.Sprintf("that member's rank is %d, not lower than the asker's %d, so nothing was done", theirs, rank)}
	}
	return nil
}
