package app

import (
	"context"
	"log/slog"
	"time"

	"telegram-ai-bot/internal/telegram"
)

// commandsTimeout bounds setting the menus, which must not hold up a start.
const commandsTimeout = 5 * time.Second

// membersMenu is what everyone in a group may use; admins see it too,
// after their own, as Telegram shows an admin the admins' menu alone.
var membersMenu = []telegram.BotCommand{
	{Command: "help", Description: "Yordam va buyruqlar"},
	{Command: "cards", Description: "Kartalar: har 2 soatda token, 1x/5x ochish, kolleksiya"},
	{Command: "games", Description: "Barcha o'yinlar"},
	{Command: "mylink", Description: "Shaxsiy taklif havolangiz: kirganlar sizga hisoblanadi"},
	{Command: "invites_week", Description: "Haftalik reyting: kim nechta odam qo'shdi"},
	{Command: "myinvites", Description: "Siz nechta odam qo'shdingiz"},
	{Command: "profile", Description: "O'yinchi kartangiz: ochko, o'rin, kartalar"},
	{Command: "rights", Description: "Guruhdagi huquqlaringiz"},
	{Command: "rep", Description: "Reputatsiya: \"+\" yoki \"rahmat\" reply bilan beriladi"},
	{Command: "toprep", Description: "Reputatsiya reytingi"},
	{Command: "admins", Description: "Adminlar va moderatorlarni chaqirish"},
	{Command: "report", Description: "Shikoyat: xabarga reply qiling"},
	{Command: "voteban", Description: "Ban uchun ovoz berish: xabarga reply qiling"},
	{Command: "staff", Description: "Moderatorlar ro'yxati"},
	{Command: "id", Description: "Sizning va chatning ID'si"},
	{Command: "weather", Description: "Ob-havo: /weather Toshkent"},
	{Command: "tictactoe", Description: "X-O: do'stingiz yoki men bilan"},
}

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
	telegram.ScopeAllGroupChats: membersMenu,
	telegram.ScopeAllChatAdmins: append([]telegram.BotCommand{
		{Command: "warn", Description: "Ogohlantirish, 3-sida 1 kunga mut (reply yoki @user)"},
		{Command: "unwarn", Description: "Bitta ogohlantirishni olib tashlash"},
		{Command: "mute", Description: "Mut: /mute 1h (reply yoki @user); botni: /mute bot"},
		{Command: "unmute", Description: "Mutdan chiqarish yoki botni gapirtirish"},
		{Command: "ban", Description: "Ban: /ban sabab (reply yoki @user)"},
		{Command: "unban", Description: "Bandan chiqarish (reply yoki @user)"},
		{Command: "kick", Description: "Guruhdan chiqarish (reply yoki @user)"},
		{Command: "del", Description: "Xabarni o'chirish (reply)"},
		{Command: "pin", Description: "Xabarni qadash (reply)"},
		{Command: "promote", Description: "Admin qilish: /promote Unvon (reply yoki @user)"},
		{Command: "demote", Description: "Adminlikni olish (reply yoki @user)"},
		{Command: "tag", Description: "Teg yoki unvon: /tag Matn (reply yoki @user)"},
		{Command: "banlist", Description: "Banlar ro'yxati, bandan chiqarish tugmalari"},
		{Command: "reports", Description: "Ko'rib chiqilmagan shikoyatlar"},
		{Command: "warns", Description: "Kimda nechta ogohlantirish (reply: bittasi)"},
		{Command: "rank", Description: "Daraja berish: /rank 3, olish: /rank 0 (reply)"},
		{Command: "ranklog", Description: "Kim kimga daraja bergan"},
		{Command: "access", Description: "Buyruqlar darajasi: /access ban 2"},
		{Command: "kickinactive", Description: "Faol emaslarni chiqarish: /kickinactive 30"},
		{Command: "amnesty", Description: "Hammani bandan chiqarish"},
		{Command: "ignore", Description: "A'zoga 24 soat javob bermaslik (reply)"},
		{Command: "cleanservice", Description: "\"Kirdi/chiqdi\" xabarlarini o'chirish (yoqish/o'chirish)"},
	}, membersMenu...),
}

// ownerMenu is every command the bot answers, for its owners alone, set
// for each in every group they write in and in their private chat with it. The menus of
// everyone else come first, then the commands they do not list, then the
// owners' own. Aliases, /delete and /ranks, are left out.
var ownerMenu = []telegram.BotCommand{
	{Command: "help", Description: "Yordam va buyruqlar"},
	{Command: "start", Description: "Salomlashish"},
	{Command: "about", Description: "Bot haqida"},
	{Command: "cards", Description: "Kartalar: har 2 soatda token, 1x/5x ochish, kolleksiya"},
	{Command: "card", Description: "Karta ochish (ilovada)"},
	{Command: "mycards", Description: "Kartalaringiz kolleksiyasi"},
	{Command: "cardtop", Description: "Kartalar reytingi: eng ko'p ochko"},
	{Command: "market", Description: "Kartalar bozori: kartalarni almashish"},
	{Command: "duel", Description: "Kartalar dueli: /duel 100 (reply yoki hammaga)"},
	{Command: "profile", Description: "O'yinchi kartangiz: ochko, o'rin, kartalar"},
	{Command: "games", Description: "Barcha o'yinlar"},
	{Command: "game", Description: "Tarozi o'yini"},
	{Command: "snake", Description: "Kosmik ilon o'yini"},
	{Command: "woodcutter", Description: "O'tinchi: daraxtni chop, shoxlardan qoch"},
	{Command: "boss", Description: "Dev jangi: guruh bilan birga devni uring"},
	{Command: "mafia", Description: "Mafia: guruh bilan, yashirin rollar, 5+ kishi"},
	{Command: "tictactoe", Description: "X-O: do'stingiz yoki men bilan"},
	{Command: "checkers", Description: "Shashka: tasodifiy raqib, do'st yoki men bilan"},
	{Command: "teams", Description: "MLBB: o'yinchilarni tasodifiy 2 jamoaga bo'lish"},
	{Command: "mylink", Description: "Shaxsiy taklif havolangiz: kirganlar sizga hisoblanadi"},
	{Command: "myinvites", Description: "Siz nechta odam qo'shdingiz"},
	{Command: "invites", Description: "Reyting: kim nechta odam qo'shdi"},
	{Command: "invites_week", Description: "Haftalik reyting: kim nechta odam qo'shdi"},
	{Command: "invites_month", Description: "Oylik reyting: kim nechta odam qo'shdi"},
	{Command: "rights", Description: "Guruhdagi huquqlaringiz"},
	{Command: "rep", Description: "Reputatsiya: \"+\" yoki \"rahmat\" reply bilan beriladi"},
	{Command: "toprep", Description: "Reputatsiya reytingi"},
	{Command: "admins", Description: "Adminlar va moderatorlarni chaqirish"},
	{Command: "report", Description: "Shikoyat: xabarga reply qiling"},
	{Command: "voteban", Description: "Ban uchun ovoz berish: xabarga reply qiling"},
	{Command: "staff", Description: "Moderatorlar ro'yxati"},
	{Command: "id", Description: "Sizning va chatning ID'si"},
	{Command: "ping", Description: "Bot ishlayaptimi: javob vaqti"},
	{Command: "calc", Description: "Kalkulyator: /calc 2+2"},
	{Command: "weather", Description: "Ob-havo: /weather Toshkent"},
	{Command: "premium", Description: "Premium: nima beradi, sotib olish yoki uzaytirish"},
	{Command: "paysupport", Description: "To'lov bo'yicha kimga yozish"},
	{Command: "warn", Description: "Ogohlantirish, 3-sida 1 kunga mut (reply yoki @user)"},
	{Command: "unwarn", Description: "Bitta ogohlantirishni olib tashlash"},
	{Command: "mute", Description: "Mut: /mute 1h (reply yoki @user); botni: /mute bot"},
	{Command: "unmute", Description: "Mutdan chiqarish yoki botni gapirtirish"},
	{Command: "ban", Description: "Ban: /ban sabab (reply yoki @user)"},
	{Command: "unban", Description: "Bandan chiqarish (reply yoki @user)"},
	{Command: "kick", Description: "Guruhdan chiqarish (reply yoki @user)"},
	{Command: "del", Description: "Xabarni o'chirish (reply)"},
	{Command: "pin", Description: "Xabarni qadash (reply)"},
	{Command: "unpin", Description: "Qadalgan xabarni olib tashlash"},
	{Command: "promote", Description: "Admin qilish: /promote Unvon (reply yoki @user)"},
	{Command: "demote", Description: "Adminlikni olish (reply yoki @user)"},
	{Command: "tag", Description: "Teg yoki unvon: /tag Matn (reply yoki @user)"},
	{Command: "banlist", Description: "Banlar ro'yxati, bandan chiqarish tugmalari"},
	{Command: "reports", Description: "Ko'rib chiqilmagan shikoyatlar"},
	{Command: "warns", Description: "Kimda nechta ogohlantirish (reply: bittasi)"},
	{Command: "rank", Description: "Daraja berish: /rank 3, olish: /rank 0 (reply)"},
	{Command: "ranklog", Description: "Kim kimga daraja bergan"},
	{Command: "access", Description: "Buyruqlar darajasi: /access ban 2"},
	{Command: "kickinactive", Description: "Faol emaslarni chiqarish: /kickinactive 30"},
	{Command: "amnesty", Description: "Hammani bandan chiqarish"},
	{Command: "ignore", Description: "A'zoga 24 soat javob bermaslik (reply)"},
	{Command: "unignore", Description: "A'zoga yana javob berish (reply)"},
	{Command: "cleanservice", Description: "\"Kirdi/chiqdi\" xabarlarini o'chirish (yoqish/o'chirish)"},
	{Command: "invites_auto", Description: "Takliflar reytingini avto-e'lon qilish, yoki off"},
	{Command: "aichat", Description: "Bu guruhda AI suhbatni yoqish/o'chirish (owner)"},
	{Command: "limit", Description: "Guruhda daqiqasiga javoblar soni (owner)"},
	{Command: "active", Description: "Kamida X faol a'zo sharti, yoki off (owner)"},
	{Command: "settings", Description: "Guruh va bot sozlamalari (owner)"},
	{Command: "set", Description: "Sozlamani o'zgartirish: /set NOM qiymat|default (owner)"},
	{Command: "stats", Description: "Statistika paneli (owner)"},
	{Command: "model", Description: "Qaysi model ishlayapti (owner)"},
	{Command: "aigroups", Description: "AI suhbat yoqilgan guruhlarni tanlash (owner)"},
	{Command: "announce", Description: "Guruhlarga e'lon yuborish (owner)"},
	{Command: "payments", Description: "Stars to'lovlari (owner)"},
	{Command: "refund", Description: "To'lovni qaytarish: /refund ID yoki @user (owner)"},
	{Command: "business", Description: "Business hisob javoblari: /business on|off (owner)"},
	{Command: "import_stickers", Description: "Stiker to'plamini import qilish: havola yoki nom (owner)"},
}

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

// ChatCommandSetter sets one chat's menu.
type ChatCommandSetter interface {
	SetChatCommands(ctx context.Context, chatID int64, commands []telegram.BotCommand) error
}

// SetOwnerMenus gives each owner known by ID their own menu in their
// private chat with the bot, logging a failure: one who never wrote to the
// bot has no chat with it yet.
func SetOwnerMenus(ctx context.Context, setter ChatCommandSetter, owners map[int64]bool, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, commandsTimeout)
	defer cancel()
	for owner := range owners {
		if err := setter.SetChatCommands(ctx, owner, ownerMenu); err != nil {
			logger.Warn("could not set an owner's private menu", "error", err)
		}
	}
}
