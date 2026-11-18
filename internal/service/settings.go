package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/config"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// Owners change the bot's settings from Telegram, in a private chat alone:
// /set NAME value, and /settings to see them. They come into force when the
// serverless instance rebuilds, within a minute. A group's own values, set
// before, are still read.
const (
	// settingsMemory is how long a group's settings are trusted before they
	// are read again; a change made here is seen at once.
	settingsMemory = time.Minute
	// ownerMenuMemory is how long an owner's menu in a group counts as set;
	// Telegram keeps it, so this only spares asking again.
	ownerMenuMemory = 24 * time.Hour

	settingRateLimit   = "GROUP_RATE_LIMIT_PER_MINUTE"
	settingMinActive   = "GROUP_MIN_ACTIVE_MEMBERS"
	settingActiveDays  = "GROUP_ACTIVE_DAYS"
	settingReplyLength = "MAX_GROUP_REPLY_LENGTH"
)

// defaultWords say "back to the default", offWords "turn it off".
var (
	defaultWords = map[string]bool{"default": true, "standart": true, "umumiy": true, "reset": true}
	offWords     = map[string]bool{"off": true, "ochir": true, "o'chir": true}
)

// SettingsBook keeps the owners' settings, by chat: storage.GlobalSettings
// for the whole bot.
type SettingsBook interface {
	Settings(ctx context.Context, chatID int64) (map[string]string, error)
	SetSetting(ctx context.Context, chatID int64, name, value string, setBy int64) error
	ClearSetting(ctx context.Context, chatID int64, name string) error
}

// MenuSetter sets the command menu one member sees in one group.
type MenuSetter interface {
	SetMemberCommands(ctx context.Context, chatID, userID int64, commands []telegram.BotCommand) error
}

// groupNumber is the group's own setting of name, if an owner made one.
// When the settings cannot be read the bot's hold.
func (s *Service) groupNumber(ctx context.Context, chatID int64, name string) (int, bool) {
	settings, ok := s.settings.get(chatID)
	if !ok {
		settings = s.loadGroupSettings(ctx, chatID)
	}
	n, err := strconv.Atoi(settings[name])
	return n, err == nil
}

func (s *Service) loadGroupSettings(ctx context.Context, chatID int64) map[string]string {
	if s.opts.Settings == nil {
		return nil
	}
	settings, err := s.opts.Settings.Settings(ctx, chatID)
	if err != nil {
		s.opts.Logger.Warn("could not read a group's settings; using the bot's", "chat_id", chatID, "error", err)
		return nil
	}
	s.settings.set(chatID, settings)
	return settings
}

// replyLimit is how many answers a minute the bot gives in a group.
func (s *Service) replyLimit(ctx context.Context, chatID int64) int {
	if n, ok := s.groupNumber(ctx, chatID, settingRateLimit); ok {
		return n
	}
	return s.opts.GroupRatePerMinute
}

// minActive is how many members must have written lately in a group for the
// bot to answer there; 0 is no such rule.
func (s *Service) minActive(ctx context.Context, chatID int64) int {
	if n, ok := s.groupNumber(ctx, chatID, settingMinActive); ok {
		return n
	}
	return s.opts.MinActiveMembers
}

// activeWindow is how far back a group's active members are counted.
func (s *Service) activeWindow(ctx context.Context, chatID int64) time.Duration {
	if n, ok := s.groupNumber(ctx, chatID, settingActiveDays); ok && n > 0 {
		return time.Duration(n) * 24 * time.Hour
	}
	return s.opts.ActiveWindow
}

// replyLength is the longest answer the bot posts in a group.
func (s *Service) replyLength(ctx context.Context, chatID int64) int {
	if n, ok := s.groupNumber(ctx, chatID, settingReplyLength); ok && n > 0 {
		return n
	}
	return s.opts.MaxGroupReplyChars
}

// showSettings is /settings, the bot's settings, for owners in private.
func (s *Service) showSettings(ctx context.Context, in incoming) string {
	if !s.isOwner(in.msg.From) {
		return textUnknown.to(in)
	}
	if s.opts.Settings == nil {
		return textError.to(in)
	}
	global, err := s.opts.Settings.Settings(ctx, storage.GlobalSettings)
	if err != nil {
		return textError.to(in)
	}
	lines := []string{"Botning umumiy sozlamalari:"}
	for _, setting := range config.Settings {
		from := "env"
		if _, ok := global[setting.Name]; ok {
			from = "baza"
		}
		lines = append(lines, fmt.Sprintf("• %s = %s (%s) — %s", setting.Name, s.settingValue(global, setting.Name), from, setting.About))
	}
	lines = append(lines, "", "O'zgartirish: /set NOMI qiymat. Env qiymatiga qaytarish: /set NOMI default.")
	return strings.Join(lines, "\n")
}

// settingValue is the bot's value of name: saved, else the environment's,
// else "standart".
func (s *Service) settingValue(global map[string]string, name string) string {
	if v, ok := global[name]; ok {
		return v
	}
	if s.opts.Getenv != nil {
		if v := s.opts.Getenv(name); v != "" {
			return v
		}
	}
	return "standart"
}

// setSetting is /set NAME value|default, the bot's setting, for owners in
// private.
func (s *Service) setSetting(ctx context.Context, in incoming) string {
	if !s.isOwner(in.msg.From) {
		return textUnknown.to(in)
	}
	fields := strings.Fields(in.text)
	if len(fields) != 3 {
		return "Yozilishi: /set NOMI qiymat, masalan /set GROUP_RATE_LIMIT_PER_MINUTE 10. Ro'yxat: /settings."
	}
	setting, ok := config.FindSetting(fields[1])
	if !ok {
		return fmt.Sprintf("%s degan sozlama yo'q. Ro'yxat: /settings.", fields[1])
	}
	return s.changeSetting(ctx, in, storage.GlobalSettings, setting, fields[2])
}

// changeSetting saves value as the setting in chatID, or clears it for
// "default", and says what was done.
func (s *Service) changeSetting(ctx context.Context, in incoming, chatID int64, setting config.Setting, value string) string {
	if s.opts.Settings == nil {
		return textError.to(in)
	}
	where := "Bot uchun"
	if chatID != storage.GlobalSettings {
		where = "Bu guruh uchun"
	}
	if defaultWords[strings.ToLower(value)] {
		if err := s.opts.Settings.ClearSetting(ctx, chatID, setting.Name); err != nil {
			s.opts.Logger.Error("could not clear a setting", "chat_id", chatID, "name", setting.Name, "error", err)
			return textError.to(in)
		}
		s.settingChanged(ctx, chatID)
		return fmt.Sprintf("%s %s qayta umumiy qiymatda%s.", where, setting.Name, s.takesEffect(chatID))
	}
	if s.opts.CheckSetting != nil {
		if err := s.opts.CheckSetting(setting, value); err != nil {
			return fmt.Sprintf("%s uchun %q bo'lmaydi: %v", setting.Name, value, err)
		}
	}
	if err := s.opts.Settings.SetSetting(ctx, chatID, setting.Name, value, in.userID); err != nil {
		s.opts.Logger.Error("could not save a setting", "chat_id", chatID, "name", setting.Name, "error", err)
		return textError.to(in)
	}
	s.settingChanged(ctx, chatID)
	s.opts.Logger.Info("setting changed", "chat_id", chatID, "name", setting.Name, "user_id", in.userID)
	return fmt.Sprintf("%s %s = %s%s.", where, setting.Name, value, s.takesEffect(chatID))
}

func (s *Service) settingChanged(ctx context.Context, chatID int64) {
	if chatID != storage.GlobalSettings {
		s.loadGroupSettings(ctx, chatID)
	}
}

func (s *Service) takesEffect(chatID int64) string {
	if chatID == storage.GlobalSettings {
		return "; bir daqiqa ichida kuchga kiradi"
	}
	return ""
}

// showOwnerMenu gives an owner who writes in a group the menu with the
// owners' commands there, to them alone. A failure is logged: the commands
// work typed all the same.
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
