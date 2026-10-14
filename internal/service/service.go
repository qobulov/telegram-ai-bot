// Package service turns Telegram updates into replies. It decides which
// updates to accept, limits how fast users and groups may send, runs
// commands, and hands ordinary messages to the agent.
package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"telegram-ai-bot/internal/agent"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	noticeQueueSize = 64
	// backgroundTranscriptions bounds voice messages transcribed at once
	// for the day's log while polling.
	backgroundTranscriptions = 2
	sendTimeout              = 15 * time.Second
	muteLookupTimeout        = 5 * time.Second
)

// Sender delivers messages and returns the ID Telegram gave them.
type Sender interface {
	SendMessage(ctx context.Context, msg telegram.Outgoing) (messageID int64, err error)
	SendPhoto(ctx context.Context, photo telegram.Photo) (messageID int64, err error)
}

// Replier answers a user's message.
type Replier interface {
	Reply(ctx context.Context, req agent.Request) (agent.Answer, error)
}

// Resetter deletes one conversation's history.
type Resetter interface {
	Reset(ctx context.Context, scope storage.Scope) error
}

// Members answers whether a user administers a group.
type Members interface {
	IsGroupAdmin(ctx context.Context, chatID, userID int64) (bool, error)
}

// Muter stores whether the bot is muted in a chat.
type Muter interface {
	SetMuted(ctx context.Context, chatID int64, muted bool, by int64) error
}

// MuteStore looks up whether the bot is muted in a chat. It is used where
// the mute state cannot be kept in memory between updates.
type MuteStore interface {
	IsMuted(ctx context.Context, chatID int64) (bool, error)
}

// BotReplyQuota counts replies to other bots per chat.
type BotReplyQuota interface {
	ReserveBotReply(ctx context.Context, chatID, botID int64, since time.Time, limit int) (bool, error)
}

// Recorder stores the Telegram message ID of an answer after it was sent.
type Recorder interface {
	SetSentMessageID(ctx context.Context, answerID, telegramMessageID int64) error
}

// Options configures a Service. All fields are required except MutedChats.
type Options struct {
	Sender   Sender
	Replier  Replier
	Resetter Resetter
	Recorder Recorder
	Members  Members
	Muter    Muter
	Media    Media
	// Transcriber turns voice messages into text; nil means the bot cannot
	// listen to them.
	Transcriber Transcriber
	// ChatLog stores the day's group messages; nil turns the log off.
	ChatLog ChatLog
	// Groups records the bot being added to or removed from groups; nil
	// ignores those updates.
	Groups GroupRegistry
	Logger *slog.Logger
	// Model is the model name shown by /model.
	Model string
	// Workers is how many messages are processed at once.
	Workers int
	// QueueSize is how many updates may wait for each worker.
	QueueSize     int
	RatePerMinute int
	MaxInputChars int
	// TurnTimeout bounds the handling of one update, excluding sending the reply.
	TurnTimeout time.Duration
	// IsAdmin reports the bot's own administrators (ADMIN_USER_IDS).
	IsAdmin       func(userID int64) bool
	AdminCommands map[string]bool

	// Bot is the bot's own account; group messages are matched against it.
	Bot Identity
	// DisplayName is the character's name.
	DisplayName string
	// The ways of addressing the bot in a group that it reacts to.
	MentionTrigger  bool
	ReplyTrigger    bool
	WakeWordTrigger bool
	// WakeWords are the names the bot answers to, in lower case.
	WakeWords []string
	// AmbientMessages is how many of today's group messages are given to the
	// model with a question. Zero turns the group log off.
	AmbientMessages int
	// Location decides when "today" starts for the group log.
	Location *time.Location
	// AllowedBots are other bots answered in groups, lower case; "*" means
	// any bot. BotReplies counts those replies, at most BotRepliesPer10Min
	// per chat.
	AllowedBots        []string
	BotReplies         BotReplyQuota
	BotRepliesPer10Min int
	// OwnerUsernames are the bot owners' Telegram usernames, lower case,
	// without @. Owners' messages are marked for the model and owners may
	// manage the bot in any group.
	OwnerUsernames []string
	// MutedChats are the groups where the bot starts muted.
	MutedChats []int64
	// MuteStore, when set, is asked about a group's mute state every time
	// instead of the copy kept in memory. Serverless deployments need this:
	// each request may run in a different instance, so memory cannot be
	// trusted to know about a /mute handled elsewhere.
	MuteStore MuteStore
	// GroupRatePerMinute limits answers per group, across all its members.
	GroupRatePerMinute int
	// MaxGroupReplyChars is the longest answer posted in a group, in UTF-16
	// code units as Telegram counts them.
	MaxGroupReplyChars int

	// MaxMediaBytes is the largest file downloaded for the model to see.
	MaxMediaBytes int64
	// MaxVoiceSeconds is the longest voice message passed on.
	MaxVoiceSeconds int
	// AudioInput is true when the model provider can hear voice messages.
	AudioInput bool
}

// Service processes updates on a fixed pool of workers.
//
// Each private chat, and each group, is always served by the same worker.
// That gives three things at once: the messages of one conversation are
// handled one at a time and in order, the number of concurrent turns never
// exceeds Workers, and no goroutine is started per update. When a worker's
// queue is full, new updates for it are refused with a "busy" reply instead
// of piling up.
//
// In groups only messages addressed to the bot are queued. The others are
// at most remembered briefly, in memory, as context for the next answer.
type Service struct {
	opts         Options
	userLimiter  *limiter
	groupLimiter *limiter
	ambient      *ambient
	queues       []chan incoming
	notices      chan incoming
	closing      atomic.Bool
	workers      sync.WaitGroup
	// background tracks voice messages being transcribed for the day's log
	// while polling; transcribing bounds how many run at once.
	background   sync.WaitGroup
	transcribing chan struct{}
	notifier     sync.WaitGroup

	mutedMu sync.RWMutex
	muted   map[int64]bool
}

// incoming is an accepted update, reduced to what handling it needs.
type incoming struct {
	updateID int64
	msg      *telegram.Message
	// scope is the conversation: the user's own in a private chat, the
	// group's shared one in a group.
	scope storage.Scope
	// userID is who sent the message, in private chats and groups alike.
	userID int64
	// fromBot is true for a message from another bot.
	fromBot bool
	group   bool
	// text is the user's text, trimmed, without the part addressing the bot.
	text string
	// described is text with the message's media described in front of it,
	// a voice message's transcript included. Filled in by process.
	described string
}

// replyTarget is the message an answer should reply to: the original message
// in a group, so the answer is attached to the question, and none in a
// private chat.
func (in incoming) replyTarget() int64 {
	if in.group {
		return in.msg.MessageID
	}
	return 0
}

// queueKey decides which worker handles the update. Everything from one
// group goes to one worker, so its shared conversation stays in order.
func (in incoming) queueKey() uint64 {
	if in.group {
		return uint64(in.scope.ChatID)
	}
	return uint64(in.userID)
}

// New starts the workers and returns the Service. Call Close to stop it.
func New(opts Options) *Service {
	s := &Service{
		opts:         opts,
		userLimiter:  newLimiter(opts.RatePerMinute),
		groupLimiter: newLimiter(opts.GroupRatePerMinute),
		ambient:      newAmbient(opts.ChatLog, opts.AmbientMessages, opts.Location, opts.Logger),
		muted:        make(map[int64]bool, len(opts.MutedChats)),
		queues:       make([]chan incoming, opts.Workers),
		notices:      make(chan incoming, noticeQueueSize),
		transcribing: make(chan struct{}, backgroundTranscriptions),
	}
	for _, chatID := range opts.MutedChats {
		s.muted[chatID] = true
	}
	for i := range s.queues {
		queue := make(chan incoming, opts.QueueSize)
		s.queues[i] = queue
		s.workers.Go(func() { s.work(queue) })
	}
	s.notifier.Go(s.sendBusyNotices)
	return s
}

// Handle accepts an update for processing and returns immediately. Updates
// the bot should not react to are dropped here. Handle must not be called
// after Close.
func (s *Service) Handle(update telegram.Update) {
	if update.MyChatMember != nil {
		s.recordMembership(update.MyChatMember)
		return
	}
	in, ok := s.accept(update)
	if !ok {
		return
	}
	queue := s.queues[in.queueKey()%uint64(len(s.queues))]
	select {
	case queue <- in:
	default:
		s.opts.Logger.Warn("worker queue full; dropping update",
			"user_id", in.userID, "chat_id", in.scope.ChatID, "update_id", in.updateID)
		s.notifyBusy(in)
	}
}

// HandleNow processes one update on the calling goroutine and returns when
// the reply, if any, has been sent and recorded. It is for callers that
// cannot leave work running after they return, such as a webhook on
// serverless hosting. Unlike Handle it does not use the workers, so it gives
// no ordering between updates that arrive at the same time.
func (s *Service) HandleNow(update telegram.Update) {
	if update.MyChatMember != nil {
		s.recordMembership(update.MyChatMember)
		return
	}
	if in, ok := s.accept(update); ok {
		s.process(in)
	}
}

// GroupRegistry keeps the list of groups the bot is in.
type GroupRegistry interface {
	RecordGroup(ctx context.Context, g storage.GroupStatus) error
}

// recordMembership notes the bot being added to, promoted in or removed from
// a group, so its owners' list of groups stays true even for quiet groups.
func (s *Service) recordMembership(change *telegram.ChatMemberUpdated) {
	isGroup := change.Chat.Type == telegram.ChatTypeGroup || change.Chat.Type == telegram.ChatTypeSupergroup
	if s.opts.Groups == nil || !isGroup {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	err := s.opts.Groups.RecordGroup(ctx, storage.GroupStatus{
		ChatID:    change.Chat.ID,
		Title:     change.Chat.Title,
		Username:  change.Chat.Username,
		BotStatus: change.NewChatMember.Status,
	})
	if err != nil {
		s.opts.Logger.Warn("could not record group membership", "chat_id", change.Chat.ID, "error", err)
		return
	}
	s.opts.Logger.Info("bot membership changed", "chat_id", change.Chat.ID, "status", change.NewChatMember.Status)
}

// accept keeps messages from a person, sent either in a private chat or, in
// a group, addressed to the bot. The sender's ID from the update is the
// identity used for permissions from here on.
func (s *Service) accept(update telegram.Update) (incoming, bool) {
	msg := update.Message
	if msg == nil || msg.From == nil || !s.acceptsSender(msg) {
		return incoming{}, false
	}
	// Posts made on behalf of a chat have no person behind them to keep a
	// conversation with: channel posts forwarded into a linked group, and
	// messages sent as a channel or as the group itself.
	if msg.SenderChat != nil || msg.IsAutomaticForward {
		return incoming{}, false
	}
	in := incoming{updateID: update.UpdateID, msg: msg, userID: msg.From.ID, fromBot: msg.From.IsBot}
	switch msg.Chat.Type {
	case telegram.ChatTypePrivate:
		in.scope = storage.Scope{ChatID: msg.Chat.ID, UserID: msg.From.ID}
		text, _ := msg.Content() // the caption, for a photo
		in.text = strings.TrimSpace(text)
		return in, true
	case telegram.ChatTypeGroup, telegram.ChatTypeSupergroup:
		in.scope = storage.Scope{ChatID: msg.Chat.ID} // shared by the group
		in.group = true
		return s.acceptGroup(in)
	default:
		return incoming{}, false
	}
}

// acceptGroup queues a group message that is addressed to the bot. Any other
// message is only remembered as recent chatter, and not even that while the
// group is muted.
//
// Whether a muted group's message is dropped is decided later, when a worker
// takes it from the queue (see process). Deciding here would be wrong: the
// /mute or /unmute that changes the answer may still be waiting in that same
// queue, ahead of this message.
func (s *Service) acceptGroup(in incoming) (incoming, bool) {
	text, addressed := s.address(in.msg)
	if in.fromBot && strings.HasPrefix(text, "/") {
		addressed = false // bots do not run commands
	}
	if !addressed {
		// Checked in this order so that, with the buffer off, an ordinary
		// group message costs no mute lookup at all.
		if s.ambient.enabled() && !s.isMuted(in.scope.ChatID) {
			s.remember(in.msg)
		}
		return incoming{}, false
	}
	in.text = text
	return in, true
}

func isMuteCommand(text string) bool {
	command := commandName(text)
	return command == "/mute" || command == "/unmute"
}

// remember logs a group message as context for the bot's next answer in
// that group. Photos and stickers are only noted; a voice message is
// transcribed, so that "what did she say?" can be answered later.
func (s *Service) remember(msg *telegram.Message) {
	m, isMedia := s.mediaOf(msg)
	if isMedia && m.audio && len(s.queues) > 0 {
		s.rememberInBackground(msg)
		return
	}
	s.logMessage(msg)
}

func (s *Service) logMessage(msg *telegram.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), transcribeTimeout+mediaTimeout)
	defer cancel()
	content, _ := msg.Content()
	text := s.describe(ctx, msg, content)
	s.ambient.addMessage(msg, s.label(msg.From), text)
}

// rememberInBackground transcribes off the polling loop, which must not wait
// seconds for a model. When too many are already running, the voice message
// is logged without its words rather than queued.
func (s *Service) rememberInBackground(msg *telegram.Message) {
	select {
	case s.transcribing <- struct{}{}:
	default:
		content, _ := msg.Content()
		m, _ := s.mediaOf(msg)
		s.ambient.addMessage(msg, s.label(msg.From), withMediaLabel(m, content))
		return
	}
	s.background.Go(func() {
		defer func() { <-s.transcribing }()
		s.logMessage(msg)
	})
}

// ownerNotice tells the model, in a private chat, that the owner is writing.
const ownerNotice = "[This message is from your owner.]"

// acceptsSender keeps people, and in groups the other bots on the allow
// list. The bot never answers itself.
func (s *Service) acceptsSender(msg *telegram.Message) bool {
	from := msg.From
	if !from.IsBot {
		return true
	}
	if from.ID == s.opts.Bot.ID || msg.Chat.Type == telegram.ChatTypePrivate {
		return false
	}
	for _, allowed := range s.opts.AllowedBots {
		if allowed == "*" || strings.EqualFold(allowed, from.Username) {
			return true
		}
	}
	return false
}

// isOwner reports whether the sender is the bot's owner. It compares the
// Telegram username, which Telegram sets and nobody can type in.
func (s *Service) isOwner(user *telegram.User) bool {
	if user == nil || user.Username == "" {
		return false
	}
	for _, owner := range s.opts.OwnerUsernames {
		if strings.EqualFold(user.Username, owner) {
			return true
		}
	}
	return false
}

// label names a sender in a group: their first name and @username, with
// [owner] after an owner's. authorLabel strips square brackets from names,
// so nobody else can carry the mark.
func (s *Service) label(user *telegram.User) string {
	label := authorLabel(user.FirstName)
	if username := usernameLabel(user.Username); username != "" {
		label += " " + username
	}
	switch {
	case s.isOwner(user):
		label += " [owner]"
	case user.IsBot:
		label += " [bot]"
	}
	return label
}

// silenced reports a message from a muted group that is not one of the
// commands controlling the mute. The bot posts nothing at all in reply to
// such a message, not even a notice.
func (s *Service) silenced(in incoming) bool {
	return in.group && s.isMuted(in.scope.ChatID) && !isMuteCommand(in.text)
}

func (s *Service) isMuted(chatID int64) bool {
	if s.opts.MuteStore != nil {
		return s.lookUpMuted(chatID)
	}
	s.mutedMu.RLock()
	defer s.mutedMu.RUnlock()
	return s.muted[chatID]
}

// lookUpMuted asks the store. If the answer cannot be had, the bot stays
// quiet: speaking in a group that asked for silence is worse than missing
// one message. The mute commands themselves are not affected by this.
func (s *Service) lookUpMuted(chatID int64) bool {
	ctx, cancel := context.WithTimeout(context.Background(), muteLookupTimeout)
	defer cancel()
	muted, err := s.opts.MuteStore.IsMuted(ctx, chatID)
	if err != nil {
		s.opts.Logger.Error("mute lookup failed; staying quiet", "chat_id", chatID, "error", err)
		return true
	}
	return muted
}

func (s *Service) markMuted(chatID int64, muted bool) {
	s.mutedMu.Lock()
	defer s.mutedMu.Unlock()
	if muted {
		s.muted[chatID] = true
		return
	}
	delete(s.muted, chatID)
}

// notifyBusy queues a "busy" reply without blocking the polling loop. If even
// that queue is full the notice is dropped: the bot is overloaded.
func (s *Service) notifyBusy(in incoming) {
	if s.silenced(in) {
		return
	}
	select {
	case s.notices <- in:
	default:
	}
}

func (s *Service) sendBusyNotices() {
	for in := range s.notices {
		s.reply(in, textBusy)
	}
}

func (s *Service) work(queue <-chan incoming) {
	for in := range queue {
		if s.closing.Load() {
			if !s.silenced(in) {
				s.reply(in, textBusy)
			}
			continue
		}
		s.process(in)
	}
}

// Close shuts the service down. The turn each worker is busy with is allowed
// to finish, bounded by TurnTimeout. Updates still waiting in a queue are not
// run: their senders get the "busy" reply, so that shutdown takes about one
// turn at most and nobody is left without an answer.
func (s *Service) Close() {
	s.closing.Store(true)
	s.stop()
}

// stop closes the queues and waits for the workers to empty them.
func (s *Service) stop() {
	for _, queue := range s.queues {
		close(queue)
	}
	s.workers.Wait()
	s.background.Wait()
	close(s.notices)
	s.notifier.Wait()
}

// reply sends text to the chat the message came from and returns the sent
// message's ID, or 0 when sending failed. It uses its own deadline, separate
// from the turn's: a turn that used up its whole time budget must still be
// able to tell the user so.
func (s *Service) reply(in incoming, text string) int64 {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	sentID, err := s.opts.Sender.SendMessage(ctx, telegram.Outgoing{
		ChatID:           in.msg.Chat.ID,
		Text:             text,
		ReplyToMessageID: in.replyTarget(),
	})
	if err != nil {
		s.opts.Logger.Error("send message failed", "chat_id", in.msg.Chat.ID, "error", err)
		return 0
	}
	return sentID
}
