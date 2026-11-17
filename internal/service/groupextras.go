package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// GroupExtras keeps a group's reputation and its settings.
type GroupExtras interface {
	GroupSetting(ctx context.Context, chatID int64, key string) (string, error)
	SetGroupSetting(ctx context.Context, chatID int64, key, value string, by int64) error
	GiveRep(ctx context.Context, chatID, from, to int64) (int, error)
	RepOf(ctx context.Context, chatID, userID int64) (points, place int, err error)
	RepTop(ctx context.Context, chatID int64, limit int) ([]storage.MemberRep, error)
	GroupsWithSetting(ctx context.Context, key, value string) ([]storage.GroupWithSetting, error)
}

// repWords thank the member replied to, and give them a point.
var repWords = map[string]bool{
	"+": true, "++": true, "+++": true, "+1": true, "+rep": true, "+реп": true,
	"rahmat": true, "raxmat": true, "rahmet": true, "raxmet": true, "rakhmat": true, "rahmatt": true, "raxmatt": true,
	"рахмат": true, "раҳмат": true, "рахмет": true, "рахматт": true,
	"спасибо": true, "спасиб": true, "спасибки": true, "спс": true, "спсб": true, "пасиба": true, "благодарю": true, "spasibo": true, "spс": true, "sps": true,
	"thanks": true, "thank": true, "thx": true, "tnx": true, "ty": true, "respect": true, "респект": true, "barakalla": true, "баракалла": true,
}

// repFillers may follow a thanks: "rahmat aka", "katta rahmat", "спс бро".
var repFillers = map[string]bool{
	"katta": true, "kotta": true, "катта": true, "aka": true, "ака": true, "uka": true, "ука": true, "opa": true, "опа": true,
	"bro": true, "бро": true, "brat": true, "брат": true, "you": true, "a lot": true, "большое": true, "огромное": true, "sizga": true, "сизга": true,
	"senga": true, "сенга": true, "jon": true, "жон": true, "do'stim": true, "dostim": true, "mari": true, "мари": true,
}

// isThanks reports a short thanks: a thanks word, with at most two
// fillers, punctuation and emoji around it.
func isThanks(text string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r == ' ' || r == ',' || r == '!' || r == '.' || r == ')' || r == '(' || unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r)
	})
	if len(words) == 0 || len(words) > 3 {
		return false
	}
	thanked := false
	for _, w := range words {
		switch {
		case repWords[w]:
			thanked = true
		case !repFillers[w]:
			return false
		}
	}
	return thanked
}

// thanked gives a point to the member a "+" or "rahmat" replies to, marking
// the thanks with a reaction; reports whether the message was one.
func (s *Service) thanked(in incoming) bool {
	replied := in.msg.ReplyToMessage
	text, _ := in.msg.Content()
	if s.opts.Extras == nil || replied == nil || replied.From == nil || replied.From.IsBot || replied.SenderChat != nil ||
		replied.From.ID == in.userID || in.fromBot || !isThanks(text) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	_, err := s.opts.Extras.GiveRep(ctx, in.msg.Chat.ID, in.userID, replied.From.ID)
	switch {
	case errors.Is(err, storage.ErrRepTooSoon):
		return true
	case err != nil:
		s.opts.Logger.Warn("could not give reputation", "chat_id", in.msg.Chat.ID, "error", err)
		return true
	}
	if s.opts.Reactor != nil && !s.isMutedLately(in.msg.Chat.ID) {
		// A muted bot still counts the thanks, without a word or a mark.
		_ = s.opts.Reactor.SetMessageReaction(ctx, in.msg.Chat.ID, in.msg.MessageID, "👍")
	}
	return true
}

// repShown is how many members /toprep lists.
const repShown = 10

// reputation answers /rep, of the asker or the member replied to.
func (s *Service) reputation(ctx context.Context, in incoming) outcome {
	who := in.msg.From
	if r := in.msg.ReplyToMessage; r != nil && r.From != nil && !r.From.IsBot {
		who = r.From
	}
	points, place, err := s.opts.Extras.RepOf(ctx, in.scope.ChatID, who.ID)
	if err != nil {
		return say(textError.to(in))
	}
	name := cardMarks.Replace(doozName(who))
	if points == 0 {
		return say("⭐ " + name + ": hali reputatsiya yo'q. Foydali xabarga reply qilib \"+\" yoki \"rahmat\" yozilsa, +1 bo'ladi.")
	}
	return say(fmt.Sprintf("⭐ %s: %d reputatsiya, guruhda %d-o'rin.", name, points, place))
}

// reputationTop answers /toprep: the group's most thanked members.
func (s *Service) reputationTop(ctx context.Context, in incoming) outcome {
	top, err := s.opts.Extras.RepTop(ctx, in.scope.ChatID, repShown)
	if err != nil {
		return say(textError.to(in))
	}
	if len(top) == 0 {
		return say("⭐ Hali hech kimda reputatsiya yo'q. Foydali xabarga reply qilib \"+\" yoki \"rahmat\" yozing.")
	}
	lines := []string{"⭐ Reputatsiya reytingi"}
	for i, m := range top {
		lines = append(lines, fmt.Sprintf("%d. %s — %d", i+1, memberLabel(m.Name, m.Username, m.UserID), m.Points))
	}
	return say(strings.Join(lines, "\n"))
}

// setCleanService answers /cleanservice: whether the group's service
// messages ("X joined", "Y pinned") are deleted. Alone it switches the
// setting; "on" and "off" set it.
func (s *Service) setCleanService(ctx context.Context, in incoming) outcome {
	if allowed, err := s.mayModerate(ctx, in, "group"); err != nil || !allowed {
		return say("⛔ Buni guruh egasi yoki 4-darajali moderator sozlaydi.")
	}
	current, err := s.opts.Extras.GroupSetting(ctx, in.scope.ChatID, storage.SettingCleanService)
	if err != nil {
		return say(textError.to(in))
	}
	on := current != "on"
	switch strings.ToLower(strings.TrimSpace(commandArgument(in.text))) {
	case "on", "yoq", "ha":
		on = true
	case "off", "o'chir", "ochir", "yo'q":
		on = false
	}
	value := ""
	if on {
		value = "on"
	}
	if err := s.opts.Extras.SetGroupSetting(ctx, in.scope.ChatID, storage.SettingCleanService, value, in.userID); err != nil {
		return say(textError.to(in))
	}
	if on {
		return say("🧹 Yoqildi: \"kirdi\", \"chiqdi\", \"qadadi\" kabi xizmat xabarlarini o'chiraman. Menga xabarlarni o'chirish huquqi kerak. O'chirish: /cleanservice")
	}
	return say("🧹 O'chirildi: xizmat xabarlari endi o'chirilmaydi. Yoqish: /cleanservice")
}

// cleanedService deletes a service message in a group that asked for it,
// reporting whether it was one: joins, leaves, pins, new titles and photos.
func (s *Service) cleanedService(msg *telegram.Message) bool {
	if s.opts.Extras == nil || s.opts.GroupModerator == nil || !msg.IsService() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	value, err := s.opts.Extras.GroupSetting(ctx, msg.Chat.ID, storage.SettingCleanService)
	if err != nil || value != "on" {
		return false
	}
	if err := s.opts.GroupModerator.DeleteMessage(ctx, msg.Chat.ID, msg.MessageID); err != nil {
		s.opts.Logger.Debug("could not delete a service message", "chat_id", msg.Chat.ID, "error", err)
		return false
	}
	return true
}
