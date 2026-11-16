package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
)

const (
	// amnestyMost and kickMost bound one amnesty and one kick of the
	// inactive, which Telegram is asked one member at a time.
	amnestyMost = 100
	kickMost    = 50
	// kickShown is how many names the list of the inactive shows.
	kickShown       = 15
	defaultKickDays = 30
	leastKickDays   = 7
	mostKickDays    = 365
	noBanBook       = "the bans are not kept here, so nothing was done"
	nobodyBanned    = "Banlar ro'yxati bo'sh: bandan chiqariladigan hech kim yo'q."
	nobodyInactive  = "Bunday a'zo yo'q: hamma %d kun ichida yozgan."
	confirmKickNote = "nothing was done yet: tell them the list in one line and that they confirm with the same request and \"ha\""
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

// kickInactive lists the members last seen writing more than days ago,
// past admins, owners and moderators with a rank; with confirmed it removes
// them, who may join again.
func (m *Moderation) kickInactive(ctx context.Context, o order, days int, confirmed bool) (any, error) {
	if m.opts.Bans == nil {
		return nil, &DeniedError{Message: noBanBook}
	}
	if days <= 0 {
		days = defaultKickDays
	}
	days = min(max(days, leastKickDays), mostKickDays)
	since := m.opts.Now().Add(-time.Duration(days) * 24 * time.Hour)
	seen, err := m.opts.Bans.InactiveMembers(ctx, o.caller.ChatID, since, kickMost)
	if err != nil {
		return nil, err
	}
	var quiet []storage.GroupMember
	for _, member := range seen {
		if m.spared(ctx, o.caller.ChatID, member.UserID) {
			continue
		}
		quiet = append(quiet, member)
	}
	if len(quiet) == 0 {
		return map[string]any{"done": true, "said": fmt.Sprintf(nobodyInactive, days), "note": doneNote}, nil
	}
	if !confirmed {
		names := make([]string, 0, kickShown)
		for _, q := range quiet[:min(len(quiet), kickShown)] {
			names = append(names, inactiveName(q))
		}
		more := ""
		if len(quiet) > kickShown {
			more = fmt.Sprintf(" va yana %d kishi", len(quiet)-kickShown)
		}
		said := fmt.Sprintf("🧹 %d kundan beri yozmaganlar, %d kishi: %s%s. Chiqarish uchun shu buyruqni \"ha\" bilan qayta yozing.", days, len(quiet), strings.Join(names, ", "), more)
		return map[string]any{"done": false, "said": said, "note": confirmKickNote}, nil
	}
	first, err := m.opts.Store.RecordModeration(ctx, o.audit())
	if err != nil {
		return nil, err
	}
	if !first {
		return map[string]any{"done": true, "note": alreadyDoneNote}, nil
	}
	removed := 0
	for _, q := range quiet {
		if err := m.opts.Telegram.BanChatMember(ctx, o.caller.ChatID, q.UserID); err != nil {
			continue
		}
		_ = m.opts.Telegram.UnbanChatMember(ctx, o.caller.ChatID, q.UserID)
		removed++
	}
	return map[string]any{"done": true, "said": fmt.Sprintf("🧹 Bajarildi: %d kundan beri yozmagan %d kishi chiqarildi, qayta qo'shila oladi.", days, removed), "note": doneNote}, nil
}

// spared is a member a kick of the inactive leaves: an owner, an admin, one
// who left, or a moderator with a rank.
func (m *Moderation) spared(ctx context.Context, chatID, userID int64) bool {
	if m.opts.Owners[userID] {
		return true
	}
	if m.opts.Ranks != nil {
		if rank, err := m.opts.Ranks.RankOf(ctx, chatID, userID); err != nil || rank > 0 {
			return true
		}
	}
	member, err := m.opts.Members.GetChatMember(ctx, chatID, userID)
	return err != nil || (member.Status != "member" && member.Status != "restricted")
}

// inactiveName is how the list of the inactive names a member.
func inactiveName(g storage.GroupMember) string {
	if g.FirstName != "" {
		return g.FirstName
	}
	if g.Username != "" {
		return "@" + g.Username
	}
	return fmt.Sprint(g.UserID)
}
