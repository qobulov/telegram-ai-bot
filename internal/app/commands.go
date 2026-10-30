package app

import (
	"context"
	"log/slog"
	"slices"
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
		{Command: "about", Description: "Bot haqida"},
		{Command: "dooz", Description: "Dooz (X-O) o'yini"},
	},
	telegram.ScopeAllGroupChats: {
		{Command: "help", Description: "Yordam va buyruqlar"},
		{Command: "about", Description: "Bot haqida"},
		{Command: "dooz", Description: "Dooz (X-O) o'yini"},
	},
	telegram.ScopeAllChatAdmins: {
		{Command: "help", Description: "Yordam va buyruqlar"},
		{Command: "mute", Description: "Botni guruhda jim qilish"},
		{Command: "unmute", Description: "Botni yana gapirtirish"},
		{Command: "ignore", Description: "A'zoga 24 soat javob bermaslik (xabariga reply qiling)"},
		{Command: "unignore", Description: "A'zoga yana javob berish (xabariga reply qiling)"},
		{Command: "about", Description: "Bot haqida"},
		{Command: "dooz", Description: "Dooz (X-O) o'yini"},
	},
}

// ownerMenu is what an owner sees in a group: the admins' menu and the
// commands only owners have, set for them alone in each group they write in.
var ownerMenu = append(append([]telegram.BotCommand(nil), commandMenus[telegram.ScopeAllChatAdmins]...),
	telegram.BotCommand{Command: "limit", Description: "Guruhda daqiqasiga javoblar soni (owner)"},
	telegram.BotCommand{Command: "active", Description: "Kamida X faol a'zo sharti, yoki off (owner)"},
)

// CommandSetter sets a command menu.
type CommandSetter interface {
	SetMyCommands(ctx context.Context, scope string, commands []telegram.BotCommand) error
}

// groupAdminRights are the rights the bot asks for when made a group's
// admin, the ones its features use: delete for deleting, purge and the
// guards; restrict for mutes, bans, locks and the swear guard; pin; invite
// for invite links and join requests; topics for forum_topic; change info
// for the group's name, description, photo and sticker pack; tags for
// member_tag; and promote, for making admins. Whoever adds the bot may untick any of them.
var groupAdminRights = telegram.AdministratorRights{
	CanManageChat:      true,
	CanDeleteMessages:  true,
	CanRestrictMembers: true,
	CanPinMessages:     true,
	CanInviteUsers:     true,
	CanManageTopics:    true,
	CanChangeInfo:      true,
	CanPromoteMembers:  true,
	CanManageTags:      true,
}

// channelAdminRights are what the bot asks for in a channel: to post and
// edit there, for the owners' broadcasts. Being an admin at all also lets
// it see who joined a channel required in private chats.
var channelAdminRights = telegram.AdministratorRights{
	CanManageChat:   true,
	CanPostMessages: true,
	CanEditMessages: true,
}

// RightsSetter sets the admin rights the bot asks for.
type RightsSetter interface {
	SetMyDefaultAdministratorRights(ctx context.Context, rights telegram.AdministratorRights, forChannels bool) error
}

// SetAdminRights sets the rights the bot asks for in groups and in
// channels, logging a failure, as SetCommandMenus does.
func SetAdminRights(ctx context.Context, setter RightsSetter, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, commandsTimeout)
	defer cancel()
	for _, forChannels := range []bool{false, true} {
		rights := groupAdminRights
		if forChannels {
			rights = channelAdminRights
		}
		if err := setter.SetMyDefaultAdministratorRights(ctx, rights, forChannels); err != nil {
			logger.Warn("could not set the admin rights the bot asks for", "channels", forChannels, "error", err)
		}
	}
}

// pageCommands send the game, in every menu while WEBAPP_URL serves it.
// /stats, for the owners alone, is in no menu.
var pageCommands = []telegram.BotCommand{
	{Command: "game", Description: "Tarozi o'yini"},
	{Command: "ilon", Description: "Kosmik ilon o'yini"},
	{Command: "otinchi", Description: "O'tinchi o'yini"},
	{Command: "boss", Description: "Dev jangi (guruh bilan)"},
	{Command: "shashka", Description: "Shashka (PvP, reyting)"},
}

// SetCommandMenus sets the menus, with the pages' commands when pages is
// set, logging a failure: the bot works without them, and the next start
// tries again.
func SetCommandMenus(ctx context.Context, setter CommandSetter, pages bool, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, commandsTimeout)
	defer cancel()
	for _, scope := range []string{telegram.ScopeDefault, telegram.ScopeAllGroupChats, telegram.ScopeAllChatAdmins} {
		menu := commandMenus[scope]
		if pages {
			menu = append(slices.Clip(menu), pageCommands...)
		}
		if err := setter.SetMyCommands(ctx, scope, menu); err != nil {
			logger.Warn("could not set the command menu", "scope", scope, "error", err)
		}
	}
}
