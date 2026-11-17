package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"telegram-ai-bot/internal/ranks"
)

// RankWriter gives or takes a moderator's rank.
type RankWriter interface {
	SetRank(ctx context.Context, chatID, userID int64, rank int, by int64) error
}

// rankWords ask about a rank: without one in the asker's own words, the
// rank tool does nothing.
var rankWords = []string{"daraj", "дараж", "admin", "админ", "moder", "модер", "rank", "ранг", "reng"}

type setRankArgs struct {
	Rank   *int   `json:"rank"`
	Member string `json:"member"`
}

// RankTool gives or takes a moderator's rank, 1 to 5, as "+3 admin Mari"
// does in code, for an order the code did not read: "- админ Мари",
// "darajasini olib tashla". The same rules hold: giving ranks takes the rank
// that opens it (4 unless set) or the group's creator or an owner, and only
// below the asker's own.
func (m *Moderation) RankTool(writer RankWriter) Tool {
	return Tool{
		Name: "set_rank",
		Description: "Give or take away a moderator rank (1–5; 0 takes it away) of the member their message replies to, or the one they tagged or named, when someone asks for it in any wording (\"- админ Мари\", \"darajasini ol\", \"3-daraja ber\", \"сними ранг\"). " +
			"A rank lets a member have you moderate without being a Telegram admin. It checks who may ask and refuses with the reason; say it is done only when it reports done. Telegram adminship itself is moderate's promote and demote, not this.",
		Schema: json.RawMessage(`{"type": "object", "properties": {
			"rank": {"type": "integer", "minimum": 0, "maximum": 5, "description": "The new rank, 1 to 5; 0 takes the rank away."},
			"member": {"type": "string", "description": "For a member they named without a reply or a tag: their first name or @username."}
		}, "required": ["rank"], "additionalProperties": false}`),
		GroupOnly: true,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return m.setRank(ctx, writer, raw)
		},
	}
}

func (m *Moderation) setRank(ctx context.Context, writer RankWriter, raw json.RawMessage) (any, error) {
	var args setRankArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	if args.Rank == nil || *args.Rank < 0 || *args.Rank > ranks.Top {
		return nil, &ArgumentError{Message: "rank must be 0 to 5"}
	}
	caller, err := groupRequester(ctx)
	if err != nil {
		return nil, err
	}
	if !says(caller.Words, rankWords) {
		return nil, &DeniedError{Message: notAsked}
	}
	if m.opts.Ranks == nil || writer == nil {
		return nil, &DeniedError{Message: "ranks are not kept here"}
	}
	target, err := m.targetOf(ctx, caller, actionWarn, args.Member)
	if err != nil {
		return nil, err
	}
	if target.UserID == 0 || target.UserID == caller.UserID || target.IsBot || target.UserID == m.opts.BotID {
		return nil, &DeniedError{Message: "a rank is given to another member, not to oneself or a bot"}
	}
	if err := m.mayGiveRank(ctx, caller, target.UserID, *args.Rank); err != nil {
		return nil, err
	}
	if err := writer.SetRank(ctx, caller.ChatID, target.UserID, *args.Rank, caller.UserID); err != nil {
		return nil, internal(err)
	}
	said := fmt.Sprintf("Bajarildi: %s endi %d-darajali moderator.", target.Name, *args.Rank)
	if *args.Rank == 0 {
		said = "Bajarildi: " + target.Name + " endi moderator emas."
	}
	return map[string]any{"done": true, "said": said, "note": doneNote}, nil
}

// mayGiveRank lets the asker give or take rank: the group's creator and the
// owners any, a moderator whose rank opens it only below their own, to a
// member below them.
func (m *Moderation) mayGiveRank(ctx context.Context, caller Caller, target int64, rank int) error {
	mine := 0
	if caller.Owner && m.opts.OwnersByID {
		mine = ranks.Above
	} else if member, err := m.opts.Members.GetChatMember(ctx, caller.ChatID, caller.UserID); err == nil && member.Status == "creator" {
		mine = ranks.Above
	} else if mine, err = m.opts.Ranks.RankOf(ctx, caller.ChatID, caller.UserID); err != nil {
		return internal(err)
	}
	overrides, err := m.opts.Ranks.CommandRanks(ctx, caller.ChatID)
	if err != nil {
		return internal(err)
	}
	if need := ranks.Need("rank", overrides); mine < need {
		return &DeniedError{Message: fmt.Sprintf("their rank is %d, and giving ranks needs rank %d here", mine, need)}
	}
	theirs, err := m.opts.Ranks.RankOf(ctx, caller.ChatID, target)
	if err != nil {
		return internal(err)
	}
	if mine < ranks.Above && (rank >= mine || theirs >= mine) {
		return &DeniedError{Message: fmt.Sprintf("that member's rank is %d, not lower than the asker's %d, so nothing was done", max(theirs, rank), mine)}
	}
	return nil
}
