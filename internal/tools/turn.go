package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"telegram-ai-bot/internal/telegram"
)

// Caller is who a turn is for and where its answer goes. Every field comes
// from the Telegram update, never from the model.
type Caller struct {
	UserID int64
	ChatID int64
	// Name is the caller's first name and @username, as the log shows them.
	Name string
	// Owner is true for the bot's owners, who have no daily limits.
	Owner bool
	// Human is false when another bot is asking.
	Human bool
	// UpdateID is the Telegram update being answered.
	UpdateID int64
	// MessageID is the message being answered.
	MessageID int64
	// Target is the message the caller replied to, or nil. Tools that act on
	// a member act on its author: the model cannot name anyone else.
	Target *Target
	// Mentions are the members the caller tagged in the message being
	// answered, or in their own message it replies to, from Telegram's
	// entities; never the bot or the caller.
	Mentions []Mention
	// Words is what the caller wrote in the message being answered, its
	// media described and a voice message's transcript included, and in
	// their own message it replies to: their own words, as opposed to
	// anything quoted from someone else or logged.
	Words string
	// Picture is the first picture the model is shown with the message,
	// its own or the replied one's, for tools that look at it; nil when
	// there is none.
	Picture []byte
	// Sound is the message's voice message, audio, video or round video, or
	// else the replied one's, for tools that listen to it; nil when there is
	// none. Only its file is named: it is downloaded when a tool asks.
	Sound *Sound
	// Forum is true in a supergroup whose messages are kept in topics.
	Forum bool
	// TopicID is the forum topic the request was written in, or else that
	// of the message it replies to; zero in the General topic and outside
	// forums.
	TopicID int64
	// Business is true when the turn answers a customer in a chat of an
	// owner's Telegram Business account: what the bot sends there goes on
	// the account's behalf, and ChatID is the customer's chat with it.
	Business bool
	// Language is the language the caller writes in, one of the Language
	// constants, for what a tool tells them; "" for Uzbek.
	Language string
}

// Sound is a Telegram file with sound in it. Size is zero when Telegram did
// not say.
type Sound struct {
	FileID string
	Size   int64
}

// Mention is a member tagged in a message: by @username, or, for a member
// without one, by a link that carries their ID.
type Mention struct {
	UserID   int64
	Username string
}

// MaxTargetRunes is the most of a Target's text that is kept.
const MaxTargetRunes = 500

// Target is the message a caller replied to, and its author.
type Target struct {
	MessageID int64
	UserID    int64
	// ChannelID is, for a message posted on behalf of a channel, that
	// channel's chat ID; UserID is then the account Telegram posts such
	// messages as, which moderation never acts on.
	ChannelID int64
	// Name is the author's first name and @username, or a channel's title.
	Name  string
	IsBot bool
	// Owner is true when the author is one of the bot's owners.
	Owner bool
	// Text is the message's text or caption, at most MaxTargetRunes.
	Text string
	// Sticker is the message's sticker, or nil: the one an owner means by
	// "take this out of the set".
	Sticker *telegram.Sticker
	// Poll is true for a poll, which create_poll may close if the bot made
	// it; StickerSet is, for a sticker, the pack it is from.
	Poll       bool
	StickerSet string
	// Answered is, when the message is one of the bot's answers, the member
	// whose message it answered, if known: whom an owner means by "don't
	// answer him" in a reply to that answer.
	Answered *Target
	// About is, when the message is the bot's own, the members it tags:
	// whom a moderation notice such as "@spammer was banned" was about.
	About []Mention
}

// Drawing is a picture made during a turn, to be sent with the answer.
type Drawing struct {
	Data     []byte
	MimeType string
}

// Audio is a sound a tool found during a turn, such as a song's preview,
// sent as the answer.
type Audio struct {
	Data             []byte
	Title, Performer string
	// AfterAnswer sends it after the answer, which is then sent too; else
	// it is the answer, sent alone.
	AfterAnswer bool
}

// Document is a file a tool made during a turn, such as a document,
// sent as the answer.
type Document struct {
	Data []byte
	Name string
}

// Sink collects the pictures drawn, the sounds found and the files made
// during one turn, and the answer a tool gave for the
// whole turn. It is safe for concurrent use.
type Sink struct {
	mu        sync.Mutex
	drawings  []Drawing
	audios    []Audio
	documents []Document
	final     string
	buttons   [][]telegram.Button
	// whole is an answer sent whole even to a group (FinishWhole).
	whole bool
}

// SetButtons keeps link buttons to go under the answer.
func (s *Sink) SetButtons(buttons [][]telegram.Button) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buttons = buttons
}

// Buttons returns the link buttons a tool set for the answer.
func (s *Sink) Buttons() [][]telegram.Button {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buttons
}

// AddDocument keeps a file to send as the answer.
func (s *Sink) AddDocument(d Document) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.documents = append(s.documents, d)
}

// TakeDocuments returns the files made so far and empties the sink.
func (s *Sink) TakeDocuments() []Document {
	s.mu.Lock()
	defer s.mu.Unlock()
	documents := s.documents
	s.documents = nil
	return documents
}

// Finish ends the turn with text as its answer, without asking the model
// again: a tool whose result is the whole answer saves that call.
func (s *Sink) Finish(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.final = text
}

// FinishWhole ends the turn as Finish does with an answer long by design,
// such as a patch's notes, which goes whole even to a group, in as many
// messages as it takes, where a group's answers are cut to one.
func (s *Sink) FinishWhole(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.final, s.whole = text, true
}

// Whole reports an answer ended with FinishWhole.
func (s *Sink) Whole() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.whole
}

// Finished is the answer a tool ended the turn with, if one did.
func (s *Sink) Finished() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.final, s.final != ""
}

// AddAudio keeps a sound to send with the answer.
func (s *Sink) AddAudio(a Audio) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audios = append(s.audios, a)
}

// TakeAudios returns the sounds found so far and empties the sink.
func (s *Sink) TakeAudios() []Audio {
	s.mu.Lock()
	defer s.mu.Unlock()
	audios := s.audios
	s.audios = nil
	return audios
}

func (s *Sink) Add(d Drawing) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drawings = append(s.drawings, d)
}

// LastDrawing is the picture drawn last so far, left in the sink.
func (s *Sink) LastDrawing() (Drawing, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.drawings) == 0 {
		return Drawing{}, false
	}
	return s.drawings[len(s.drawings)-1], true
}

// Take returns the pictures drawn so far and empties the sink.
func (s *Sink) Take() []Drawing {
	s.mu.Lock()
	defer s.mu.Unlock()
	drawings := s.drawings
	s.drawings = nil
	return drawings
}

type turnKey struct{}

type turn struct {
	caller Caller
	sink   *Sink
}

// WithTurn attaches the turn's caller and picture sink to ctx, for tools
// that act on the chat rather than only answer the model.
func WithTurn(ctx context.Context, caller Caller, sink *Sink) context.Context {
	return context.WithValue(ctx, turnKey{}, turn{caller: caller, sink: sink})
}

// CallerFrom returns who the turn is for, if ctx belongs to a turn.
func CallerFrom(ctx context.Context) (Caller, bool) {
	t, ok := ctx.Value(turnKey{}).(turn)
	return t.caller, ok
}

// SinkFrom returns the turn's picture sink, if any.
func SinkFrom(ctx context.Context) (*Sink, bool) {
	t, ok := ctx.Value(turnKey{}).(turn)
	return t.sink, ok && t.sink != nil
}

// LimitError reports that the caller has used up a daily allowance, such as
// their stickers for today.
type LimitError struct {
	Message string
}

func (e *LimitError) Error() string { return e.Message }

// daily is a per-user daily limit: limit things a day, counted from
// midnight where location is; owners have none.
type daily struct {
	limit    int
	location *time.Location
	now      func() time.Time
	what     string
}

// reserveToday takes one of the caller's things for today with reserve,
// or reports with a LimitError that they have used them all.
func reserveToday(ctx context.Context, caller Caller, d daily, reserve func(ctx context.Context, userID, chatID int64, since time.Time, limit int) (int64, bool, error)) (int64, error) {
	limit := d.limit
	if caller.Owner {
		limit = -1
	}
	local := d.now().In(d.location)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, d.location)
	id, granted, err := reserve(ctx, caller.UserID, caller.ChatID, midnight, limit)
	if err != nil {
		return 0, internal(err)
	}
	if !granted {
		return 0, &LimitError{Message: fmt.Sprintf("this person has used their %d %s for today; more after midnight", d.limit, d.what)}
	}
	return id, nil
}
