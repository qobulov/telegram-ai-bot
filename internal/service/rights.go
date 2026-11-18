package service

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"telegram-ai-bot/internal/ranks"
	"telegram-ai-bot/internal/telegram"
)

// myRights answers /rights or "huquqlarim": the asker's rank in the group
// and the commands it opens them; for a Telegram admin also the commands
// their admin rights open.
func (s *Service) myRights(ctx context.Context, in incoming) outcome {
	mine, err := s.askerRank(ctx, in)
	if err != nil {
		return say(textError.to(in))
	}
	if mine >= ranks.Above {
		return say("👑 Siz guruh egasisiz: barcha buyruqlar sizga ochiq.")
	}
	admin, held, rightsKnown := false, []string(nil), false
	if s.opts.Members != nil {
		var err error
		admin, held, err = s.opts.Members.AdminRights(ctx, in.scope.ChatID, in.userID)
		rightsKnown = err == nil
		if err != nil {
			s.opts.Logger.Warn("could not read a member's admin rights", "chat_id", in.scope.ChatID, "user_id", in.userID, "error", err)
			admin, _ = s.opts.Members.IsGroupAdmin(ctx, in.scope.ChatID, in.userID)
		}
	}
	overrides, err := s.opts.Ranks.CommandRanks(ctx, in.scope.ChatID)
	if err != nil {
		return say(textError.to(in))
	}
	var byRank []ranks.Command
	for _, c := range ranks.Commands {
		if mine > 0 && mine >= ranks.Need(c.Code, overrides) {
			byRank = append(byRank, c)
		}
	}
	switch {
	case admin && rightsKnown:
		return say(adminRightsText(mine, held, byRank))
	case len(byRank) > 0:
		head := fmt.Sprintf("🎖 Darajangiz: %d. Sizga ochiq buyruqlar:", mine)
		if admin {
			head = fmt.Sprintf("🛡 Siz Telegram adminisiz, darajangiz: %d. Darajangiz ochgan buyruqlar:", mine)
		}
		return say(head + "\n" + strings.Join(rankLines(byRank), "\n"))
	case admin:
		return say("🛡 Siz Telegram adminisiz: o'z huquqlaringiz doirasidagi moderatsiya buyruqlari ochiq.")
	}
	return say("Sizda daraja yo'q: oddiy a'zo buyruqlari ochiq — /report, /voteban, /admins, /invites. Darajalar: /ranks")
}

func rankLines(byRank []ranks.Command) []string {
	lines := make([]string, len(byRank))
	for i, c := range byRank {
		lines[i] = "• " + c.Name
	}
	return lines
}

// rightCommands are the commands each Telegram admin right opens its
// holder, as the moderate tool checks them (neededRight in
// internal/tools/moderation.go): the person asking must hold the right.
var rightCommands = []struct{ right, line string }{
	{telegram.RightRestrictMembers, "• /mute, /unmute, /ban, /unban, /kick, /warn, /unwarn, /amnesty, /kickinactive — a'zolarni cheklash; stiker va rasm cheklovi"},
	{telegram.RightDeleteMessages, "• /del — xabarni o'chirish, ko'p xabarni o'chirish"},
	{telegram.RightPinMessages, "• /pin, /unpin — xabarni qadash"},
	{telegram.RightPromoteMembers, "• /promote, /demote — admin qilish va adminlikni olish, adminlar unvoni"},
	{telegram.RightManageTags, "• /tag — a'zoning tegi"},
	{telegram.RightChangeInfo, "• guruh nomi, tavsifi, rasmi, stiker to'plami — so'z bilan so'rang"},
	{telegram.RightInviteUsers, "• taklif havolalari — so'z bilan so'rang"},
}

// everyAdmin are the commands any Telegram admin of the group may use,
// whatever their rights: the bot checks only that they are an admin.
const everyAdmin = "• /banlist, /reports, /warns, /cleanservice, /ignore, /unignore, /invites_auto, /mute bot — har bir admin uchun"

// rankRights are the admin rights that open, with no rank, all a rank
// command does; a command missing here (undo, rank, access) only a rank
// opens. banlist needs no right: any admin may see it.
var rankRights = map[string][]string{
	"ban": {telegram.RightRestrictMembers}, "kick": {telegram.RightRestrictMembers},
	"mute": {telegram.RightRestrictMembers}, "warn": {telegram.RightRestrictMembers},
	"media": {telegram.RightRestrictMembers}, "amnesty": {telegram.RightRestrictMembers},
	"masskick": {telegram.RightRestrictMembers}, "pin": {telegram.RightPinMessages},
	"delete": {telegram.RightDeleteMessages}, "purge": {telegram.RightDeleteMessages},
	"admin": {telegram.RightPromoteMembers}, "banlist": {},
	"group": {telegram.RightChangeInfo, telegram.RightInviteUsers, telegram.RightRestrictMembers, telegram.RightManageTags},
}

// adminRightsText lists what a Telegram admin holding the rights held may
// do, and under it what their rank opens that those rights do not.
func adminRightsText(rank int, held []string, byRank []ranks.Command) string {
	head := "🛡 Siz Telegram adminisiz. Sizga ochiq buyruqlar:"
	if rank > 0 {
		head = fmt.Sprintf("🛡 Siz Telegram adminisiz, darajangiz: %d. Sizga ochiq buyruqlar:", rank)
	}
	lines := []string{head}
	for _, c := range rightCommands {
		if slices.Contains(held, c.right) {
			lines = append(lines, c.line)
		}
	}
	lines = append(lines, everyAdmin)
	var extra []ranks.Command
	for _, c := range byRank {
		needs, known := rankRights[c.Code]
		if !known || slices.ContainsFunc(needs, func(r string) bool { return !slices.Contains(held, r) }) {
			extra = append(extra, c)
		}
	}
	if len(extra) > 0 {
		lines = append(lines, "", "🎖 Darajangiz bilan yana:")
		lines = append(lines, rankLines(extra)...)
	}
	return strings.Join(append(lines, "", "Huquq kerak bo'lgan amal uchun o'sha huquq menda ham bo'lishi kerak."), "\n")
}

// rightsWords ask what the asker may do in the group.
var rightsWords = map[string]bool{"huquqlarim": true, "huquqim": true, "huquqlarim qanday": true, "мои права": true}

func (s *Service) isRightsQuestion(text string) bool {
	return commandName(text) == "/rights" || rightsWords[strings.Join(s.orderWords(text), " ")]
}
