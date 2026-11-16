// Package ranks is the group moderators' ranks, 1 to 5, as Iris and other
// group bots keep them: a member given a rank may have the bot do each
// command whose rank is theirs or lower, without being a Telegram admin. A
// group's creator and the bot's owners stand above every rank.
package ranks

// Top is the highest rank; Above is the creator's and the owners', who give
// it.
const (
	Top   = 5
	Above = Top + 1
)

// Command is what a rank opens: Code names it in the access table, Name says
// it to the group.
type Command struct {
	Code    string
	Name    string
	Default int
}

// Commands are the commands ranks open, with the rank each needs unless a
// group's creator sets another; the defaults follow Iris's.
var Commands = []Command{
	{"ban", "Ban va bandan chiqarish", 1},
	{"kick", "Guruhdan chiqarish (kik)", 1},
	{"mute", "Mut va mutdan chiqarish", 1},
	{"warn", "Ogohlantirish (varn)", 1},
	{"pin", "Xabarni qadash", 2},
	{"media", "Stiker va rasm cheklovi", 2},
	{"delete", "Xabarni o'chirish", 3},
	{"undo", "Oxirgi amalni bekor qilish", 3},
	{"purge", "Ko'p xabarni o'chirish", 4},
	{"group", "Guruh sozlamalari: nom, rasm, havola, yopish", 4},
	{"rank", "Daraja berish va olish", 4},
	{"admin", "Telegram adminligini berish va olish", 5},
	{"access", "Buyruqlar darajasini o'zgartirish", 5},
}

// actions are the moderate tool's actions, by the command each is.
var actions = map[string]string{
	"ban": "ban", "unban": "ban", "kick": "kick", "mute": "mute", "unmute": "mute",
	"warn": "warn", "unwarn": "warn", "pin": "pin", "unpin": "pin", "unpin_all": "pin",
	"no_stickers": "media", "allow_stickers": "media", "no_photos": "media", "allow_photos": "media",
	"delete": "delete", "undo": "undo", "purge": "purge",
	"set_title": "group", "set_description": "group", "set_photo": "group", "delete_photo": "group",
	"set_sticker_set": "group", "delete_sticker_set": "group", "invite_link": "group", "revoke_link": "group",
	"edit_link": "group", "lock": "group", "unlock": "group", "member_tag": "group",
	"promote": "admin", "demote": "admin", "admin_title": "admin",
}

// CodeOf is the command a moderate action is, or "" for one ranks do not
// open.
func CodeOf(action string) string { return actions[action] }

// Find is the command named code.
func Find(code string) (Command, bool) {
	for _, c := range Commands {
		if c.Code == code {
			return c, true
		}
	}
	return Command{}, false
}

// Need is the rank code needs in a group whose creator set overrides.
func Need(code string, overrides map[string]int) int {
	if r, ok := overrides[code]; ok {
		return r
	}
	if c, ok := Find(code); ok {
		return c.Default
	}
	return Above
}
