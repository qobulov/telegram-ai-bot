package app

import (
	"context"
	"log/slog"
	"time"

	"telegram-ai-bot/internal/telegram"
)

// commandsTimeout bounds setting the menus, which must not hold up a start.
const commandsTimeout = 5 * time.Second

// commandMenus are the commands Telegram lists on "/": in private chats,
// for a group's members, and for its admins, who can also quiet the bot or
// have it ignore a member. Owners' commands, /model among them, are left
// out.
var commandMenus = map[string][]telegram.BotCommand{
	telegram.ScopeDefault: {
		{Command: "start", Description: "Salomlashish"},
		{Command: "help", Description: "Yordam va buyruqlar"},
		{Command: "reset", Description: "Suhbat tarixini o'chirish"},
		{Command: "stop", Description: "Ishlayotgan testni to'xtatish"},
		{Command: "about", Description: "Bot haqida"},
	},
	telegram.ScopeAllGroupChats: {
		{Command: "help", Description: "Yordam va buyruqlar"},
		{Command: "about", Description: "Bot haqida"},
	},
	telegram.ScopeAllChatAdmins: {
		{Command: "help", Description: "Yordam va buyruqlar"},
		{Command: "mute", Description: "Botni guruhda jim qilish"},
		{Command: "unmute", Description: "Botni yana gapirtirish"},
		{Command: "ignore", Description: "A'zoga 24 soat javob bermaslik (xabariga reply qiling)"},
		{Command: "unignore", Description: "A'zoga yana javob berish (xabariga reply qiling)"},
		{Command: "reset", Description: "Guruh suhbat tarixini o'chirish"},
		{Command: "about", Description: "Bot haqida"},
	},
}

// CommandSetter sets a command menu.
type CommandSetter interface {
	SetMyCommands(ctx context.Context, scope string, commands []telegram.BotCommand) error
}

// SetCommandMenus sets the menus, logging a failure: the bot works without
// them, and the next start tries again.
func SetCommandMenus(ctx context.Context, setter CommandSetter, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, commandsTimeout)
	defer cancel()
	for _, scope := range []string{telegram.ScopeDefault, telegram.ScopeAllGroupChats, telegram.ScopeAllChatAdmins} {
		if err := setter.SetMyCommands(ctx, scope, commandMenus[scope]); err != nil {
			logger.Warn("could not set the command menu", "scope", scope, "error", err)
		}
	}
}
