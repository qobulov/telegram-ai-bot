// Package agent runs one conversation turn: it loads the user's history, asks
// the model, executes the tools the model requests, and saves the result.
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/storage"
)

var (
	// ErrDuplicate means the Telegram update was already answered.
	ErrDuplicate = errors.New("agent: update already processed")
	// ErrToolLoop means the model kept requesting tools instead of answering.
	ErrToolLoop = errors.New("agent: model did not produce an answer")
)

const toolLimitResult = `{"ok":false,"error":{"code":"tool_limit","message":"The tool call limit for this message was reached. Answer with the information you already have."}}`

// Store is the conversation persistence the agent needs.
type Store interface {
	History(ctx context.Context, userID int64, limit int) ([]ai.Message, error)
	SaveTurn(ctx context.Context, userID, updateID int64, msgs []ai.Message) error
	UpdateProcessed(ctx context.Context, updateID int64) (bool, error)
}

// Model produces the assistant's next message.
type Model interface {
	Complete(ctx context.Context, req ai.Request) (ai.Message, error)
}

// Tools lists and executes the tools a caller may use.
type Tools interface {
	Definitions(callerID int64) []ai.ToolDefinition
	Execute(ctx context.Context, callerID int64, name, args string) string
}

// Options configures an Agent. All fields are required.
type Options struct {
	Store        Store
	Model        Model
	Tools        Tools
	SystemPrompt string
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

// Reply answers the user's message and saves the whole turn. userID must come
// from the authenticated Telegram update: it selects the history and decides
// which tools may run. Nothing is saved when the turn fails.
func (a *Agent) Reply(ctx context.Context, userID, updateID int64, text string) (string, error) {
	processed, err := a.opts.Store.UpdateProcessed(ctx, updateID)
	if err != nil {
		return "", err
	}
	if processed {
		return "", ErrDuplicate
	}
	history, err := a.opts.Store.History(ctx, userID, a.opts.MaxHistory)
	if err != nil {
		return "", err
	}

	t := &turn{
		userID:   userID,
		updateID: updateID,
		history:  sanitize(history),
		messages: []ai.Message{{Role: ai.RoleUser, Content: text}},
	}
	answer, err := a.run(ctx, t)
	if err != nil {
		return "", err
	}
	if err := a.save(ctx, t); err != nil {
		return "", err
	}
	return answer, nil
}

// turn is the state of one Reply call.
type turn struct {
	userID    int64
	updateID  int64
	history   []ai.Message
	messages  []ai.Message // this turn: user message, tool exchanges, answer
	toolsUsed int
}

// run alternates between the model and its tool calls until the model
// answers in text. The loop cannot run away: once MaxToolCalls is reached the
// model is told to stop using tools, and after MaxToolCalls+2 requests the
// turn fails with ErrToolLoop.
func (a *Agent) run(ctx context.Context, t *turn) (string, error) {
	definitions := a.opts.Tools.Definitions(t.userID)
	for range a.opts.MaxToolCalls + 2 {
		reply, err := a.opts.Model.Complete(ctx, ai.Request{
			Messages:    a.prompt(t),
			Tools:       definitions,
			ForbidTools: t.toolsUsed >= a.opts.MaxToolCalls,
		})
		if err != nil {
			return "", err
		}
		if len(reply.ToolCalls) == 0 {
			return finish(t, reply)
		}
		a.runTools(ctx, t, reply)
	}
	return "", ErrToolLoop
}

func (a *Agent) prompt(t *turn) []ai.Message {
	messages := make([]ai.Message, 0, 1+len(t.history)+len(t.messages))
	messages = append(messages, ai.Message{Role: ai.RoleSystem, Content: a.opts.SystemPrompt})
	messages = append(messages, t.history...)
	return append(messages, t.messages...)
}

func finish(t *turn, reply ai.Message) (string, error) {
	answer := strings.TrimSpace(reply.Content)
	if answer == "" {
		return "", ai.ErrEmptyResponse
	}
	t.messages = append(t.messages, ai.Message{Role: ai.RoleAssistant, Content: answer})
	return answer, nil
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
	return a.opts.Tools.Execute(ctx, t.userID, call.Function.Name, call.Function.Arguments)
}

// identified returns the calls with an ID on each. Some models omit the ID,
// and without one a result cannot be matched to its call.
func identified(t *turn, calls []ai.ToolCall) []ai.ToolCall {
	out := make([]ai.ToolCall, len(calls))
	for i, call := range calls {
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d_%d", t.updateID, len(t.messages)+i)
		}
		if call.Type == "" {
			call.Type = "function"
		}
		out[i] = call
	}
	return out
}

func (a *Agent) save(ctx context.Context, t *turn) error {
	err := a.opts.Store.SaveTurn(ctx, t.userID, t.updateID, t.messages)
	if errors.Is(err, storage.ErrDuplicateUpdate) {
		return ErrDuplicate
	}
	return err
}
