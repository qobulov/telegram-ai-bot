// Package agent runs one conversation turn: it loads the user's history, asks
// the model, executes the tools the model requests, and saves the result.
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

var (
	// ErrDuplicate means the Telegram update was already answered.
	ErrDuplicate = errors.New("agent: update already processed")
	// ErrSilent means the model decided the message needs no reply. Nothing
	// is saved and nothing should be sent.
	ErrSilent = errors.New("agent: the model chose not to reply")
	// ErrToolLoop means the model kept requesting tools instead of answering.
	ErrToolLoop = errors.New("agent: model did not produce an answer")
)

// SilentMarker is the answer the model gives to stay silent.
const SilentMarker = "[silent]"

// silentMarkers are the spellings of SilentMarker that silence an answer
// wherever they appear in it, compared in lower case.
var silentMarkers = []string{SilentMarker, "(silent)"}

// variationSelector is the invisible U+FE0F that models often write after
// an emoji such as ❤ or ⚡. Telegram's list of reactions does not have it.
const variationSelector = "\uFE0F"

const toolLimitResult = `{"ok":false,"error":{"code":"tool_limit","message":"The tool call limit for this message was reached. Answer with the information you already have."}}`

// Store is the conversation persistence the agent needs.
type Store interface {
	History(ctx context.Context, scope storage.Scope, limit int) ([]ai.Message, error)
	SaveTurn(ctx context.Context, turn storage.Turn) (answerID int64, err error)
	UpdateProcessed(ctx context.Context, updateID int64) (bool, error)
}

// Model produces the assistant's next message.
type Model interface {
	Complete(ctx context.Context, req ai.Request) (ai.Message, error)
}

// Tools lists and executes the tools a caller may use.
type Tools interface {
	Definitions(ctx context.Context, callerID int64) []ai.ToolDefinition
	Execute(ctx context.Context, callerID int64, name, args string) string
}

// Options configures an Agent. All fields are required.
type Options struct {
	Store Store
	Model Model
	Tools Tools
	// Persona is the character's instructions, sent with every request.
	Persona string
	// GroupNote is added to Persona for messages from a group.
	GroupNote    string
	MaxHistory   int
	MaxToolCalls int
}

// Agent answers user messages. It is safe for concurrent use as long as
// messages from the same user are not processed at the same time.
type Agent struct {
	opts Options
}

// New returns an Agent.
func New(opts Options) *Agent {
	return &Agent{opts: opts}
}

// Request is one user message to answer.
type Request struct {
	// Scope selects the conversation. It must come from the Telegram update.
	Scope storage.Scope
	// UserID is who sent the message. It decides which tools may run, and
	// must come from the Telegram update too: in a group the scope names no
	// user, so this is the only identity there is.
	UserID     int64
	AuthorName string
	UpdateID   int64
	// MessageID and ReplyToMessageID are stored with the turn.
	MessageID        int64
	ReplyToMessageID int64
	// Text is the user's message as it is stored and replayed in later turns.
	Text string
	// Context is extra material for this turn only, such as recent group
	// messages. It is sent in front of Text and never stored.
	Context string
	// Attachments are images or audio the message carries, sent with Text in
	// this request only. They are never stored: later turns see only the
	// text, which should say what was attached ("[photo]").
	Attachments []ai.ContentPart
	// Group is true for messages from a group or supergroup.
	Group bool
}

// Answer is the assistant's reply. ID identifies the stored answer.
type Answer struct {
	Text string
	// Reaction is the emoji the model chose to react with instead of
	// writing. Text is then empty.
	Reaction string
	ID       int64
}

// Reply answers the user's message and saves the whole turn. Nothing is
// saved when the turn fails.
func (a *Agent) Reply(ctx context.Context, req Request) (Answer, error) {
	processed, err := a.opts.Store.UpdateProcessed(ctx, req.UpdateID)
	if err != nil {
		return Answer{}, err
	}
	if processed {
		return Answer{}, ErrDuplicate
	}
	history, err := a.opts.Store.History(ctx, req.Scope, a.opts.MaxHistory)
	if err != nil {
		return Answer{}, err
	}

	t := &turn{
		req:      req,
		history:  sanitize(history),
		messages: []ai.Message{{Role: ai.RoleUser, Content: req.Text}},
	}
	answer, err := a.run(ctx, t)
	if err != nil {
		return Answer{}, err
	}
	if answer.ID, err = a.save(ctx, t); err != nil {
		return Answer{}, err
	}
	return answer, nil
}

// turn is the state of one Reply call.
type turn struct {
	req       Request
	history   []ai.Message
	messages  []ai.Message // this turn: user message, tool exchanges, answer
	toolsUsed int
}

// run alternates between the model and its tool calls until the model
// answers in text. The loop cannot run away: once MaxToolCalls is reached the
// model is told to stop using tools, and after MaxToolCalls+2 requests the
// turn fails with ErrToolLoop.
func (a *Agent) run(ctx context.Context, t *turn) (Answer, error) {
	definitions := a.opts.Tools.Definitions(ctx, t.req.UserID)
	for range a.opts.MaxToolCalls + 2 {
		reply, err := a.opts.Model.Complete(ctx, ai.Request{
			Messages:    a.prompt(t),
			Tools:       definitions,
			ForbidTools: t.toolsUsed >= a.opts.MaxToolCalls,
		})
		if err != nil {
			return Answer{}, err
		}
		if len(reply.ToolCalls) == 0 {
			return finish(t, reply)
		}
		a.runTools(ctx, t, reply)
	}
	return Answer{}, ErrToolLoop
}

func (a *Agent) prompt(t *turn) []ai.Message {
	messages := make([]ai.Message, 0, 1+len(t.history)+len(t.messages))
	messages = append(messages, ai.Message{Role: ai.RoleSystem, Content: a.systemPrompt(t.req)})
	messages = append(messages, t.history...)
	messages = append(messages, t.messages...)
	// The first message of the turn is the user's. Context and attachments
	// are added to it in this request only; t.messages, which is what gets
	// saved, keeps the plain text.
	question := &messages[1+len(t.history)]
	if t.req.Context != "" {
		question.Content = t.req.Context + "\n\n" + question.Content
	}
	question.Parts = t.req.Attachments
	return messages
}

func (a *Agent) systemPrompt(req Request) string {
	if req.Group {
		return a.opts.Persona + "\n\n" + a.opts.GroupNote
	}
	return a.opts.Persona
}

// finish turns the model's final message into the answer and adds it to the
// turn. A reaction is saved as "<reacted X>", so that later turns see that
// the bot answered and how.
func finish(t *turn, reply ai.Message) (Answer, error) {
	answer := strings.TrimSpace(reply.Content)
	if answer == "" {
		return Answer{}, ai.ErrEmptyResponse
	}
	if isSilent(answer) {
		return Answer{}, ErrSilent
	}
	if emoji, ok := reactionOf(answer); ok {
		// The model wanted no words. Telegram refuses any emoji not on its
		// list, so for any other one silence is the closest it can have.
		emoji = strings.ReplaceAll(emoji, variationSelector, "")
		if !telegram.IsReactionEmoji(emoji) {
			return Answer{}, ErrSilent
		}
		t.messages = append(t.messages, ai.Message{Role: ai.RoleAssistant, Content: "<reacted " + emoji + ">"})
		return Answer{Reaction: emoji}, nil
	}
	t.messages = append(t.messages, ai.Message{Role: ai.RoleAssistant, Content: answer})
	return Answer{Text: answer}, nil
}

// isSilent reports an answer with the silent marker anywhere in it. The
// marker is meant to be the whole answer, but models wrap it in a sentence
// ("Yana bitta gap bo'lsa, [silent].") or change its brackets, and sending
// such an answer would post the marker into the chat.
func isSilent(answer string) bool {
	lower := strings.ToLower(answer)
	for _, marker := range silentMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// reactionOf returns X when the whole answer is "[react X]", ignoring
// decoration around it as in "*[react 🔥]*". It also takes "<reacted X>":
// that is how past reactions look in the history, and models copy it.
// X may be empty or not an emoji at all; finish decides what it is worth.
func reactionOf(answer string) (string, bool) {
	bare := strings.TrimFunc(answer, func(char rune) bool {
		return !strings.ContainsRune("[]<>", char) && !unicode.IsLetter(char) && !unicode.IsDigit(char)
	})
	switch {
	case strings.HasPrefix(bare, "[") && strings.HasSuffix(bare, "]"):
		return argument(bare[1:len(bare)-1], "react")
	case strings.HasPrefix(bare, "<") && strings.HasSuffix(bare, ">"):
		return argument(bare[1:len(bare)-1], "reacted")
	}
	return "", false
}

// argument returns the rest of command when its first word is verb, in any
// case: "🔥" for "react 🔥".
func argument(command, verb string) (string, bool) {
	words := strings.Fields(command)
	if len(words) == 0 || !strings.EqualFold(words[0], verb) {
		return "", false
	}
	return strings.Join(words[1:], " "), true
}

// runTools records the assistant's request and one result per call. Every
// call gets a result, even a refused one, because the API rejects a tool call
// that is left unanswered.
func (a *Agent) runTools(ctx context.Context, t *turn, reply ai.Message) {
	calls := identified(t, reply.ToolCalls)
	t.messages = append(t.messages, ai.Message{Role: ai.RoleAssistant, Content: reply.Content, ToolCalls: calls})
	for _, call := range calls {
		t.messages = append(t.messages, ai.Message{
			Role:       ai.RoleTool,
			ToolCallID: call.ID,
			Content:    a.execute(ctx, t, call),
		})
	}
}

func (a *Agent) execute(ctx context.Context, t *turn, call ai.ToolCall) string {
	t.toolsUsed++
	if t.toolsUsed > a.opts.MaxToolCalls {
		return toolLimitResult
	}
	return a.opts.Tools.Execute(ctx, t.req.UserID, call.Function.Name, call.Function.Arguments)
}

// identified returns the calls with an ID on each. Some models omit the ID,
// and without one a result cannot be matched to its call.
func identified(t *turn, calls []ai.ToolCall) []ai.ToolCall {
	out := make([]ai.ToolCall, len(calls))
	for i, call := range calls {
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d_%d", t.req.UpdateID, len(t.messages)+i)
		}
		if call.Type == "" {
			call.Type = "function"
		}
		out[i] = call
	}
	return out
}

func (a *Agent) save(ctx context.Context, t *turn) (int64, error) {
	answerID, err := a.opts.Store.SaveTurn(ctx, storage.Turn{
		Scope:            t.req.Scope,
		UpdateID:         t.req.UpdateID,
		MessageID:        t.req.MessageID,
		ReplyToMessageID: t.req.ReplyToMessageID,
		AuthorID:         t.req.UserID,
		AuthorName:       t.req.AuthorName,
		Messages:         t.messages,
	})
	if errors.Is(err, storage.ErrDuplicateUpdate) {
		return 0, ErrDuplicate
	}
	return answerID, err
}
