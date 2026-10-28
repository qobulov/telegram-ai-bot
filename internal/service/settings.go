package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// An owner sets, for one group, how many answers a minute the bot gives
// there (/limit) and how many members must have written lately for it to
// answer at all (/active). Without a setting the configured default holds.
const (
	// settingsMemory is how long a group's settings are trusted before they
	// are read again; a change made here is seen at once.
	settingsMemory = time.Minute
	maxReplyLimit  = 60
	maxMinActive   = 1000
	// ownerMenuMemory is how long an owner's menu in a group counts as set;
	// Telegram keeps it, so this only spares asking again.
	ownerMenuMemory = 24 * time.Hour
)

// MenuSetter sets the command menu one member sees in one group.
type MenuSetter interface {
	SetMemberCommands(ctx context.Context, chatID, userID int64, commands []telegram.BotCommand) error
}

// showOwnerMenu gives an owner who writes in a group the menu with the
// owners' commands, /limit and /active, there and to them alone. A failure
// is logged: the commands work typed all the same.
func (s *Service) showOwnerMenu(msg *telegram.Message) {
	if len(s.opts.OwnerMenu) == 0 || s.opts.MenuSetter == nil || !s.isOwner(msg.From) {
		return
	}
	key := [2]int64{msg.Chat.ID, msg.From.ID}
	if _, set := s.ownerMenus.get(key); set {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	if err := s.opts.MenuSetter.SetMemberCommands(ctx, msg.Chat.ID, msg.From.ID, s.opts.OwnerMenu); err != nil {
		s.opts.Logger.Warn("could not set an owner's menu", "chat_id", msg.Chat.ID, "error", err)
		return
	}
	s.ownerMenus.set(key, true)
}

// settingWords say "back to the default" and "turn it off".
var (
	defaultWords = map[string]bool{"default": true, "standart": true, "umumiy": true, "reset": true}
	offWords     = map[string]bool{"off": true, "ochir": true, "o'chir": true, "0": true}
)

// groupSetting is the group's setting of kind, if an owner made one. When
// the rules cannot be read the default holds.
func (s *Service) groupSetting(ctx context.Context, chatID int64, kind string) (int, bool) {
	settings, ok := s.settings.get(chatID)
	if !ok {
		settings = s.loadSettings(ctx, chatID)
	}
	n, ok := settings[kind]
	return n, ok
}

func (s *Service) loadSettings(ctx context.Context, chatID int64) map[string]int {
	settings := map[string]int{}
	if s.opts.Rules == nil {
		return settings
	}
	rules, err := s.opts.Rules.GroupRules(ctx, chatID)
	if err != nil {
		s.opts.Logger.Warn("could not read a group's settings; using the defaults", "chat_id", chatID, "error", err)
		return settings
	}
	for _, r := range rules {
		if r.ChatID != chatID || (r.Kind != storage.RuleReplyLimit && r.Kind != storage.RuleMinActive) {
			continue
		}
		if n, err := strconv.Atoi(r.Text); err == nil {
			settings[r.Kind] = n
		}
	}
	s.settings.set(chatID, settings)
	return settings
}

// replyLimit is how many answers a minute the bot gives in a group.
func (s *Service) replyLimit(ctx context.Context, chatID int64) int {
	if n, ok := s.groupSetting(ctx, chatID, storage.RuleReplyLimit); ok {
		return n
	}
	return s.opts.GroupRatePerMinute
}

// minActive is how many members must have written lately in a group for the
// bot to answer there; 0 is no such rule.
func (s *Service) minActive(ctx context.Context, chatID int64) int {
	if n, ok := s.groupSetting(ctx, chatID, storage.RuleMinActive); ok {
		return n
	}
	return s.opts.MinActiveMembers
}

// setReplyLimit is /limit [n|default], for owners in a group.
func (s *Service) setReplyLimit(ctx context.Context, in incoming) string {
	arg, refusal := s.settingArg(in)
	if refusal != "" {
		return refusal
	}
	chatID := in.scope.ChatID
	switch {
	case arg == "":
		_, own := s.groupSetting(ctx, chatID, storage.RuleReplyLimit)
		return fmt.Sprintf("Bu guruhda bot daqiqasiga %d ta javob beradi%s.\nO'zgartirish: /limit 10. Umumiyga qaytarish: /limit default.",
			s.replyLimit(ctx, chatID), defaultMark(!own))
	case defaultWords[arg]:
		if !s.clearSetting(ctx, in, storage.RuleReplyLimit) {
			return textError.to(in)
		}
		return fmt.Sprintf("Bu guruhda umumiy limit qaytdi: daqiqasiga %d ta javob.", s.opts.GroupRatePerMinute)
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 || n > maxReplyLimit {
		return fmt.Sprintf("Son yozing, 1 dan %d gacha: /limit 10. Umumiyga qaytarish: /limit default.", maxReplyLimit)
	}
	if !s.saveSetting(ctx, in, storage.RuleReplyLimit, n) {
		return textError.to(in)
	}
	return fmt.Sprintf("Endi bu guruhda bot daqiqasiga %d ta javob beradi.", n)
}

// setMinActive is /active [n|off|default], for owners in a group.
func (s *Service) setMinActive(ctx context.Context, in incoming) string {
	arg, refusal := s.settingArg(in)
	if refusal != "" {
		return refusal
	}
	chatID := in.scope.ChatID
	days := int(s.opts.ActiveWindow.Hours() / 24)
	switch {
	case arg == "":
		_, own := s.groupSetting(ctx, chatID, storage.RuleMinActive)
		n := s.minActive(ctx, chatID)
		if n <= 0 {
			return "Bu guruhda faol a'zolar sharti o'chiq" + defaultMark(!own) + ": bot har doim javob beradi.\nYoqish: /active 10."
		}
		text := fmt.Sprintf("Bu guruhda bot oxirgi %d kunda kamida %d kishi yozgan bo'lsa javob beradi%s.", days, n, defaultMark(!own))
		if s.opts.ActiveCounter != nil {
			if count, err := s.opts.ActiveCounter.ActiveMembers(ctx, chatID, s.opts.Now().Add(-s.opts.ActiveWindow)); err == nil {
				text += fmt.Sprintf(" Hozir %d kishi.", count)
			}
		}
		return text + "\nO'zgartirish: /active 5. O'chirish: /active off. Umumiyga qaytarish: /active default."
	case defaultWords[arg]:
		if !s.clearSetting(ctx, in, storage.RuleMinActive) {
			return textError.to(in)
		}
		if s.opts.MinActiveMembers <= 0 {
			return "Bu guruhda umumiy qoida qaytdi: faol a'zolar sharti yo'q."
		}
		return fmt.Sprintf("Bu guruhda umumiy qoida qaytdi: kamida %d faol a'zo.", s.opts.MinActiveMembers)
	case offWords[arg]:
		if !s.saveSetting(ctx, in, storage.RuleMinActive, 0) {
			return textError.to(in)
		}
		return "Bu guruhda faol a'zolar sharti o'chirildi: bot har doim javob beradi."
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 || n > maxMinActive {
		return fmt.Sprintf("Son yozing, 1 dan %d gacha: /active 10. O'chirish: /active off.", maxMinActive)
	}
	if !s.saveSetting(ctx, in, storage.RuleMinActive, n) {
		return textError.to(in)
	}
	return fmt.Sprintf("Endi bu guruhda bot oxirgi %d kunda kamida %d kishi yozgan bo'lsa javob beradi.", days, n)
}

// settingArg is the word after the command, lowered, for an owner in a
// group; otherwise the refusal. To anyone else the command does not exist.
func (s *Service) settingArg(in incoming) (string, string) {
	if !s.isOwner(in.msg.From) {
		return "", textUnknown.to(in)
	}
	if !in.group {
		return "", textGroupsOnly
	}
	_, arg, _ := strings.Cut(strings.TrimSpace(in.text), " ")
	return strings.ToLower(strings.TrimSpace(arg)), ""
}

func (s *Service) saveSetting(ctx context.Context, in incoming, kind string, n int) bool {
	_, err := s.opts.Rules.SetGroupRule(ctx, storage.GroupRule{ChatID: in.scope.ChatID, Kind: kind, Text: strconv.Itoa(n), SetBy: in.userID})
	if err != nil {
		s.opts.Logger.Error("could not save a group setting", "chat_id", in.scope.ChatID, "kind", kind, "error", err)
		return false
	}
	s.loadSettings(ctx, in.scope.ChatID)
	return true
}

func (s *Service) clearSetting(ctx context.Context, in incoming, kind string) bool {
	if err := s.opts.Rules.ClearGroupSetting(ctx, in.scope.ChatID, kind); err != nil {
		s.opts.Logger.Error("could not clear a group setting", "chat_id", in.scope.ChatID, "kind", kind, "error", err)
		return false
	}
	s.loadSettings(ctx, in.scope.ChatID)
	return true
}

func defaultMark(isDefault bool) string {
	if isDefault {
		return " (umumiy qiymat)"
	}
	return ""
}
