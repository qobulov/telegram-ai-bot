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
// for a group's members, and for its admins and moderators, who also
// moderate with them. Owners' commands, /model among them, are left out, and
// so are the games' many, behind /games. The rest are answered all the same.
var commandMenus = map[string][]telegram.BotCommand{
	telegram.ScopeDefault: {
		{Command: "start", Description: "Salomlashish"},
		{Command: "help", Description: "Yordam va buyruqlar"},
		{Command: "cards", Description: "Kartalar: har 2 soatda token, 1x/5x ochish, kolleksiya"},
		{Command: "games", Description: "Barcha o'yinlar"},
		{Command: "calc", Description: "Kalkulyator: /calc 2+2"},
		{Command: "id", Description: "Sizning ID'ingiz"},
	},
	telegram.ScopeAllGroupChats: {
		{Command: "help", Description: "Yordam va buyruqlar"},
		{Command: "cards", Description: "Kartalar: har 2 soatda token, 1x/5x ochish, kolleksiya"},
		{Command: "games", Description: "Barcha o'yinlar"},
		{Command: "mylink", Description: "Shaxsiy taklif havolangiz: kirganlar sizga hisoblanadi"},
		{Command: "invites_week", Description: "Haftalik reyting: kim nechta odam qo'shdi"},
		{Command: "myinvites", Description: "Siz nechta odam qo'shdingiz"},
		{Command: "profile", Description: "O'yinchi kartangiz: ochko, o'rin, kartalar"},
		{Command: "rights", Description: "Guruhdagi huquqlaringiz"},
		{Command: "admins", Description: "Adminlar va moderatorlarni chaqirish"},
		{Command: "report", Description: "Shikoyat: xabarga reply qiling"},
		{Command: "voteban", Description: "Ban uchun ovoz berish: xabarga reply qiling"},
		{Command: "staff", Description: "Moderatorlar ro'yxati"},
		{Command: "id", Description: "Sizning va chatning ID'si"},
		{Command: "weather", Description: "Ob-havo: /weather Toshkent"},
	},
	telegram.ScopeAllChatAdmins: {
		{Command: "help", Description: "Yordam va buyruqlar"},
		{Command: "warn", Description: "Ogohlantirish, 3-sida 1 kunga mut (reply)"},
		{Command: "unwarn", Description: "Bitta ogohlantirishni olib tashlash (reply)"},
		{Command: "mute", Description: "Mut: a'zoga reply qilib /mute 1h; botni: /mute bot"},
		{Command: "unmute", Description: "Mutdan chiqarish (reply) yoki botni gapirtirish"},
		{Command: "ban", Description: "Ban: /ban sabab (reply)"},
		{Command: "kick", Description: "Guruhdan chiqarish (reply)"},
		{Command: "banlist", Description: "Banlar ro'yxati, bandan chiqarish tugmalari"},
		{Command: "reports", Description: "Ko'rib chiqilmagan shikoyatlar"},
		{Command: "warns", Description: "Kimda nechta ogohlantirish (reply: bittasi)"},
		{Command: "staff", Description: "Moderatorlar ro'yxati (daraja: \"+3 admin mari\")"},
		{Command: "ranklog", Description: "Kim kimga daraja bergan"},
		{Command: "access", Description: "Buyruqlar darajasi: /access ban 2"},
		{Command: "kickinactive", Description: "Faol emaslarni chiqarish: /kickinactive 30"},
		{Command: "promote", Description: "Admin qilish: /promote Unvon (reply); /demote, /tag"},
		{Command: "cards", Description: "Kartalar: har 2 soatda token, 1x/5x ochish, kolleksiya"},
		{Command: "games", Description: "Barcha o'yinlar"},
		{Command: "admins", Description: "Adminlar va moderatorlarni chaqirish"},
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
