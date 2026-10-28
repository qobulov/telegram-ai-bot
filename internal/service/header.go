package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
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
	groupAbilities = "You can reply here, react with one emoji, and use your tools. You cannot see read receipts, who is online or the full member list (group_admins lists the admins, group_members the members you have seen, catch_up the day's messages for a recap), read other chats or private chats except through your tools, open links, join groups, or remember anything older than the messages below unless a rule says so."
	rightsUnknown  = "Your admin rights here are unknown."
	notAnAdmin     = "You are not an admin here."
	// noToolRights is said of a bot that is an admin, but holds none of the
	// rights its tools use.
	noToolRights = "You are an admin here, without the rights to delete messages, ban and mute members or pin messages."
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
	return `[About this chat: the group "` + inQuotes(in.msg.Chat.Title) + `". ` + date + "\n" +
		s.rightsLine(ctx, in.scope.ChatID) + "\n" +
		groupAbilities + "]"
}

// inQuotes folds text that the header puts inside double quotes, a group's
// title or an owner's form of address, onto one line. Admins, and members
// where the group allows it, can rename a group: a double quote in the title
// would close the quote and let the rest pass for the header's own words,
// such as admin rights the bot does not hold. So double quotes become single
// ones.
func inQuotes(text string) string {
	return strings.ReplaceAll(oneLine(text), `"`, "'")
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
	switch {
	case len(rights) == 0:
		return notAnAdmin
	case words == "":
		return noToolRights
	}
	return "Your admin rights here: " + words + "."
}

const (
	rulesOpening = "[Standing rules from your owners for this group:"
	// ruleDayLayout dates an instruction; ruleTimeLayout, an ignore, which
	// is often for hours rather than days.
	ruleDayLayout  = "2 Jan"
	ruleTimeLayout = "2 Jan 15:04"
)

// rulesHeader follows chatHeader: the owners' standing rules for a group,
// its instructions and then the members not to answer, and, when an owner is
// writing, the form of address they asked for. It is "" when there is none
// of these. Rules are given with every turn, so the model cannot forget
// them as it forgets what scrolled out of the log.
func (s *Service) rulesHeader(rules []storage.GroupRule, in incoming) string {
	var parts []string
	if in.group {
		if lines := s.ruleLines(rules); len(lines) > 0 {
			parts = append(parts, rulesOpening+"\n"+strings.Join(lines, "\n")+"]")
		}
	}
	if address := s.addressFor(rules, in); address != "" {
		parts = append(parts, `[Address the owner who is writing as: "`+inQuotes(address)+`".]`)
	}
	return strings.Join(parts, "\n")
}

// ProfileBook reads what each person asked the bot to remember about
// talking to them.
type ProfileBook interface {
	Profile(ctx context.Context, userID int64) (storage.UserProfile, error)
}

// profileLine tells the model how to talk to the person writing: by the
// name they or an owner asked for in every chat, unless an owner's own form
// of address wins, and with "sen" when they swore at the bot; "siz", for
// everyone else, is the persona's. It is "" when there is nothing to tell.
func (s *Service) profileLine(ctx context.Context, in incoming, rules []storage.GroupRule) string {
	if in.fromBot {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, logTimeout)
	defer cancel()
	var lines []string
	if name := s.callName(ctx, in); name != "" && s.addressFor(rules, in) == "" {
		lines = append(lines, `[In every chat, the person writing is to be called "`+inQuotes(name)+`".]`)
	}
	if s.sworeAtBot(ctx, in) {
		lines = append(lines, senLine)
	}
	return strings.Join(lines, "\n")
}

// senLine tells the model to drop "siz" for someone who swore at it.
const senLine = `[The person writing swore at you today: speak to them with "sen" ("ты" in Russian), not "siz".]`

// callName is the name the person writing asked to be called, "" when they
// asked for none or it cannot be read.
func (s *Service) callName(ctx context.Context, in incoming) string {
	if s.opts.Profiles == nil {
		return ""
	}
	p, err := s.opts.Profiles.Profile(ctx, in.userID)
	if err != nil {
		s.opts.Logger.Warn("could not read a profile", "user_id", in.userID, "error", err)
		return ""
	}
	return p.CallName
}

// sworeAtBot reports a person the swear guard caught swearing at the bot in
// this chat within swearWindow.
func (s *Service) sworeAtBot(ctx context.Context, in incoming) bool {
	if s.opts.Strikes == nil || !in.group {
		return false
	}
	n, err := s.opts.Strikes.SwearStrikes(ctx, in.msg.Chat.ID, in.userID, storage.ActionSwearAtBot, s.opts.Now().Add(-swearWindow))
	if err != nil {
		s.opts.Logger.Warn("could not count swearing at the bot", "user_id", in.userID, "error", err)
		return false
	}
	return n > 0
}

// guardLines tell the model what a guard on in the group does without it.
var guardLines = map[string]string{
	storage.RuleSpamGuard:  "Known spammers (CAS, lols.bot) are banned here in code, not by you.",
	storage.RuleLinkGuard:  "Links to malware or phishing (Google Safe Browsing, URLhaus) are deleted here in code, not by you.",
	storage.RuleImageGuard: "Nude or gory pictures are deleted here in code, from anyone, not by you, even as a sticker, GIF or file; other stickers and GIFs are allowed.",
}

// ruleLines lists the instructions, then the ignored members. Each is folded
// onto one line without square brackets, so that it cannot close the rules'
// bracket and pass for something else.
func (s *Service) ruleLines(rules []storage.GroupRule) []string {
	var instructions, ignores []string
	for _, r := range rules {
		switch r.Kind {
		case storage.RuleInstruction:
			line := fmt.Sprintf("- (#%d) %s", r.ID, oneLine(r.Text))
			if r.UserID != 0 && r.UserName != "" {
				line = fmt.Sprintf("- (#%d) About %s: %s", r.ID, oneLine(r.UserName), oneLine(r.Text))
			}
			if !r.ExpiresAt.IsZero() {
				line += " (until " + r.ExpiresAt.In(s.opts.Location).Format(ruleDayLayout) + ")"
			}
			instructions = append(instructions, line)
		case storage.RuleSwearGuard:
			calls := "the group's creator"
			if r.UserName != "" {
				calls = oneLine(r.UserName)
			}
			instructions = append(instructions, "- Swearing is watched here in code, not by you: a member's first swear gets a warning, the next within a day a 1-hour mute that tags "+calls+". Never swear back at anyone in this group, and never mute anyone for swearing yourself.")
		case storage.RuleSpamGuard, storage.RuleLinkGuard, storage.RuleImageGuard:
			instructions = append(instructions, "- "+guardLines[r.Kind])
		case storage.RuleIgnore:
			line := "- Do not answer " + oneLine(r.UserName)
			if !r.ExpiresAt.IsZero() {
				line += "; ignored until " + r.ExpiresAt.In(s.opts.Location).Format(ruleTimeLayout)
			}
			ignores = append(ignores, line+".")
		}
	}
	return append(instructions, ignores...)
}

// addressFor is how the owner who is writing asked to be addressed, or "".
func (s *Service) addressFor(rules []storage.GroupRule, in incoming) string {
	if !s.isOwner(in.msg.From) {
		return ""
	}
	for _, r := range rules {
		if r.Kind == storage.RuleAddress && r.UserID == in.userID {
			return r.Text
		}
	}
	return ""
}

// rightWords names each admin right the way the header lists it, in the
// order it lists them. Only the rights a tool uses are listed: told she may
// add admins or change the group's info, the model offered to, and no tool
// can.
var rightWords = []struct{ right, words string }{
	{telegram.RightDeleteMessages, "delete messages"},
	{telegram.RightRestrictMembers, "ban and mute members"},
	{telegram.RightPinMessages, "pin messages"},
}

// rightsInWords renders rights, given by their Bot API names, as plain words
// joined by commas, always in the same order. Rights no tool uses are left
// out; with none left, it returns "".
func rightsInWords(rights []string) string {
	var words []string
	for _, r := range rightWords {
		if slices.Contains(rights, r.right) {
			words = append(words, r.words)
		}
	}
	return strings.Join(words, ", ")
}
