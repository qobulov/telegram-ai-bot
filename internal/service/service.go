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
	sendTimeout     = 15 * time.Second
)

// Sender delivers a text message and returns the ID Telegram gave it.
type Sender interface {
	SendMessage(ctx context.Context, msg telegram.Outgoing) (messageID int64, err error)
}

// Replier answers a user's message.
type Replier interface {
	Reply(ctx context.Context, req agent.Request) (agent.Answer, error)
}

// Resetter deletes one conversation's history.
type Resetter interface {
	Reset(ctx context.Context, scope storage.Scope) error
}

// Recorder stores the Telegram message ID of an answer after it was sent.
type Recorder interface {
	SetSentMessageID(ctx context.Context, answerID, telegramMessageID int64) error
}

// Options configures a Service. All fields are required except DisplayName,
// which is needed only when DisplayNameTrigger is set.
type Options struct {
	Sender   Sender
	Replier  Replier
	Resetter Resetter
	Recorder Recorder
	Logger   *slog.Logger
	// Model is the model name shown by /model.
	Model string
	// Workers is how many messages are processed at once.
	Workers int
	// QueueSize is how many updates may wait for each worker.
	QueueSize     int
	RatePerMinute int
	MaxInputChars int
	// TurnTimeout bounds the handling of one update, excluding sending the reply.
	TurnTimeout   time.Duration
	IsAdmin       func(userID int64) bool
	AdminCommands map[string]bool

	// Bot is the bot's own account; group messages are matched against it.
	Bot Identity
	// The ways of addressing the bot in a group that it reacts to.
	MentionTrigger     bool
	ReplyTrigger       bool
	DisplayNameTrigger bool
	DisplayName        string
	// GroupRatePerMinute limits answers per group, across all its members.
	GroupRatePerMinute int
	// MaxGroupReplyChars is the longest answer posted in a group.
	MaxGroupReplyChars int
}

// Service processes updates on a fixed pool of workers.
//
// Each user is always served by the same worker, which gives three things at
// once: a user's messages are handled one at a time and in order, the number
// of concurrent turns never exceeds Workers, and no goroutine is started per
// update. When a worker's queue is full, new updates for it are refused with
// a "busy" reply instead of piling up.
//
// In groups only messages addressed to the bot are queued at all, so ordinary
// group conversation costs nothing.
type Service struct {
	opts         Options
	userLimiter  *limiter
	groupLimiter *limiter
	queues       []chan incoming
	notices      chan incoming
	closing      atomic.Bool
	workers      sync.WaitGroup
	notifier     sync.WaitGroup
}

// incoming is an accepted update, reduced to what handling it needs.
type incoming struct {
	updateID int64
	msg      *telegram.Message
	// scope is the conversation: this user in this chat.
	scope storage.Scope
	group bool
	// text is the user's text, trimmed, without the part addressing the bot.
	text string
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

// New starts the workers and returns the Service. Call Close to stop it.
func New(opts Options) *Service {
	s := &Service{
		opts:         opts,
		userLimiter:  newLimiter(opts.RatePerMinute),
		groupLimiter: newLimiter(opts.GroupRatePerMinute),
		queues:       make([]chan incoming, opts.Workers),
		notices:      make(chan incoming, noticeQueueSize),
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
	in, ok := s.accept(update)
	if !ok {
		return
	}
	queue := s.queues[uint64(in.scope.UserID)%uint64(len(s.queues))]
	select {
	case queue <- in:
	default:
		s.opts.Logger.Warn("worker queue full; dropping update",
			"user_id", in.scope.UserID, "chat_id", in.scope.ChatID, "update_id", in.updateID)
		s.notifyBusy(in)
	}
}

// accept keeps messages from a person, sent either in a private chat or, in
// a group, addressed to the bot. The sender's ID from the update is the
// identity used for history and permissions from here on.
func (s *Service) accept(update telegram.Update) (incoming, bool) {
	msg := update.Message
	if msg == nil || msg.From == nil || msg.From.IsBot {
		return incoming{}, false
	}
	in := incoming{
		updateID: update.UpdateID,
		msg:      msg,
		scope:    storage.Scope{ChatID: msg.Chat.ID, UserID: msg.From.ID},
	}
	switch msg.Chat.Type {
	case telegram.ChatTypePrivate:
		in.text = strings.TrimSpace(msg.Text)
		return in, true
	case telegram.ChatTypeGroup, telegram.ChatTypeSupergroup:
		text, addressed := s.address(msg)
		in.group, in.text = true, text
		return in, addressed
	default:
		return incoming{}, false
	}
}

// notifyBusy queues a "busy" reply without blocking the polling loop. If even
// that queue is full the notice is dropped: the bot is overloaded.
func (s *Service) notifyBusy(in incoming) {
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
			s.reply(in, textBusy)
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
