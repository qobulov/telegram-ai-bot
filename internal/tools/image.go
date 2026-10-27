package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"telegram-ai-bot/internal/ai"
)

const (
	maxImagePrompt = 2000
	// imageTimeout allows for slow image models; the turn's own deadline
	// still applies.
	imageTimeout = 50 * time.Second
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
	// UpdateID is the Telegram update being answered. A broadcast drafted
	// in one update can only be confirmed in another.
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
	// Name is the author's first name and @username.
	Name  string
	IsBot bool
	// Owner is true when the author is one of the bot's owners.
	Owner bool
	// Text is the message's text or caption, at most MaxTargetRunes.
	Text string
	// Answered is, when the message is one of the bot's answers, the member
	// whose message it answered, if known: whom an owner means by "don't
	// answer him" in a reply to that answer.
	Answered *Target
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

// ImageGenerator draws a picture from a prompt.
type ImageGenerator interface {
	Generate(ctx context.Context, prompt string) (ai.Image, error)
}

// ImageQuota counts pictures per user per day.
type ImageQuota interface {
	ReserveImage(ctx context.Context, userID, chatID int64, since time.Time, limit int) (int64, bool, error)
	ReleaseImage(ctx context.Context, id int64) error
}

// ImageOptions configures the generate_image tool.
type ImageOptions struct {
	Generator ImageGenerator
	Quota     ImageQuota
	// DailyLimit is how many pictures a user who is not an owner may get
	// per day.
	DailyLimit int
	// Location decides when a day starts.
	Location *time.Location
	Now      func() time.Time
}

// LimitError reports that the caller has used up their pictures for today.
type LimitError struct {
	Message string
}

func (e *LimitError) Error() string { return e.Message }

type imageArgs struct {
	Prompt string `json:"prompt"`
}

// ImageTool lets the model draw a picture, which is sent to the chat with
// its answer. Owners have no limit; everyone else gets DailyLimit a day.
func ImageTool(opts ImageOptions) Tool {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	return Tool{
		Name:        "generate_image",
		Description: "Draw a picture and send it to the chat with your reply. Use it only when someone asks for a picture, drawing or image. Write the prompt in English and make it detailed.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"prompt": {"type": "string", "description": "What to draw, in English, with style and details."}
			},
			"required": ["prompt"],
			"additionalProperties": false
		}`),
		Timeout: imageTimeout,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return drawImage(ctx, opts, raw)
		},
	}
}

func drawImage(ctx context.Context, opts ImageOptions, raw json.RawMessage) (any, error) {
	var args imageArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(args.Prompt)
	if prompt == "" || len([]rune(prompt)) > maxImagePrompt {
		return nil, &ArgumentError{Message: fmt.Sprintf("prompt must be 1 to %d characters", maxImagePrompt)}
	}
	t, ok := ctx.Value(turnKey{}).(turn)
	if !ok || t.sink == nil {
		return nil, errors.New("pictures cannot be sent from here")
	}
	reservation, err := reserve(ctx, opts, t.caller)
	if err != nil {
		return nil, err
	}
	image, err := opts.Generator.Generate(ctx, prompt)
	if err != nil {
		_ = opts.Quota.ReleaseImage(context.WithoutCancel(ctx), reservation)
		return nil, drawingError(err)
	}
	t.sink.Add(Drawing{Data: image.Data, MimeType: image.MimeType})
	return map[string]any{"drawn": true, "note": "The picture is sent with your reply. Comment on it briefly; do not describe it at length."}, nil
}

func reserve(ctx context.Context, opts ImageOptions, caller Caller) (int64, error) {
	return reserveToday(ctx, caller, daily{limit: opts.DailyLimit, location: opts.Location, now: opts.Now, what: "pictures"}, opts.Quota.ReserveImage)
}

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

// drawingError tells the model what went wrong in words it can pass on.
func drawingError(err error) error {
	switch {
	case ai.IsOverloaded(err):
		return errors.New("drawing is not available right now (the image service is busy or out of quota)")
	case errors.Is(err, ai.ErrNoImage):
		return &ArgumentError{Message: "the image model would not draw this: " + err.Error()}
	default:
		return errors.New("the picture could not be drawn")
	}
}
