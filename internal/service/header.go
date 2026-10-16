package service

import (
	"context"
	"slices"
	"strings"
	"time"

	"telegram-ai-bot/internal/telegram"
)

// BotRightsSource tells which admin rights the bot holds in a group, by
// their Bot API names; none when it is not an admin there.
type BotRightsSource interface {
	BotRights(ctx context.Context, chatID int64) ([]string, error)
}

const (
	// headerDateLayout is how the header gives the time, in words a model
	// cannot misread as another date format.
	headerDateLayout = "Monday 2 January 2006, 15:04"
	// groupAbilities tells the model, every group turn, what it can and
	// cannot do. Without it the model claimed things it has no means to do
	// or know: messages "sent to all groups", groups "joined", photos "seen".
	groupAbilities = "You can reply here, react with one emoji, and use your tools. You cannot see read receipts, who is online, the member list, other chats or private chats, open links, join groups, or remember anything older than the messages below unless a rule says so."
	rightsUnknown  = "Your admin rights here are unknown."
	notAnAdmin     = "You are not an admin here."
	// rightsLookupTimeout bounds learning the bot's rights, which may mean
	// asking Telegram. Past it the header says they are unknown rather than
	// hold up the answer.
	rightsLookupTimeout = 5 * time.Second
)

// chatHeader is the first part of a turn's context: where the model is, the
// time there, and in a group what it may do. It is given for this turn only
// and never stored, so the time and the rights are always the current ones.
func (s *Service) chatHeader(ctx context.Context, in incoming) string {
	now := s.opts.Now().In(s.opts.Location)
	date := "Now: " + now.Format(headerDateLayout) + " (" + s.opts.Location.String() + ")."
	if !in.group {
		return "[" + date + "]"
	}
	return `[About this chat: the group "` + headerTitle(in.msg.Chat.Title) + `". ` + date + "\n" +
		s.rightsLine(ctx, in.scope.ChatID) + "\n" +
		groupAbilities + "]"
}

// headerTitle folds a group's title onto the header's first line, inside its
// double quotes. Admins, and members where the group allows it, can rename a
// group: a double quote in the title would close the quote and let the rest
// pass for the header's own words, such as admin rights the bot does not
// hold. So double quotes become single ones.
func headerTitle(title string) string {
	return strings.ReplaceAll(oneLine(title), `"`, "'")
}

// rightsLine says which admin rights the bot holds in the group. When they
// cannot be learned it says so rather than guess: the model must neither
// promise an action it cannot take nor refuse one it can.
func (s *Service) rightsLine(ctx context.Context, chatID int64) string {
	if s.opts.BotRights == nil {
		return rightsUnknown
	}
	ctx, cancel := context.WithTimeout(ctx, rightsLookupTimeout)
	defer cancel()
	rights, err := s.opts.BotRights.BotRights(ctx, chatID)
	if err != nil {
		s.opts.Logger.Warn("could not learn the bot's admin rights", "chat_id", chatID, "error", err)
		return rightsUnknown
	}
	words := rightsInWords(rights)
	if words == "" {
		return notAnAdmin
	}
	return "Your admin rights here: " + words + "."
}

// rightWords names each admin right the way the header lists it, in the
// order it lists them.
var rightWords = []struct{ right, words string }{
	{telegram.RightDeleteMessages, "delete messages"},
	{telegram.RightRestrictMembers, "ban and mute members"},
	{telegram.RightPinMessages, "pin messages"},
	{telegram.RightPromoteMembers, "add admins"},
	{telegram.RightChangeInfo, "change group info"},
	{telegram.RightInviteUsers, "invite users"},
	{telegram.RightManageTopics, "manage topics"},
}

// rightsInWords renders rights, given by their Bot API names, as plain words
// joined by commas, always in the same order. Names it does not know are
// left out; with none it knows, it returns "".
func rightsInWords(rights []string) string {
	var words []string
	for _, r := range rightWords {
		if slices.Contains(rights, r.right) {
			words = append(words, r.words)
		}
	}
	return strings.Join(words, ", ")
}
