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
}

// Drawing is a picture made during a turn, to be sent with the answer.
type Drawing struct {
	Data     []byte
	MimeType string
}

// Sink collects the pictures drawn during one turn. It is safe for
// concurrent use.
type Sink struct {
	mu       sync.Mutex
	drawings []Drawing
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
	limit := opts.DailyLimit
	if caller.Owner {
		limit = -1
	}
	local := opts.Now().In(opts.Location)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, opts.Location)
	id, granted, err := opts.Quota.ReserveImage(ctx, caller.UserID, caller.ChatID, midnight, limit)
	if err != nil {
		return 0, err
	}
	if !granted {
		return 0, &LimitError{Message: fmt.Sprintf("this person has used their %d pictures for today; more after midnight", opts.DailyLimit)}
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
