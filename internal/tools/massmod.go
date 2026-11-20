package tools

import (
	"context"
	"fmt"
)

const (
	// amnestyMost bounds one amnesty, for which Telegram is asked one
	// member at a time.
	amnestyMost  = 100
	noBanBook    = "the bans are not kept here, so nothing was done"
	nobodyBanned = "Banlar ro'yxati bo'sh: bandan chiqariladigan hech kim yo'q."
)

// amnesty unbans everyone the bot banned in the group since the last
// amnesty, recorded once, so that the same message never does it twice.
func (m *Moderation) amnesty(ctx context.Context, o order) (any, error) {
	if m.opts.Bans == nil {
		return nil, &DeniedError{Message: noBanBook}
	}
	banned, err := m.opts.Bans.BannedMembers(ctx, o.caller.ChatID, amnestyMost)
	if err != nil {
		return nil, err
	}
	if len(banned) == 0 {
		return map[string]any{"done": true, "said": nobodyBanned, "note": doneNote}, nil
	}
	first, err := m.opts.Store.RecordModeration(ctx, o.audit())
	if err != nil {
		return nil, err
	}
	if !first {
		return map[string]any{"done": true, "note": alreadyDoneNote}, nil
	}
	freed := 0
	for _, b := range banned {
		if err := m.opts.Telegram.UnbanChatMember(ctx, o.caller.ChatID, b.UserID); err == nil {
			freed++
		}
	}
	return map[string]any{"done": true, "said": fmt.Sprintf("🕊 Amnistiya: %d kishi bandan chiqarildi va qayta qo'shila oladi.", freed), "note": doneNote}, nil
}
