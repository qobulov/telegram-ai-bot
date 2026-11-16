package service

import (
	"context"
	"strconv"
	"strings"

	"telegram-ai-bot/internal/telegram"
)

// A member's own invite link: whoever joins by it is counted for them. A
// group's public link, or an admin's link a member shares, tells the bot only
// who made the link, so the people it brings were counted for that admin, or
// for nobody.

// MemberLinks makes and keeps the members' own invite links.
type MemberLinks interface {
	MemberLink(ctx context.Context, chatID, userID int64) (string, bool, error)
	SaveMemberLink(ctx context.Context, chatID, userID int64, link string) error
}

// LinkMaker makes a group's invite link.
type LinkMaker interface {
	CreateChatInviteLink(ctx context.Context, l telegram.InviteLink) (string, error)
}

// memberLinkPrefix starts the name of a member's link, the member's ID
// after it: "m123456 Bek".
const memberLinkPrefix = "m"

// isMyLink reports "/mylink" or the same in words.
func (s *Service) isMyLink(in incoming) bool {
	if !in.group || s.opts.MemberLinks == nil || s.opts.LinkMaker == nil {
		return false
	}
	if commandName(in.text) == "/mylink" {
		return true
	}
	written, _ := in.msg.Content()
	switch strings.Join(s.orderWords(written), " ") {
	case "mening havolam", "havolam", "taklif havolam", "моя ссылка", "my link":
		return true
	}
	return false
}

// myLink gives the member their own invite link, made once.
func (s *Service) myLink(ctx context.Context, in incoming) outcome {
	chatID := in.scope.ChatID
	link, found, err := s.opts.MemberLinks.MemberLink(ctx, chatID, in.userID)
	if err != nil {
		return say(textError.to(in))
	}
	if !found {
		name := memberLinkPrefix + strconv.FormatInt(in.userID, 10) + " " + doozName(in.msg.From)
		if r := []rune(name); len(r) > 32 {
			name = string(r[:32])
		}
		link, err = s.opts.LinkMaker.CreateChatInviteLink(ctx, telegram.InviteLink{ChatID: chatID, Name: name})
		if err != nil {
			return say("Havola yaratib bo'lmadi: menga guruhda \"Foydalanuvchilarni taklif qilish\" huquqi kerak.")
		}
		if err := s.opts.MemberLinks.SaveMemberLink(ctx, chatID, in.userID, link); err != nil {
			s.opts.Logger.Warn("could not keep a member's link", "chat_id", chatID, "error", err)
		}
	}
	return say("🔗 " + cardMarks.Replace(doozName(in.msg.From)) + ", shaxsiy havolangiz:\n" + link +
		"\nShu havola orqali kirganlar sizga hisoblanadi: /invites")
}

// linkOwner is the member a link the bot made for them belongs to, by the
// ID its name carries; 0 for any other link.
func (s *Service) linkOwner(link *telegram.ChatInviteLink) int64 {
	if link == nil || link.Creator == nil || link.Creator.ID != s.opts.Bot.ID {
		return 0
	}
	first, _, _ := strings.Cut(link.Name, " ")
	if !strings.HasPrefix(first, memberLinkPrefix) {
		return 0
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(first, memberLinkPrefix), 10, 64)
	if err != nil {
		return 0
	}
	return id
}
