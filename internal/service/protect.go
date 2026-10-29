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
// known spammers banned, links to malicious hosts and nude pictures
// deleted. The spam and link guards leave admins, owners, bots and posts on
// behalf of a chat alone; the picture guard spares nobody. A muted group is
// left alone.

const (
	guardTime      = 15 * time.Second
	guardsMemory   = time.Minute
	verdictMemory  = 6 * time.Hour
	maxLinksLooked = 3
	// maxGuardedImage is the largest picture size looked at; the biggest
	// size Telegram keeps under it is taken.
	maxGuardedImage = 1 << 20
)

// Guards ask outside services about members, links and pictures.
type Guards interface {
	Spammer(ctx context.Context, userID int64) (string, bool, error)
	MaliciousLink(ctx context.Context, link string) (string, bool, error)
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

// guardsReady reports whether the guards can run at all.
func (s *Service) guardsReady() bool {
	return s.opts.Guards != nil && s.opts.GroupModerator != nil && s.opts.Rules != nil
}

// guardable reports a sender the spam and link guards look at.
func (s *Service) guardable(msg *telegram.Message) bool {
	return !msg.From.IsBot && msg.SenderChat == nil && !s.isOwner(msg.From)
}

// protected acts on a group message a guard catches, and reports whether
// it did: such a message gets nothing else.
func (s *Service) protected(in incoming) bool {
	msg := in.msg
	if !s.guardsReady() || msg.From == nil || len(msg.NewChatMembers) > 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), guardTime)
	defer cancel()
	set := s.guardsOn(ctx, msg.Chat.ID)
	if !set.any() || s.isMutedLately(msg.Chat.ID) {
		return false
	}
	guardable := s.guardable(msg)
	if guardable && set.spam {
		if source, bad := s.spammer(ctx, msg.From.ID); bad && !s.isGroupAdmin(ctx, msg) {
			s.banSpammer(ctx, in, msg.From, source, true)
			return true
		}
	}
	if guardable && set.links {
		if reason, bad := s.maliciousLink(ctx, msg); bad && !s.isGroupAdmin(ctx, msg) {
			s.removeGuarded(ctx, in, storage.ActionLinkDelete, mentionOf(msg.From)+", xavfli havola o'chirildi ("+reason+").")
			return true
		}
	}
	// A nude picture goes whoever sent it, owners and admins too; the
	// owners' punishment for it spares them.
	if set.images {
		if reason, bad := s.unsafePicture(ctx, msg); bad {
			if s.removeGuarded(ctx, in, storage.ActionImageDelete, senderMention(msg)+", nomaqbul rasm o'chirildi ("+reason+").") && guardable {
				s.punishPicture(ctx, msg)
			}
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

// maliciousLink is the host of the first link in a message the link
// services take for malware or phishing, and why.
func (s *Service) maliciousLink(ctx context.Context, msg *telegram.Message) (string, bool) {
	for _, link := range messageLinks(msg) {
		v, ok := s.linkVerdicts.get(link.String())
		if !ok {
			reason, bad, err := s.opts.Guards.MaliciousLink(ctx, link.String())
			if err != nil {
				s.opts.Logger.Warn("guards: could not ask about a link", "host", link.Hostname(), "error", err)
				continue
			}
			v = verdict{bad: bad, reason: reason}
			s.linkVerdicts.set(link.String(), v)
		}
		if v.bad {
			return link.Hostname() + ", " + v.reason, true
		}
	}
	return "", false
}

// messageLinks are the web links in a message, each once, at most a few,
// their hosts in lower case.
func messageLinks(msg *telegram.Message) []*url.URL {
	text, entities := msg.Content()
	var links []*url.URL
	seen := map[string]bool{}
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
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		u.Host = strings.ToLower(u.Host)
		if seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		links = append(links, u)
		if len(links) == maxLinksLooked {
			break
		}
	}
	return links
}

// unsafePicture asks Sightengine about a message's picture, once per file
// for verdictMemory: a sticker is sent again and again.
func (s *Service) unsafePicture(ctx context.Context, msg *telegram.Message) (string, bool) {
	picture, ok := guardedPicture(msg)
	if !ok || s.opts.Media == nil {
		return "", false
	}
	if v, ok := s.pictureVerdicts.get(picture.FileUniqueID); ok {
		return v.reason, v.bad
	}
	data, err := s.opts.Media.DownloadFile(ctx, picture.FileID, maxGuardedImage)
	if err != nil {
		s.opts.Logger.Warn("guards: could not download a picture", "chat_id", msg.Chat.ID, "error", err)
		return "", false
	}
	reason, bad, err := s.opts.Guards.UnsafeImage(ctx, data)
	if err != nil {
		s.opts.Logger.Warn("guards: could not check a picture", "chat_id", msg.Chat.ID, "error", err)
		return "", false
	}
	s.opts.Logger.Info("guards: picture checked", "chat_id", msg.Chat.ID, "user_id", msg.From.ID, "unsafe", bad, "reason", reason)
	if picture.FileUniqueID != "" {
		s.pictureVerdicts.set(picture.FileUniqueID, verdict{bad: bad, reason: reason})
	}
	return reason, bad
}

// guardedPicture is what the picture guard looks at: a photo at the biggest
// size under maxGuardedImage, a static sticker, a picture sent as a file,
// or the first frame of a GIF, video or animated sticker.
func guardedPicture(msg *telegram.Message) (telegram.PhotoSize, bool) {
	var picture telegram.PhotoSize
	switch {
	case len(msg.Photo) > 0:
		for _, size := range msg.Photo {
			if size.FileSize <= maxGuardedImage {
				picture = size
			}
		}
	case msg.Sticker != nil && !msg.Sticker.IsAnimated && !msg.Sticker.IsVideo:
		picture = telegram.PhotoSize{FileID: msg.Sticker.FileID, FileUniqueID: msg.Sticker.FileUniqueID}
	case msg.Sticker != nil:
		picture = thumbnailOf(msg.Sticker.Thumbnail)
	case msg.Animation != nil: // before Document, which Telegram also sets on a GIF
		picture = thumbnailOf(msg.Animation.Thumbnail)
	case msg.Video != nil:
		picture = thumbnailOf(msg.Video.Thumbnail)
	case msg.VideoNote != nil:
		picture = thumbnailOf(msg.VideoNote.Thumbnail)
	case msg.Document != nil && strings.HasPrefix(msg.Document.MimeType, "image/") && msg.Document.FileSize <= maxGuardedImage:
		picture = telegram.PhotoSize{FileID: msg.Document.FileID, FileUniqueID: msg.Document.FileUniqueID}
	}
	return picture, picture.FileID != ""
}

func thumbnailOf(thumbnail *telegram.PhotoSize) telegram.PhotoSize {
	if thumbnail == nil {
		return telegram.PhotoSize{}
	}
	return *thumbnail
}

// senderMention names who sent a message: the chat it was posted on behalf
// of, such as an anonymous admin's group, or the member.
func senderMention(msg *telegram.Message) string {
	if msg.SenderChat != nil && msg.SenderChat.Title != "" {
		return msg.SenderChat.Title
	}
	return mentionOf(msg.From)
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

// removeGuarded deletes a caught message and says why, once per update,
// and reports whether it did.
func (s *Service) removeGuarded(ctx context.Context, in incoming, action, note string) bool {
	if !s.recordGuard(ctx, in, action, in.msg.From.ID) {
		return false
	}
	if err := s.opts.GroupModerator.DeleteMessage(ctx, in.msg.Chat.ID, in.msg.MessageID); err != nil {
		s.opts.Logger.Warn("guards: could not delete a message", "chat_id", in.msg.Chat.ID, "action", action, "error", err)
		return false
	}
	s.postGuard(in.msg, "⚠️ "+note)
	return true
}

// punishPicture carries out the owners' punishment for an unsafe picture,
// when they set one for its sender or for anyone in the group.
func (s *Service) punishPicture(ctx context.Context, msg *telegram.Message) {
	rules, err := s.opts.Rules.GroupRules(ctx, msg.Chat.ID)
	if err != nil {
		s.opts.Logger.Warn("guards: could not read the rules", "chat_id", msg.Chat.ID, "error", err)
		return
	}
	if _, p, ok := punishmentFor(rules, msg.Chat.ID, msg.From.ID, storage.RulePicturePunishment); ok {
		role := ""
		if s.isGroupAdmin(ctx, msg) {
			role = "administrator"
		}
		s.punish(ctx, msg, p, role, "nomaqbul rasm yubordi", "")
	}
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
