package service

import (
	"context"
	"net/url"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// The guards an owner may turn on in a group, run in code with no model:
// known spammers banned, links to malicious hosts and nude or gory pictures
// deleted. Admins, owners, bots and posts on behalf of a chat are left
// alone, and so is a muted group.

const (
	guardTime      = 15 * time.Second
	guardsMemory   = time.Minute
	verdictMemory  = 6 * time.Hour
	maxHostsLooked = 3
	// maxGuardedImage is the largest picture size looked at; the biggest
	// size Telegram keeps under it is taken.
	maxGuardedImage = 1 << 20
)

// Guards ask outside services about members, links and pictures.
type Guards interface {
	Spammer(ctx context.Context, userID int64) (string, bool, error)
	MaliciousHost(ctx context.Context, host string) (string, bool, error)
	UnsafeImage(ctx context.Context, image []byte) (string, bool, error)
	CanCheckLinks() bool
	CanCheckImages() bool
}

// GroupModerator deletes messages and bans members.
type GroupModerator interface {
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
	BanChatMember(ctx context.Context, chatID, userID int64) error
}

// guardSet are the guards on in a group.
type guardSet struct{ spam, links, images bool }

func (g guardSet) any() bool { return g.spam || g.links || g.images }

// verdict is an outside service's answer kept a while: bad, and why.
type verdict struct {
	bad    bool
	reason string
}

// guardsOn are the guards on in a group, lately read.
func (s *Service) guardsOn(ctx context.Context, chatID int64) guardSet {
	if set, ok := s.guardSets.get(chatID); ok {
		return set
	}
	var set guardSet
	rules, err := s.opts.Rules.GroupRules(ctx, chatID)
	if err != nil {
		s.opts.Logger.Warn("guards: could not read the rules", "chat_id", chatID, "error", err)
		return set
	}
	for _, r := range rules {
		if r.ChatID != chatID {
			continue
		}
		switch r.Kind {
		case storage.RuleSpamGuard:
			set.spam = true
		case storage.RuleLinkGuard:
			set.links = s.opts.Guards.CanCheckLinks()
		case storage.RuleImageGuard:
			set.images = s.opts.Guards.CanCheckImages()
		}
	}
	s.guardSets.set(chatID, set)
	return set
}

// guardable reports a sender the guards look at.
func (s *Service) guardable(msg *telegram.Message) bool {
	return s.opts.Guards != nil && s.opts.GroupModerator != nil && s.opts.Rules != nil &&
		msg.From != nil && !msg.From.IsBot && msg.SenderChat == nil && !s.isOwner(msg.From)
}

// protected acts on a group message a guard catches, and reports whether
// it did: such a message gets nothing else.
func (s *Service) protected(in incoming) bool {
	msg := in.msg
	if !s.guardable(msg) || len(msg.NewChatMembers) > 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), guardTime)
	defer cancel()
	set := s.guardsOn(ctx, msg.Chat.ID)
	if !set.any() || s.isMutedLately(msg.Chat.ID) {
		return false
	}
	if set.spam {
		if source, bad := s.spammer(ctx, msg.From.ID); bad && !s.isGroupAdmin(ctx, msg) {
			s.banSpammer(ctx, in, msg.From, source, true)
			return true
		}
	}
	if set.links {
		if reason, bad := s.maliciousLink(ctx, msg); bad && !s.isGroupAdmin(ctx, msg) {
			s.removeGuarded(ctx, in, storage.ActionLinkDelete, mentionOf(msg.From)+", xavfli havola o'chirildi ("+reason+").")
			return true
		}
	}
	if set.images && len(msg.Photo) > 0 {
		if reason, bad := s.unsafePicture(ctx, msg); bad && !s.isGroupAdmin(ctx, msg) {
			s.removeGuarded(ctx, in, storage.ActionImageDelete, mentionOf(msg.From)+", nomaqbul rasm o'chirildi ("+reason+").")
			return true
		}
	}
	return false
}

// guardJoiners bans the people who just joined and are known spammers, and
// returns them, so they are not welcomed.
func (s *Service) guardJoiners(in incoming) map[int64]bool {
	if s.opts.Guards == nil || s.opts.GroupModerator == nil || s.opts.Rules == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), guardTime)
	defer cancel()
	if !s.guardsOn(ctx, in.scope.ChatID).spam || s.isMutedLately(in.scope.ChatID) {
		return nil
	}
	banned := map[int64]bool{}
	for i := range in.msg.NewChatMembers {
		u := &in.msg.NewChatMembers[i]
		if u.IsBot || s.isOwner(u) {
			continue
		}
		if source, bad := s.spammer(ctx, u.ID); bad {
			s.banSpammer(ctx, in, u, source, false)
			banned[u.ID] = true
		}
	}
	return banned
}

// spammer asks the spam lists about an account, at most once a while.
func (s *Service) spammer(ctx context.Context, userID int64) (string, bool) {
	if v, ok := s.spamVerdicts.get(userID); ok {
		return v.reason, v.bad
	}
	source, bad, err := s.opts.Guards.Spammer(ctx, userID)
	if err != nil {
		s.opts.Logger.Warn("guards: could not ask the spam lists", "user_id", userID, "error", err)
		return "", false
	}
	s.spamVerdicts.set(userID, verdict{bad: bad, reason: source})
	return source, bad
}

// maliciousLink is the first link in a message to a host URLhaus lists.
func (s *Service) maliciousLink(ctx context.Context, msg *telegram.Message) (string, bool) {
	for _, host := range linkHosts(msg) {
		v, ok := s.hostVerdicts.get(host)
		if !ok {
			reason, bad, err := s.opts.Guards.MaliciousHost(ctx, host)
			if err != nil {
				s.opts.Logger.Warn("guards: could not ask about a host", "host", host, "error", err)
				continue
			}
			v = verdict{bad: bad, reason: reason}
			s.hostVerdicts.set(host, v)
		}
		if v.bad {
			return host + ", " + v.reason, true
		}
	}
	return "", false
}

// linkHosts are the hosts a message links to, each once, at most a few.
func linkHosts(msg *telegram.Message) []string {
	text, entities := msg.Content()
	var hosts []string
	for _, e := range entities {
		link := e.URL
		if e.Type == "url" {
			link, _ = telegram.EntityText(text, e)
		}
		if link == "" {
			continue
		}
		if !strings.Contains(link, "://") {
			link = "http://" + link
		}
		u, err := url.Parse(link)
		if err != nil || u.Hostname() == "" {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if !containsString(hosts, host) {
			hosts = append(hosts, host)
		}
		if len(hosts) == maxHostsLooked {
			break
		}
	}
	return hosts
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// unsafePicture asks Sightengine about a message's photo, at the biggest
// size under maxGuardedImage.
func (s *Service) unsafePicture(ctx context.Context, msg *telegram.Message) (string, bool) {
	if s.opts.Media == nil {
		return "", false
	}
	fileID := ""
	for _, size := range msg.Photo {
		if size.FileSize <= maxGuardedImage {
			fileID = size.FileID
		}
	}
	if fileID == "" {
		return "", false
	}
	data, err := s.opts.Media.DownloadFile(ctx, fileID, maxGuardedImage)
	if err != nil {
		s.opts.Logger.Warn("guards: could not download a picture", "chat_id", msg.Chat.ID, "error", err)
		return "", false
	}
	reason, bad, err := s.opts.Guards.UnsafeImage(ctx, data)
	if err != nil {
		s.opts.Logger.Warn("guards: could not check a picture", "chat_id", msg.Chat.ID, "error", err)
		return "", false
	}
	return reason, bad
}

// isGroupAdmin reports a sender who administers the group; when Telegram
// cannot say, the answer is yes, so that nobody is punished on a guess.
func (s *Service) isGroupAdmin(ctx context.Context, msg *telegram.Message) bool {
	if s.opts.AdminList == nil {
		return false
	}
	admins, err := s.opts.AdminList.GetChatAdministrators(ctx, msg.Chat.ID)
	if err != nil {
		s.opts.Logger.Warn("guards: could not list the admins; leaving it", "chat_id", msg.Chat.ID, "error", err)
		return true
	}
	return roleOf(admins, msg.From.ID) != ""
}

// banSpammer bans a known spammer and says so, once per update.
func (s *Service) banSpammer(ctx context.Context, in incoming, user *telegram.User, source string, deleteMessage bool) {
	if !s.recordGuard(ctx, in, storage.ActionSpamBan, user.ID) {
		return
	}
	if deleteMessage {
		if err := s.opts.GroupModerator.DeleteMessage(ctx, in.msg.Chat.ID, in.msg.MessageID); err != nil {
			s.opts.Logger.Warn("guards: could not delete a spammer's message", "chat_id", in.msg.Chat.ID, "error", err)
		}
	}
	if err := s.opts.GroupModerator.BanChatMember(ctx, in.msg.Chat.ID, user.ID); err != nil {
		s.opts.Logger.Warn("guards: could not ban a spammer", "chat_id", in.msg.Chat.ID, "error", err)
		return
	}
	s.postGuard(in.msg, "🚫 "+mentionOf(user)+" spamerlar ro'yxatida ("+source+"), guruhdan chiqarildi.")
}

// removeGuarded deletes a caught message and says why, once per update.
func (s *Service) removeGuarded(ctx context.Context, in incoming, action, note string) {
	if !s.recordGuard(ctx, in, action, in.msg.From.ID) {
		return
	}
	if err := s.opts.GroupModerator.DeleteMessage(ctx, in.msg.Chat.ID, in.msg.MessageID); err != nil {
		s.opts.Logger.Warn("guards: could not delete a message", "chat_id", in.msg.Chat.ID, "action", action, "error", err)
		return
	}
	s.postGuard(in.msg, "⚠️ "+note)
}

// recordGuard notes what a guard did, for the owners' report, and reports
// whether it is the first time for this update: Telegram may deliver one
// twice. Without a store, it acts.
func (s *Service) recordGuard(ctx context.Context, in incoming, action string, userID int64) bool {
	if s.opts.Strikes == nil {
		return true
	}
	first, err := s.opts.Strikes.RecordModeration(ctx, storage.ModerationAction{
		ChatID: in.msg.Chat.ID, Action: action, RequesterID: s.opts.Bot.ID, TargetUserID: userID,
		TargetMessageID: in.msg.MessageID, UpdateID: in.updateID,
	})
	if err != nil {
		s.opts.Logger.Warn("guards: could not record", "chat_id", in.msg.Chat.ID, "action", action, "error", err)
		return true
	}
	return first
}
