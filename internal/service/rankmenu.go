package service

import (
	"context"

	"telegram-ai-bot/internal/telegram"
)

// A moderator with a rank who is not the group's Telegram admin sees, in
// that group, the moderation commands their rank opens there, through a
// menu for them alone. It is set when /rank gives the rank and, once a day,
// when they write; /rank 0 takes it away. Owners have their own menu, and
// Telegram admins the admins' one, which a member's menu would hide.

// showRankMenu sets the menu of a ranked member who writes in a group, once
// in ownerMenuMemory. A failure is logged: the commands work typed all the
// same.
func (s *Service) showRankMenu(msg *telegram.Message) {
	if !s.ranksHaveMenus() || msg.From == nil || msg.From.IsBot || s.isOwner(msg.From) {
		return
	}
	key := [2]int64{msg.Chat.ID, msg.From.ID}
	if _, read := s.rankMenus.get(key); read {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	rank, err := s.opts.Ranks.RankOf(ctx, msg.Chat.ID, msg.From.ID)
	if err != nil {
		s.opts.Logger.Warn("could not read a rank for the menu", "chat_id", msg.Chat.ID, "error", err)
		return
	}
	if rank == 0 {
		// A menu left from a rank taken away went with /rank 0.
		s.rankMenus.set(key, 0)
		return
	}
	s.setRankMenu(ctx, msg.Chat.ID, msg.From, rank)
}

// rankMenuChanged follows /rank: it gives the member the menu of their new
// rank, or with 0 takes their menu away.
func (s *Service) rankMenuChanged(ctx context.Context, chatID int64, member *telegram.User, rank int) {
	if !s.ranksHaveMenus() || s.isOwner(member) {
		return
	}
	if rank > 0 {
		s.setRankMenu(ctx, chatID, member, rank)
		return
	}
	if err := s.opts.MenuSetter.DeleteMemberCommands(ctx, chatID, member.ID); err != nil {
		s.opts.Logger.Warn("could not take a moderator's menu away", "chat_id", chatID, "error", err)
		return
	}
	s.rankMenus.set([2]int64{chatID, member.ID}, 0)
}

// setRankMenu gives member the menu of rank in the group, unless they are
// its Telegram admin, and remembers the rank.
func (s *Service) setRankMenu(ctx context.Context, chatID int64, member *telegram.User, rank int) {
	key := [2]int64{chatID, member.ID}
	if s.opts.Members != nil {
		admin, err := s.opts.Members.IsGroupAdmin(ctx, chatID, member.ID)
		if err != nil {
			s.opts.Logger.Warn("could not tell whether a moderator is an admin, for the menu", "chat_id", chatID, "error", err)
			return
		}
		if admin {
			s.rankMenus.set(key, rank)
			return
		}
	}
	overrides, err := s.opts.Ranks.CommandRanks(ctx, chatID)
	if err != nil {
		s.opts.Logger.Warn("could not read the commands' ranks for the menu", "chat_id", chatID, "error", err)
		return
	}
	if err := s.opts.MenuSetter.SetMemberCommands(ctx, chatID, member.ID, s.opts.RankMenu(rank, overrides)); err != nil {
		s.opts.Logger.Warn("could not set a moderator's menu", "chat_id", chatID, "error", err)
		return
	}
	s.rankMenus.set(key, rank)
}

// ranksHaveMenus reports whether moderators are given menus by rank.
func (s *Service) ranksHaveMenus() bool {
	return s.opts.RankMenu != nil && s.opts.MenuSetter != nil && s.opts.Ranks != nil
}
