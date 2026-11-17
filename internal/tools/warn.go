package tools

import (
	"context"
	"strconv"
	"time"

	"telegram-ai-bot/internal/telegram"
)

const (
	// WarnLimit warnings mute a member for WarnMuteMinutes, and the count
	// starts again.
	WarnLimit       = 3
	WarnMuteMinutes = 24 * 60
	noWarns         = "warnings are not kept here, so nothing was done"
)

// warn gives o's member a warning, or takes one off; the WarnLimit'th mutes
// them for WarnMuteMinutes. The update is recorded first, so that the same
// message never warns twice.
func (m *Moderation) warn(ctx context.Context, o order) (any, error) {
	if m.opts.Warns == nil {
		return nil, &DeniedError{Message: noWarns}
	}
	first, err := m.opts.Store.RecordModeration(ctx, o.audit())
	if err != nil {
		return nil, err
	}
	if !first {
		return map[string]any{"done": true, "note": alreadyDoneNote}, nil
	}
	chatID, userID, name := o.caller.ChatID, o.target.UserID, o.target.Name
	if o.action == actionUnwarn {
		left, err := m.opts.Warns.RemoveWarn(ctx, chatID, userID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"done": true, "said": "Bajarildi: " + name + " ogohlantirishi olib tashlandi, endi " + count(left) + ".", "note": doneNote}, nil
	}
	n, err := m.opts.Warns.AddWarn(ctx, chatID, userID, o.reason)
	if err != nil {
		return nil, err
	}
	if n < WarnLimit {
		why := ""
		if o.reason != "" {
			why = " Sabab: " + o.reason + "."
		}
		return map[string]any{"done": true, "said": "⚠️ " + name + " ogohlantirildi: " + count(n) + "." + why + " " + strconv.Itoa(WarnLimit) + "-ogohlantirishda 1 kunga yoza olmaydi; ogohlantirishlar 30 kundan keyin o'chadi.", "note": doneNote}, nil
	}
	until := m.opts.Now().Add(WarnMuteMinutes * time.Minute)
	if err := m.opts.Telegram.RestrictChatMember(ctx, telegram.Restriction{ChatID: chatID, UserID: userID, Permissions: telegram.NoPermissions(), Until: until}); err != nil {
		return nil, notCarriedOut(telegramFailure(err))
	}
	if err := m.opts.Warns.ResetWarns(ctx, chatID, userID); err != nil {
		return nil, err
	}
	return map[string]any{"done": true, "said": "⛔ " + name + " " + count(n) + " ogohlantirish oldi: " + m.untilText(until) + " gacha yoza olmaydi.", "note": doneNote}, nil
}

// count is n warnings of WarnLimit: "2/3".
func count(n int) string { return strconv.Itoa(n) + "/" + strconv.Itoa(WarnLimit) }
