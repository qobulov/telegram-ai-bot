// Package ai is a client for the OpenRouter Chat Completions API.
package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Chat message roles defined by the Chat Completions API.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// ErrEmptyResponse means the provider answered without any usable message.
var ErrEmptyResponse = errors.New("ai: empty response")

// Message is one chat message. An assistant message may carry ToolCalls; a
// tool message answers one of them through ToolCallID.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is the model's request to run a tool.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall names the tool and carries its arguments as a JSON string,
// exactly as the model produced it. It may be malformed.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDefinition describes a tool the model may call.
type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition is the tool's name, purpose and JSON Schema for arguments.
type FunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Request is one completion request.
type Request struct {
	Messages []Message
	Tools    []ToolDefinition
	// ForbidTools tells the model it must answer in text, which ends a tool loop.
	ForbidTools bool
}

// APIError is a failure reported by OpenRouter or the upstream provider.
type APIError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("openrouter: status %d: %s", e.Status, e.Message)
}
