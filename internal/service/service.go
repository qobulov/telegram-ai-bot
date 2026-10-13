// Package service turns Telegram updates into replies. It decides which
// updates to accept, limits how fast users may send, runs commands, and hands
// ordinary messages to the agent.
package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"telegram-ai-bot/internal/telegram"
)

const (
	noticeQueueSize = 64
	sendTimeout     = 15 * time.Second
)

// Sender delivers a text message to a chat.
type Sender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

// Replier answers a user's message.
type Replier interface {
	Reply(ctx context.Context, userID, updateID int64, text string) (string, error)
}

// Resetter deletes a user's conversation history.
type Resetter interface {
	Reset(ctx context.Context, userID int64) error
}

// Options configures a Service. All fields are required.
type Options struct {
	Sender   Sender
	Replier  Replier
	Resetter Resetter
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
}

// Service processes updates on a fixed pool of workers.
//
// Each user is always served by the same worker, which gives three things at
// once: a user's messages are handled one at a time and in order, the number
// of concurrent turns never exceeds Workers, and no goroutine is started per
// update. When a worker's queue is full, new updates for it are refused with
// a "busy" reply instead of piling up.
type Service struct {
	opts     Options
	limiter  *limiter
	queues   []chan telegram.Update
	notices  chan int64
	workers  sync.WaitGroup
	notifier sync.WaitGroup
}

// New starts the workers and returns the Service. Call Close to stop it.
func New(opts Options) *Service {
	s := &Service{
		opts:    opts,
		limiter: newLimiter(opts.RatePerMinute),
		queues:  make([]chan telegram.Update, opts.Workers),
		notices: make(chan int64, noticeQueueSize),
	}
	for i := range s.queues {
		queue := make(chan telegram.Update, opts.QueueSize)
		s.queues[i] = queue
		s.workers.Go(func() { s.work(queue) })
	}
	s.notifier.Go(s.sendBusyNotices)
	return s
}

// Handle accepts an update for processing and returns immediately. Updates
// that are not a private message from a person are ignored. Handle must not
// be called after Close.
func (s *Service) Handle(update telegram.Update) {
	if !isFromPerson(update.Message) {
		return
	}
	userID := update.Message.From.ID
	queue := s.queues[uint64(userID)%uint64(len(s.queues))]
	select {
	case queue <- update:
	default:
		s.opts.Logger.Warn("worker queue full; dropping update", "user_id", userID, "update_id", update.UpdateID)
		s.notifyBusy(update.Message.Chat.ID)
	}
}

// isFromPerson accepts only one-to-one chats with a human. In a private chat
// Telegram guarantees the sender is the account that owns the chat, so
// From.ID is a trustworthy identity for history and permissions.
func isFromPerson(msg *telegram.Message) bool {
	if msg == nil || msg.From == nil || msg.From.IsBot {
		return false
	}
	return msg.Chat.Type == telegram.ChatTypePrivate
}

// notifyBusy queues a "busy" reply without blocking the polling loop. If even
// that queue is full the notice is dropped: the bot is overloaded.
func (s *Service) notifyBusy(chatID int64) {
	select {
	case s.notices <- chatID:
	default:
	}
}

func (s *Service) sendBusyNotices() {
	for chatID := range s.notices {
		s.reply(chatID, textBusy)
	}
}

func (s *Service) work(queue <-chan telegram.Update) {
	for update := range queue {
		s.process(update)
	}
}

// Close stops accepting updates and waits until every queued update has been
// processed. A turn in progress is given up to TurnTimeout to finish.
func (s *Service) Close() {
	for _, queue := range s.queues {
		close(queue)
	}
	s.workers.Wait()
	close(s.notices)
	s.notifier.Wait()
}

// reply sends with its own deadline, separate from the turn's. A turn that
// used up its whole time budget must still be able to tell the user so.
func (s *Service) reply(chatID int64, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if err := s.opts.Sender.SendMessage(ctx, chatID, text); err != nil {
		s.opts.Logger.Error("send message failed", "chat_id", chatID, "error", err)
	}
}
