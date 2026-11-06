// Package tools holds the functions the model is allowed to call and the
// registry that validates and runs them.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"telegram-ai-bot/internal/ai"
)

const (
	defaultTimeout = 5 * time.Second
	maxOutputBytes = 4000
)

// Error codes returned to the model inside a failed tool result.
const (
	codeUnknownTool      = "unknown_tool"
	codeForbidden        = "forbidden"
	codeInvalidArguments = "invalid_arguments"
	codeTimeout          = "timeout"
	codeOutputTooLarge   = "output_too_large"
	codeExecutionFailed  = "execution_failed"
	codeDailyLimit       = "daily_limit"
)

// Tool is one function the model may call.
type Tool struct {
	Name        string
	Description string
	// Schema is the JSON Schema for the arguments, sent to the model.
	Schema json.RawMessage
	// AdminOnly restricts the tool to users listed as administrators.
	AdminOnly bool
	// OwnerOnly restricts the tool to the bot's owners, as the turn's
	// Caller says.
	OwnerOnly bool
	// GroupOnly offers the tool only in a group's turns, as the turn's
	// Caller says.
	GroupOnly bool
	// NoBusiness keeps the tool out of the turns answering a customer of an
	// owner's business account: it sends to the chat itself, as the bot
	// rather than as the account, or keeps what it learns of the person for
	// their own chats with the bot, which business chats are kept apart
	// from.
	NoBusiness bool
	// MaxOutputBytes bounds the result. Zero means maxOutputBytes.
	MaxOutputBytes int
	// Timeout bounds one execution. Zero means defaultTimeout.
	Timeout time.Duration
	// Run validates args and does the work. Return an *ArgumentError when
	// the arguments are the problem.
	Run func(ctx context.Context, args json.RawMessage) (any, error)
}

// ArgumentError reports arguments the tool cannot accept. Its message is
// shown to the model so it can correct the call.
type ArgumentError struct {
	Message string
}

func (e *ArgumentError) Error() string { return e.Message }

// DeniedError reports that the caller may not have the tool do this. Its
// message says why in plain words, for the model to pass on.
type DeniedError struct {
	Message string
}

func (e *DeniedError) Error() string { return e.Message }

// internalFailure is all the model is told of a failure on the bot's own
// side.
const internalFailure = "something went wrong on my side; try again later"

// InternalError is a failure whose cause the model must not see, such as an
// unreachable database. The model is told only Message, or internalFailure
// when it is empty: the cause can name hosts, users and databases, and the
// model may repeat what it is told in a group. The registry logs the cause.
type InternalError struct {
	Err     error
	Message string
}

func (e *InternalError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return internalFailure
}

func (e *InternalError) Unwrap() error { return e.Err }

// internal is err as an InternalError, or nil.
func internal(err error) error {
	if err == nil {
		return nil
	}
	return &InternalError{Err: err}
}

// cause is err as the logs need it: with what an InternalError hides.
func cause(err error) error {
	var hidden *InternalError
	if errors.As(err, &hidden) {
		return hidden.Err
	}
	return err
}

// Registry is the fixed set of tools available to the model. Nothing outside
// it can be executed. It is safe for concurrent use.
type Registry struct {
	isAdmin func(userID int64) bool
	logger  *slog.Logger
	tools   []Tool
	byName  map[string]Tool
}

// NewRegistry builds a registry. isAdmin decides who may use admin-only
// tools; logger, slog.Default when nil, gets what InternalErrors hide.
func NewRegistry(isAdmin func(userID int64) bool, logger *slog.Logger, tools ...Tool) (*Registry, error) {
	if logger == nil {
		logger = slog.Default()
	}
	registry := &Registry{isAdmin: isAdmin, logger: logger, byName: make(map[string]Tool, len(tools))}
	for _, tool := range tools {
		if tool.Name == "" || tool.Run == nil {
			return nil, errors.New("tools: every tool needs a name and a Run function")
		}
		if _, exists := registry.byName[tool.Name]; exists {
			return nil, fmt.Errorf("tools: duplicate tool %q", tool.Name)
		}
		if tool.Timeout <= 0 {
			tool.Timeout = defaultTimeout
		}
		if tool.MaxOutputBytes <= 0 {
			tool.MaxOutputBytes = maxOutputBytes
		}
		registry.tools = append(registry.tools, tool)
		registry.byName[tool.Name] = tool
	}
	return registry, nil
}

// Definitions lists the tools the caller is allowed to use, in the format
// the model expects. ctx carries the turn, which says whether the caller is
// an owner.
func (r *Registry) Definitions(ctx context.Context, callerID int64) []ai.ToolDefinition {
	var definitions []ai.ToolDefinition
	for _, tool := range r.tools {
		if r.refusal(ctx, tool, callerID) != "" {
			continue
		}
		definitions = append(definitions, ai.ToolDefinition{
			Type: "function",
			Function: ai.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Schema,
			},
		})
	}
	return definitions
}

// Execute runs a tool on behalf of callerID and returns a JSON result for the
// model: {"ok":true,"result":...} or {"ok":false,"error":{"code","message"}}.
//
// Permission depends only on callerID and the turn in ctx, which the caller
// must take from the authenticated Telegram update. Nothing in name or args
// can grant access.
func (r *Registry) Execute(ctx context.Context, callerID int64, name, args string) string {
	tool, ok := r.byName[name]
	if !ok {
		return failure(codeUnknownTool, fmt.Sprintf("there is no tool named %q", name))
	}
	if reason := r.refusal(ctx, tool, callerID); reason != "" {
		return failure(codeForbidden, reason)
	}
	raw, err := normalizeArgs(args)
	if err != nil {
		return failure(codeInvalidArguments, err.Error())
	}
	result, err := runWithTimeout(ctx, tool, raw)
	if err != nil {
		var hidden *InternalError
		if errors.As(err, &hidden) {
			r.logger.Warn("tool failed", "tool", name, "user_id", callerID, "error", hidden.Err)
		}
		return failure(errorCode(err), err.Error())
	}
	return success(result, tool.MaxOutputBytes)
}

// refusal says why the caller may not use the tool here, or "" when they
// may.
func (r *Registry) refusal(ctx context.Context, tool Tool, callerID int64) string {
	caller, inTurn := CallerFrom(ctx)
	switch {
	case tool.AdminOnly && !r.isAdmin(callerID),
		tool.OwnerOnly && !(inTurn && caller.Owner && caller.UserID == callerID):
		return "this tool is restricted to administrators or owners"
	case tool.GroupOnly && !(inTurn && caller.ChatID < 0):
		return "this tool works only in a group"
	case tool.NoBusiness && inTurn && caller.Business:
		return "this tool does not work in a business chat"
	}
	return ""
}

// normalizeArgs accepts only a JSON object. Models often send an empty string
// for tools without parameters, which is treated as {}.
func normalizeArgs(args string) (json.RawMessage, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return json.RawMessage(`{}`), nil
	}
	if !strings.HasPrefix(args, "{") || !json.Valid([]byte(args)) {
		return nil, errors.New("arguments must be a valid JSON object")
	}
	return json.RawMessage(args), nil
}

type outcome struct {
	result any
	err    error
}

// runWithTimeout enforces the deadline even if the tool ignores its context,
// and turns a panic into an ordinary error.
func runWithTimeout(ctx context.Context, tool Tool, args json.RawMessage) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, tool.Timeout)
	defer cancel()

	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				done <- outcome{err: errors.New("the tool failed unexpectedly")}
			}
		}()
		result, err := tool.Run(ctx, args)
		done <- outcome{result: result, err: err}
	}()

	select {
	case finished := <-done:
		return finished.result, finished.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func errorCode(err error) string {
	var argErr *ArgumentError
	var limitErr *LimitError
	var deniedErr *DeniedError
	switch {
	case errors.As(err, &limitErr):
		return codeDailyLimit
	case errors.As(err, &deniedErr):
		return codeForbidden
	case errors.As(err, &argErr):
		return codeInvalidArguments
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return codeTimeout
	default:
		return codeExecutionFailed
	}
}

type resultEnvelope struct {
	OK     bool       `json:"ok"`
	Result any        `json:"result,omitempty"`
	Error  *toolError `json:"error,omitempty"`
}

type toolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func success(result any, limit int) string {
	encoded, err := json.Marshal(resultEnvelope{OK: true, Result: result})
	if err != nil {
		return failure(codeExecutionFailed, "the tool returned a result that could not be encoded")
	}
	if len(encoded) > limit {
		return failure(codeOutputTooLarge, fmt.Sprintf("the tool result exceeded %d bytes", limit))
	}
	return string(encoded)
}

func failure(code, message string) string {
	encoded, _ := json.Marshal(resultEnvelope{Error: &toolError{Code: code, Message: message}})
	return string(encoded)
}

// decodeArgs unmarshals arguments into a typed struct and rejects properties
// the tool does not define.
func decodeArgs(args json.RawMessage, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return &ArgumentError{Message: "arguments do not match the tool's schema: " + err.Error()}
	}
	return nil
}
