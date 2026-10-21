package service

import (
	"context"
	"strings"
	"time"
	"unicode"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// The swear guard, which an owner turns on for a group with group_rules,
// handles swearing there in code, without the model: the first swear of a
// member gets a warning, the next within swearWindow an hour's mute, which
// tags the group's creator or whom the owner named.
const (
	swearWindow    = 24 * time.Hour
	swearMute      = time.Hour
	swearGuardTime = 10 * time.Second
)

// SwearStrikes records and counts what the swear guard did to each member.
type SwearStrikes interface {
	SwearStrikes(ctx context.Context, chatID, userID int64, since time.Time) (int, error)
	RecordModeration(ctx context.Context, a storage.ModerationAction) (bool, error)
}

// Restrictor mutes a member of a group.
type Restrictor interface {
	RestrictChatMember(ctx context.Context, r telegram.Restriction) error
}

// AdminLister lists a group's creator and admins.
type AdminLister interface {
	GetChatAdministrators(ctx context.Context, chatID int64) ([]telegram.ChatMember, error)
}

// swearWords are whole words that are swearing whatever the context.
// Words that also mean something harmless are left out, such as "jala"
// (a downpour), "am" or "kot" (a cat): a wrong mute is worse than a missed
// swear.
var swearWords = map[string]bool{
	"dnx": true, "днх": true, "pshnx": true, "пшнх": true, "nx": true, "нх": true, "нах": true,
	"pzds": true, "пздс": true, "naxuy": true, "nahuy": true, "naxui": true, "наху": true, "нахуй": true, "нахуя": true,
	"бля": true, "blya": true, "blyat": true, "blyad": true, "сука": true, "suka": true, "сучка": true,
	"pizdes": true, "pizdec": true, "pidr": true, "пидр": true, "пидор": true,
	"yeban": true, "dalbayob": true, "dalbayop": true, "qotoq": true, "қотоқ": true, "котак": true,
}

// swearStems are beginnings of words that are swearing in any form:
// "нахуй", "пиздец", "ебать", "jalablar", "qo'tog'im".
var swearStems = []string{
	"хуй", "хуе", "хую", "хуя", "пизд", "ебан", "ебат", "ебал", "ебу", "бляд", "блят",
	"pizd", "xuyn", "gandon", "гандон", "jalab", "жалаб", "dalbayob", "далбаёб", "далбаеб",
	"qo'tog", "qotog", "котаг", "қўтоғ", "кўтоғ", "sikay", "sikam", "sikib", "sikdi", "sikvor",
}

// isSwearing reports whether text holds a word from swearWords or one that
// begins with a swearStem, in any case. Uzbek's apostrophes count as one:
// "qoʻtogʻ" and "qo`tog`" are "qo'tog'".
func isSwearing(text string) bool {
	folded := apostrophes.Replace(strings.ToLower(text))
	for _, word := range strings.FieldsFunc(folded, func(r rune) bool { return !unicode.IsLetter(r) && r != '\'' }) {
		word = strings.Trim(word, "'")
		if swearWords[word] {
			return true
		}
		for _, stem := range swearStems {
			if strings.HasPrefix(word, stem) {
				return true
			}
		}
	}
	return false
}

var apostrophes = strings.NewReplacer("ʻ", "'", "ʼ", "'", "’", "'", "‘", "'", "`", "'")

// guardedSwearing handles a member's swearing in a group whose owners turned
// the swear guard on, and reports whether it did: the message then gets no
// answer from the model, which would only swear back. Admins, owners, bots
// and posts on behalf of a chat are left alone, and so is a muted group.
func (s *Service) guardedSwearing(msg *telegram.Message, updateID int64) bool {
	if s.opts.Strikes == nil || s.opts.Rules == nil || s.opts.AdminList == nil || s.opts.Restrictor == nil ||
		msg.From == nil || msg.From.IsBot || msg.SenderChat != nil || msg.ForwardOrigin != nil || s.isOwner(msg.From) {
		return false
	}
	text, _ := msg.Content()
	if !isSwearing(text) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), swearGuardTime)
	defer cancel()
	guard, on := s.swearGuard(ctx, msg.Chat.ID)
	if !on || s.isMuted(msg.Chat.ID) {
		return false
	}
	admins, err := s.opts.AdminList.GetChatAdministrators(ctx, msg.Chat.ID)
	if err != nil {
		s.opts.Logger.Warn("swear guard: could not list the admins; leaving it", "chat_id", msg.Chat.ID, "error", err)
		return false
	}
	if isAmong(admins, msg.From.ID) {
		return false
	}
	// Recorded first: an update Telegram delivers twice is handled once,
	// and its second delivery is not taken for a second swear.
	if !s.recordStrike(ctx, msg, updateID) {
		return true
	}
	strikes, err := s.opts.Strikes.SwearStrikes(ctx, msg.Chat.ID, msg.From.ID, s.opts.Now().Add(-swearWindow))
	if err != nil {
		s.opts.Logger.Warn("swear guard: could not count strikes; leaving it", "chat_id", msg.Chat.ID, "error", err)
		return false
	}
	if strikes <= 1 {
		s.postGuard(msg, mentionOf(msg.From)+", guruhda so'kinish mumkin emas. Bu ogohlantirish: yana so'kinsangiz, 1 soat yozolmaysiz.")
	} else {
		s.muteSwearer(ctx, msg, swearCaller(guard, admins))
	}
	return true
}

// swearGuard is the group's swear guard rule, if an owner turned it on.
func (s *Service) swearGuard(ctx context.Context, chatID int64) (storage.GroupRule, bool) {
	rules, err := s.opts.Rules.GroupRules(ctx, chatID)
	if err != nil {
		s.opts.Logger.Warn("swear guard: could not read the rules", "chat_id", chatID, "error", err)
		return storage.GroupRule{}, false
	}
	for _, r := range rules {
		if r.Kind == storage.RuleSwearGuard && r.ChatID == chatID {
			return r, true
		}
	}
	return storage.GroupRule{}, false
}

func (s *Service) muteSwearer(ctx context.Context, msg *telegram.Message, caller string) {
	who := mentionOf(msg.From)
	err := s.opts.Restrictor.RestrictChatMember(ctx, telegram.Restriction{
		ChatID: msg.Chat.ID, UserID: msg.From.ID, Permissions: telegram.NoPermissions(), Until: s.opts.Now().Add(swearMute),
	})
	var text string
	if err != nil {
		s.opts.Logger.Warn("swear guard: mute failed", "chat_id", msg.Chat.ID, "user_id", msg.From.ID, "error", err)
		text = who + " yana so'kindi, lekin uni yozolmaydigan qila olmadim: menda a'zolarni cheklash huquqi yo'q."
		if caller != "" {
			text += " " + caller + ", qarab qo'ying."
		}
	} else {
		text = who + " so'kingani uchun 1 soat yozolmaydi."
		if caller != "" {
			text += " " + caller + ", xabaringiz bo'lsin."
		}
	}
	s.postGuard(msg, text)
}

// recordStrike notes a swear the guard caught, and reports whether it is
// new: the same update is not.
func (s *Service) recordStrike(ctx context.Context, msg *telegram.Message, updateID int64) bool {
	text, _ := msg.Content()
	fresh, err := s.opts.Strikes.RecordModeration(ctx, storage.ModerationAction{
		ChatID: msg.Chat.ID, Action: storage.ActionSwear, RequesterID: s.opts.Bot.ID, TargetUserID: msg.From.ID,
		TargetMessageID: msg.MessageID, TargetText: truncate(text, 500), Reason: "swearing", UpdateID: updateID,
	})
	if err != nil {
		s.opts.Logger.Warn("swear guard: could not record", "chat_id", msg.Chat.ID, "error", err)
		return false
	}
	return fresh
}

// postGuard replies to the swearer's message, and logs the reply as the
// bot's own, so that the model knows what was done.
func (s *Service) postGuard(msg *telegram.Message, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	sentID, err := s.opts.Sender.SendMessage(ctx, telegram.Outgoing{ChatID: msg.Chat.ID, Text: text, ReplyToMessageID: msg.MessageID})
	if err != nil {
		s.opts.Logger.Error("swear guard: send failed", "chat_id", msg.Chat.ID, "error", err)
		return
	}
	s.ambient.add(msg.Chat.ID, sentID, s.opts.Bot.ID, botAuthor, text)
}

// swearCaller is whom a mute tags: the member the owner named, or else the
// group's creator by @username; "" when neither can be tagged.
func swearCaller(guard storage.GroupRule, admins []telegram.ChatMember) string {
	if guard.UserName != "" {
		return guard.UserName
	}
	for _, a := range admins {
		if a.Status == "creator" && !a.IsAnonymous && a.User != nil && a.User.Username != "" {
			return "@" + a.User.Username
		}
	}
	return ""
}

func isAmong(admins []telegram.ChatMember, userID int64) bool {
	for _, a := range admins {
		if a.User != nil && a.User.ID == userID {
			return true
		}
	}
	return false
}

// mentionOf tags a member by @username, which notifies them; one without
// a username is named.
func mentionOf(user *telegram.User) string {
	if user.Username != "" {
		return "@" + user.Username
	}
	return plainLabel(user)
}
