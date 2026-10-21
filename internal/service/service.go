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
	// for the group log while polling.
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

// GroupReplyQuota counts the bot's answers to people in groups. It has to
// live outside the process: on serverless hosting each update may run in a
// different instance, and a count kept in memory would limit nothing.
type GroupReplyQuota interface {
	ReserveGroupReply(ctx context.Context, q storage.ReplyQuota) (id int64, granted bool, err error)
	ReleaseGroupReply(ctx context.Context, id int64) error
}

// PrivateQuota counts the answers each user gets in private chats in a day.
// Like GroupReplyQuota it lives in the database, which every instance shares.
type PrivateQuota interface {
	ReservePrivateMessage(ctx context.Context, q storage.DailyQuota) (id int64, allowed, notify bool, err error)
	ReleasePrivateMessage(ctx context.Context, id int64) error
}

// Reactor sets the bot's emoji reaction on a message.
type Reactor interface {
	SetMessageReaction(ctx context.Context, chatID, messageID int64, emoji string) error
}

// ChatActor shows a status, such as "typing", in a chat, in the forum topic
// threadID unless it is 0.
type ChatActor interface {
	SendChatAction(ctx context.Context, chatID, threadID int64, action string) error
}

// AnswerLog tells whom a message of the bot's answered, from the stored
// conversations.
type AnswerLog interface {
	AnsweredAuthor(ctx context.Context, chatID, botMessageID int64) (storage.Author, error)
}

// RuleBook keeps the owners' standing rules, among them whom not to answer.
// They live apart from the conversation, so /mute and /reset leave them.
type RuleBook interface {
	GroupRules(ctx context.Context, chatID int64) ([]storage.GroupRule, error)
	SetGroupRule(ctx context.Context, r storage.GroupRule) (int64, error)
	RemoveIgnore(ctx context.Context, chatID, userID int64) (bool, error)
}

// Options configures a Service. A field may be left nil when its comment
// says what nil, or leaving it unset, does; lists may be empty. The other
// fields are required.
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
	// ChatLog stores the groups' messages; nil turns the log off.
	ChatLog ChatLog
	// Groups records the bot being added to or removed from groups; nil
	// ignores those updates.
	Groups GroupRegistry
	// Profiles holds what each person asked to be remembered about talking
	// to them; nil holds nothing.
	Profiles ProfileBook
	// Exchanges reads a person's talks with the bot in other chats, and
	// MemberCount how many members a group has: together they let a chat
	// that only the person and the bot can read recall those talks.
	Exchanges   ExchangeBook
	MemberCount MemberCounter
	// Bots counts the other bots a group has, which MemberCount includes.
	Bots BotTally
	// MinGroupMembers is the smallest group whose members are answered as a
	// group's; a smaller one only as PrivateChats allows. 0 is no minimum.
	MinGroupMembers int
	// Roster keeps the members seen in each group; nil keeps none.
	Roster MemberBook
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
	// AmbientMessages is how many of a group's recent messages are given to
	// the model with a question. Zero turns the group log off.
	AmbientMessages int
	// Location is the time zone of the times shown to the model: the turn
	// header's date and the group log's times. Nil is UTC.
	Location *time.Location
	// Now is the clock of the turn header and of the log's window; nil is
	// time.Now.
	Now func() time.Time
	// BotRights tells the turn header which admin rights the bot holds in a
	// group; nil leaves them unknown.
	BotRights BotRightsSource
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
	// OwnerIDs, when not empty, replace OwnerUsernames in deciding who is an
	// owner: only these Telegram user IDs are owners.
	OwnerIDs map[int64]bool
	// MutedChats are the groups where the bot starts muted.
	MutedChats []int64
	// MuteStore, when set, is asked about a group's mute state every time
	// instead of the copy kept in memory. Serverless deployments need this:
	// each request may run in a different instance, so memory cannot be
	// trusted to know about a /mute handled elsewhere.
	MuteStore MuteStore
	// GroupReplies counts answers to people in groups: at most
	// GroupRatePerMinute per group in a minute, across all its members, and
	// MemberRepliesPer10Min to one member in 10 minutes. Nil does not limit.
	GroupReplies          GroupReplyQuota
	GroupRatePerMinute    int
	MemberRepliesPer10Min int
	// PrivateQuota counts the answers each user gets in private chats: at
	// most PrivateDailyLimit a day, from midnight in Location. Owners are not
	// counted. Nil, or a limit of 0, does not limit.
	PrivateQuota      PrivateQuota
	PrivateDailyLimit int
	// PrivateChats is whom private chats are answered for: "off" nobody,
	// "owners" the owners only; anything else, everyone.
	PrivateChats string
	// Reactor sets emoji reactions, the bot's answer to applause and the
	// model's when it reacts instead of writing; nil means it never reacts.
	Reactor Reactor
	// ChatActor shows "typing" while the model works on an answer; nil
	// shows nothing.
	ChatActor ChatActor
	// Rules keeps the owners' standing rules and the ignore list; nil means
	// there are none, and /ignore and /unignore are unknown commands.
	Rules RuleBook
	// Strikes, AdminList and Restrictor run the swear guard an owner may
	// turn on in a group; with any of them nil it never acts.
	Strikes    SwearStrikes
	AdminList  AdminLister
	Restrictor Restrictor
	// Judge is the model asked whom a message the guard caught is aimed at.
	Judge InsultJudge
	// Answers tells whom one of the bot's answers in a group answered, so
	// that "don't answer him", replying to it, means that member; nil means
	// it is never known.
	Answers AnswerLog
	// MaxGroupReplyChars is the longest answer posted in a group, in UTF-16
	// code units as Telegram counts them.
	MaxGroupReplyChars int

	// MaxMediaBytes is the largest file downloaded for the model to see.
	MaxMediaBytes int64
	// MaxVoiceSeconds is the longest voice message passed on.
	MaxVoiceSeconds int
	// AudioInput is true when the model provider can hear voice messages.
	AudioInput bool
	// ImagesEnabled is true when the model has the image tool; /help offers
	// drawing only then.
	ImagesEnabled bool
	// FootballEnabled is true when the model has the football tool; /help
	// offers football results only then.
	FootballEnabled bool
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
	opts        Options
	userLimiter *limiter
	ambient     *ambient
	queues      []chan incoming
	notices     chan incoming
	closing     atomic.Bool
	workers     sync.WaitGroup
	// background tracks voice messages being transcribed for the group log
	// while polling; transcribing bounds how many run at once.
	background   sync.WaitGroup
	transcribing chan struct{}
	notifier     sync.WaitGroup

	mutedMu sync.RWMutex
	muted   map[int64]bool

	// typingInterval is how often "typing" is shown again while a turn
	// runs: typingRefresh, shortened by tests.
	typingInterval time.Duration
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
	// trigger is how a group message addressed the bot.
	trigger trigger
	// text is the user's text, trimmed, without the part addressing the bot.
	text string
	// described is text with the message's media described in front of it,
	// a voice message's transcript included. Filled in by process in a
	// group, and by ask in a private chat.
	described string
	// groupReply is the answer taken from the group's quota for this
	// message, to give back if the turn answers nothing; 0 when none was.
	groupReply int64
	// unanswered marks a private message PRIVATE_CHATS does not answer: it
	// gets a reaction, chosen in code, and nothing else.
	unanswered bool
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
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	s := &Service{
		opts:           opts,
		userLimiter:    newLimiter(opts.RatePerMinute),
		ambient:        newAmbient(opts.ChatLog, opts.AmbientMessages, opts.Location, opts.Logger),
		muted:          make(map[int64]bool, len(opts.MutedChats)),
		queues:         make([]chan incoming, opts.Workers),
		notices:        make(chan incoming, noticeQueueSize),
		transcribing:   make(chan struct{}, backgroundTranscriptions),
		typingInterval: typingRefresh,
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
	if update.ChatMember != nil {
		s.recordMember(update.ChatMember)
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
	if update.ChatMember != nil {
		s.recordMember(update.ChatMember)
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
		Rights:    change.NewChatMember.Rights(),
	})
	if err != nil {
		s.opts.Logger.Warn("could not record group membership", "chat_id", change.Chat.ID, "error", err)
		return
	}
	s.opts.Logger.Info("bot membership changed", "chat_id", change.Chat.ID, "status", change.NewChatMember.Status)
}

// MemberBook keeps the members seen in each group. The Bot API cannot list
// them, so this is what the group_members tool reads.
type MemberBook interface {
	SeeMember(ctx context.Context, m storage.GroupMember) error
}

// recordMember notes another member's change in a group, which Telegram
// reports where the bot is an admin: joined, left, banned, promoted.
func (s *Service) recordMember(change *telegram.ChatMemberUpdated) {
	isGroup := change.Chat.Type == telegram.ChatTypeGroup || change.Chat.Type == telegram.ChatTypeSupergroup
	if isGroup {
		s.seeMember(change.Chat.ID, change.NewChatMember.User, change.NewChatMember.Status)
	}
}

// noteMembers notes the people a group message shows: its sender, who is
// there, and anyone it says joined or left.
func (s *Service) noteMembers(msg *telegram.Message) {
	s.seeMember(msg.Chat.ID, msg.From, "")
	for i := range msg.NewChatMembers {
		s.seeMember(msg.Chat.ID, &msg.NewChatMembers[i], "member")
	}
	if msg.LeftChatMember != nil {
		s.seeMember(msg.Chat.ID, msg.LeftChatMember, "left")
	}
}

// seeMember records one member. The bot itself is not one of them. A
// failure is logged: the list is a convenience, not something a reply needs.
func (s *Service) seeMember(chatID int64, user *telegram.User, status string) {
	if s.opts.Roster == nil || user == nil || user.ID == s.opts.Bot.ID {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	err := s.opts.Roster.SeeMember(ctx, storage.GroupMember{
		ChatID: chatID, UserID: user.ID, FirstName: user.FirstName, Username: user.Username, IsBot: user.IsBot, Status: status,
	})
	if err != nil {
		s.opts.Logger.Warn("could not record a group member", "chat_id", chatID, "error", err)
	}
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
	msg = withoutTopicReply(msg)
	in := incoming{updateID: update.UpdateID, msg: msg, userID: msg.From.ID, fromBot: msg.From.IsBot}
	switch msg.Chat.Type {
	case telegram.ChatTypePrivate:
		in.scope = storage.Scope{ChatID: msg.Chat.ID, UserID: msg.From.ID}
		if !s.answersPrivately(msg.From) {
			in.unanswered = true
			return in, true
		}
		text, _ := msg.Content() // the caption, for a photo
		in.text = strings.TrimSpace(text)
		return in, true
	case telegram.ChatTypeGroup, telegram.ChatTypeSupergroup:
		s.noteMembers(msg)
		in.scope = storage.Scope{ChatID: msg.Chat.ID} // shared by the group
		in.group = true
		return s.acceptGroup(in)
	default:
		return incoming{}, false
	}
}

// answersPrivately reports whether a private message from user is answered
// at all under PrivateChats. One that is not gets nothing: no reply, no
// notice, no model call.
func (s *Service) answersPrivately(user *telegram.User) bool {
	switch s.opts.PrivateChats {
	case "off":
		return false
	case "owners":
		return s.isOwner(user)
	}
	return true
}

// withoutTopicReply drops the reply Telegram puts on every message in a forum
// topic (see telegram.Message.ImplicitTopicReply), so that it is not taken
// for one: the log would say every line answers the topic's creator, and the
// model would be shown the topic's first message as a quote. The update's
// own message is left as it was.
func withoutTopicReply(msg *telegram.Message) *telegram.Message {
	if !msg.ImplicitTopicReply() {
		return msg
	}
	plain := *msg
	plain.ReplyToMessage = nil
	return &plain
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
	// Swearing in a guarded group is handled in code and not answered.
	if s.guardedSwearing(in.msg, in.updateID) {
		if s.ambient.enabled() {
			s.remember(in.msg)
		}
		return incoming{}, false
	}
	text, how := s.address(in.msg)
	if in.fromBot && strings.HasPrefix(text, "/") {
		how = triggerNone // bots do not run commands
	}
	if how == triggerNone {
		// Checked in this order so that, with the buffer off, an ordinary
		// group message costs no mute lookup at all.
		if s.ambient.enabled() && !s.isMuted(in.scope.ChatID) {
			s.remember(in.msg)
		}
		return incoming{}, false
	}
	in.text, in.trigger = text, how
	return in, true
}

func isMuteCommand(text string) bool {
	command := commandName(text)
	return command == "/mute" || command == "/unmute"
}

// remember logs a group message as context for the bot's next answer in
// that group. Photos and stickers are only noted; a voice message is
// transcribed, so that "what did she say?" can be answered later, except in
// a group too small for her to answer its members: that would be a call to
// the model for nobody.
func (s *Service) remember(msg *telegram.Message) {
	m, isMedia := s.mediaOf(msg)
	if isMedia && m.audio && s.isSmallGroup(context.Background(), msg.Chat.ID) {
		s.logUnheard(msg, m)
		return
	}
	if isMedia && m.audio && len(s.queues) > 0 {
		s.rememberInBackground(msg)
		return
	}
	s.logMessage(msg)
}

// logUnheard logs a voice message by its label alone, untranscribed.
func (s *Service) logUnheard(msg *telegram.Message, m media) {
	content, _ := msg.Content()
	s.logFrom(msg, withForward(msg, withMediaLabel(m, content)))
}

func (s *Service) logMessage(msg *telegram.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), transcribeTimeout+mediaTimeout)
	defer cancel()
	content, _ := msg.Content()
	s.logFrom(msg, s.describe(ctx, msg, content))
}

// logFrom adds a message from a member, or from an allowed bot, to the
// group's log, as text: the one place where the log's member lines are made.
func (s *Service) logFrom(msg *telegram.Message, text string) {
	s.ambient.addMessage(msg, s.label(msg.From), s.logText(msg, text))
}

// logText is how a message is written in the log. A reply first says whom it
// answers, "(to Ann @ann) no way", so that the log shows who is talking to
// whom; the bot is "You", as in its own lines. A message with no text stays
// empty and so is not logged.
func (s *Service) logText(msg *telegram.Message, text string) string {
	replied := msg.ReplyToMessage
	if text == "" || replied == nil || replied.From == nil {
		return text
	}
	addressee := plainLabel(replied.From)
	if s.isFromBot(replied) {
		addressee = botAuthor
	}
	return "(to " + addressee + ") " + text
}

// rememberInBackground transcribes off the polling loop, which must not wait
// seconds for a model. When too many are already running, the voice message
// is logged without its words rather than queued.
func (s *Service) rememberInBackground(msg *telegram.Message) {
	select {
	case s.transcribing <- struct{}{}:
	default:
		m, _ := s.mediaOf(msg)
		s.logUnheard(msg, m)
		return
	}
	s.background.Go(func() {
		defer func() { <-s.transcribing }()
		s.logMessage(msg)
	})
}

// ownerNotice tells the model, in a private chat, that the owner is writing.
// The persona's owner note quotes it exactly; change both together.
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

// isOwner reports whether the sender is the bot's owner. Both checks use what
// Telegram sets and nobody can type in. With OwnerIDs it compares the account
// ID, because a username can be given up and then taken by someone else;
// without them it compares the username.
func (s *Service) isOwner(user *telegram.User) bool {
	if user == nil {
		return false
	}
	if len(s.opts.OwnerIDs) > 0 {
		return s.opts.OwnerIDs[user.ID]
	}
	if user.Username == "" {
		return false
	}
	for _, owner := range s.opts.OwnerUsernames {
		if strings.EqualFold(user.Username, owner) {
			return true
		}
	}
	return false
}

// The marks label puts after an owner's name and another bot's.
const (
	ownerMark = " [owner]"
	botMark   = " [bot]"
)

// label names a sender in a group: their first name and @username, with
// [owner] after an owner's. authorLabel strips square brackets from names,
// so nobody else can carry the mark.
func (s *Service) label(user *telegram.User) string {
	label := withUsername(authorLabel(user.FirstName), user.Username)
	switch {
	case s.isOwner(user):
		label += ownerMark
	case user.IsBot:
		label += botMark
	}
	return label
}

// unlabel undoes label for a name stored with a turn: the name without its
// mark, and whether the mark said owner or bot.
func unlabel(label string) (name string, owner, bot bool) {
	if name, owner := strings.CutSuffix(label, ownerMark); owner {
		return name, true, false
	}
	name, bot = strings.CutSuffix(label, botMark)
	return name, false, bot
}

// someone stands in, mid-sentence, for a person Telegram did not name. At
// the start of a line, in front of what they said, label writes
// unknownAuthor instead.
const someone = "someone"

// plainLabel names a user inside a line rather than in front of what they
// said: in a quote's header, "(to Ann @ann)" or "<Ann joined the group>". It
// is the first name and @username without label's marks, which only mean
// something in front of a message. A nil user, or one whose name was nothing
// authorLabel keeps, is someone.
func plainLabel(user *telegram.User) string {
	if user == nil {
		return someone
	}
	name := authorLabel(user.FirstName)
	if name == unknownAuthor {
		name = someone
	}
	return withUsername(name, user.Username)
}

// withUsername adds the user's @username, if they have one, after name.
func withUsername(name, username string) string {
	if at := usernameLabel(username); at != "" {
		return name + " " + at
	}
	return name
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
// that queue is full the notice is dropped: the bot is overloaded. A message
// that would not be answered anyway gets none.
func (s *Service) notifyBusy(in incoming) {
	if s.silenced(in) || in.unanswered {
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
			if !s.silenced(in) && !in.unanswered {
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

// react sets an emoji reaction on the message being handled. A reaction is
// a gesture, not an answer anyone waits for, so a failure is only logged at
// debug level. Like reply, it has its own deadline.
func (s *Service) react(in incoming, emoji string) {
	if s.opts.Reactor == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if err := s.opts.Reactor.SetMessageReaction(ctx, in.msg.Chat.ID, in.msg.MessageID, emoji); err != nil {
		s.opts.Logger.Debug("reaction failed", "chat_id", in.msg.Chat.ID, "emoji", emoji, "error", err)
	}
}

// typingRefresh is how often "typing" is shown again while a turn runs.
// Telegram clears it after about 5 seconds.
const typingRefresh = 4 * time.Second

// showTyping shows "typing" in the chat at once and again every
// typingInterval, until stop is called or ctx ends: a turn can take many
// seconds, and without a sign of life people ask again. stop waits for the
// goroutine, so that no "typing" is sent after the answer, where it would
// hang over a finished reply. Like a reaction, it is a gesture, so failures
// are only logged at debug level, and not at all once the turn is over: a
// send cut off by stop is expected.
func (s *Service) showTyping(ctx context.Context, msg *telegram.Message) (stop func()) {
	if s.opts.ChatActor == nil {
		return func() {}
	}
	chatID, threadID := msg.Chat.ID, int64(0)
	if msg.IsTopicMessage {
		threadID = msg.MessageThreadID // where the answer goes, as a reply
	}
	ctx, cancel := context.WithCancel(ctx)
	var typing sync.WaitGroup
	typing.Go(func() {
		ticker := time.NewTicker(s.typingInterval)
		defer ticker.Stop()
		// The loop condition, not the select, ends it, so that no send starts
		// once the turn is over: stop may come before the first one, and
		// select may pick the ticker even when ctx is already done.
		for ctx.Err() == nil {
			err := s.opts.ChatActor.SendChatAction(ctx, chatID, threadID, "typing")
			if err != nil && ctx.Err() == nil {
				s.opts.Logger.Debug("typing indicator failed", "chat_id", chatID, "error", err)
			}
			select {
			case <-ctx.Done():
			case <-ticker.C:
			}
		}
	})
	return func() {
		cancel()
		typing.Wait()
	}
}
