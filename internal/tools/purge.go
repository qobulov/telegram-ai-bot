package tools

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"telegram-ai-bot/internal/telegram"
)

// purge deletes the messages a member wrote in a group lately, as the
// group's log has them: a spammer's flood, or a quarrel the admins clear
// out. The Bot API cannot list a chat's messages, so what the log never saw,
// such as what the guards deleted, is not purged, nor is anything with the
// log off. It needs the delete right from the asker and the bot, as delete
// does, and like a deletion it cannot be undone.
const actionPurge = "purge"

const (
	// A purge reaches back an hour unless the asker says otherwise, and two
	// days at most: Telegram deletes no message older than 48 hours.
	defaultPurgeMinutes = 60
	maxPurgeMinutes     = 48 * 60
	// maxPurged bounds the messages one purge deletes, in that many calls
	// to Telegram, so that it ends within the tool's time.
	maxPurged = 10 * telegram.MaxDeletedAtOnce
)

const (
	noMessageLog   = "I keep no log of this group's messages, so I can't find theirs; ask them to reply to each message to delete"
	nothingToPurge = "I have no message of theirs from the last %d minutes in my log of this group, so nothing was deleted"
	purgeNote      = "Done. Tell them in one short line, in their language, like the line in said, and that deleted messages cannot be brought back; do not ask anything."
	partlyNote     = "Partly done: Telegram stopped answering, so some of their messages are left. Tell them in one short line, in their language, how many were deleted, as said says, and that they can ask again."
)

// MessageLog finds a member's messages in a group's log.
type MessageLog interface {
	AuthorMessageIDs(ctx context.Context, chatID, authorID int64, since time.Time, limit int) ([]int64, error)
}

// purge finds the target's messages of the last o.minutes, records the
// purge, and has Telegram delete them, in batches. Recorded first, an update
// Telegram delivers twice purges once.
func (m *Moderation) purge(ctx context.Context, o order) (any, error) {
	if m.opts.Messages == nil {
		return nil, &DeniedError{Message: noMessageLog}
	}
	since := m.opts.Now().Add(-time.Duration(o.minutes) * time.Minute)
	ids, err := m.opts.Messages.AuthorMessageIDs(ctx, o.caller.ChatID, o.target.UserID, since, maxPurged)
	if err != nil {
		return nil, internal(err)
	}
	// The message replied to is meant too, however old: Telegram refuses it
	// only past 48 hours.
	if id := o.target.MessageID; id != 0 && !slices.Contains(ids, id) && len(ids) < maxPurged {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, &DeniedError{Message: fmt.Sprintf(nothingToPurge, o.minutes)}
	}
	record := o.audit()
	record.TargetText = strconv.Itoa(len(ids)) + " messages"
	first, err := m.opts.Store.RecordModeration(ctx, record)
	if err != nil {
		return nil, internal(err)
	}
	if !first {
		return map[string]any{"done": true, "note": alreadyDoneNote}, nil
	}
	deleted, err := m.deleteAll(ctx, o.caller.ChatID, ids)
	if deleted == 0 {
		return nil, notCarriedOut(m.failed(ctx, o, err))
	}
	said := purgedText(o.target.Name, deleted, o.minutes).String()
	if err != nil {
		return map[string]any{"done": true, "said": said, "note": partlyNote}, nil
	}
	return map[string]any{"done": true, "said": said, "note": purgeNote}, nil
}

// deleteAll deletes ids in batches as large as Telegram takes, and returns
// how many it asked Telegram to delete before an error, if one stopped it.
func (m *Moderation) deleteAll(ctx context.Context, chatID int64, ids []int64) (int, error) {
	for start := 0; start < len(ids); start += telegram.MaxDeletedAtOnce {
		end := min(start+telegram.MaxDeletedAtOnce, len(ids))
		if err := m.opts.Telegram.DeleteMessages(ctx, chatID, ids[start:end]); err != nil {
			return start, err
		}
	}
	return len(ids), nil
}

// purgedText says, for the chat, what a purge deleted.
func purgedText(name string, deleted, minutes int) chatText {
	count, span := strconv.Itoa(deleted), strconv.Itoa(minutes)
	return chatText{
		"Bajarildi: " + name + " ning so'nggi " + span + " daqiqadagi " + count + " ta xabari o'chirildi; ularni qaytarib bo'lmaydi.",
		"Готово: удалено сообщений от " + name + " за последние " + span + " мин.: " + count + "; их не вернуть.",
	}
}
