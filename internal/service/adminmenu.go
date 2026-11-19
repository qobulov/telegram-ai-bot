package service

import (
	"context"

	"telegram-ai-bot/internal/telegram"
)

// A group's admins see there the moderation commands the bot can carry out
// in it, by the rights it holds: no /ban where it may not ban, none at all
// where it is no admin. The menu is set when the bot's membership in the
// group changes and, for groups joined before, once a day when someone
// writes there. Owners and moderators with a rank keep their own menus,
// which Telegram shows over this one.

// AdminMenuSetter sets the menu one group's admins see.
type AdminMenuSetter interface {
	SetChatAdminCommands(ctx context.Context, chatID int64, commands []telegram.BotCommand) error
}

// adminMenuChanged follows the bot's membership in a group: being added,
// promoted, demoted or given other rights sets the admins' menu from the
// rights it now holds. A group it left is forgotten, so that a return sets
// it again.
func (s *Service) adminMenuChanged(change *telegram.ChatMemberUpdated) {
	if !s.adminsHaveMenus() || !isGroupChat(change.Chat) {
		return
	}
	if !change.NewChatMember.IsMember() {
		s.adminMenus.forget(change.Chat.ID)
		return
	}
	s.adminMenus.set(change.Chat.ID, true)
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	s.setAdminMenu(ctx, change.Chat.ID, change.NewChatMember.Rights())
}

// showAdminMenu sets the admins' menu of a group someone writes in, from
// the rights the bot holds there as stored, once in ownerMenuMemory. The
// group is remembered before Telegram is asked, so a failure is not tried
// again on every message.
func (s *Service) showAdminMenu(chatID int64) {
	if !s.adminsHaveMenus() || s.opts.BotRights == nil {
		return
	}
	if _, set := s.adminMenus.get(chatID); set {
		return
	}
	s.adminMenus.set(chatID, true)
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	rights, err := s.opts.BotRights.BotRights(ctx, chatID)
	if err != nil {
		s.opts.Logger.Warn("could not read the bot's rights for the admins' menu", "chat_id", chatID, "error", err)
		return
	}
	s.setAdminMenu(ctx, chatID, rights)
}

// setAdminMenu sets the group's admins' menu for rights, logging a
// failure: the commands work typed all the same.
func (s *Service) setAdminMenu(ctx context.Context, chatID int64, rights []string) {
	if err := s.opts.AdminMenuSetter.SetChatAdminCommands(ctx, chatID, s.opts.AdminMenu(rights)); err != nil {
		s.opts.Logger.Warn("could not set the group's admins' menu", "chat_id", chatID, "error", err)
	}
}

// adminsHaveMenus reports whether groups' admins are given menus by the
// bot's rights.
func (s *Service) adminsHaveMenus() bool {
	return s.opts.AdminMenu != nil && s.opts.AdminMenuSetter != nil
}

func isGroupChat(chat telegram.Chat) bool {
	return chat.Type == telegram.ChatTypeGroup || chat.Type == telegram.ChatTypeSupergroup
}
