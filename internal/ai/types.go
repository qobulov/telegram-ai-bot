// Package ai is a client for OpenAI-compatible Chat Completions APIs, such as
// Gemini's and xAI's.
package ai

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

// ErrTruncated is a reply the model ran out of tokens in before it was done:
// a fragment of words, or of a tool call written out as words, that must not
// be sent. Asking again would end the same way.
var ErrTruncated = errors.New("ai: the reply was cut off at the token limit")

// Message is one chat message. An assistant message may carry ToolCalls; a
// tool message answers one of them through ToolCallID.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	// Parts are images or audio sent along with Content. They are only ever
	// part of a request: they are not stored, and never come back in a reply.
	Parts []ContentPart `json:"-"`
	// FinishReason is why the model stopped, as the provider says, in a
	// reply only: what an empty one is told by.
	FinishReason string `json:"-"`
}

// ContentPart is one piece of a multimodal message.
type ContentPart struct {
	Type       string      `json:"type"`
	Text       string      `json:"text,omitempty"`
	ImageURL   *ImageURL   `json:"image_url,omitempty"`
	InputAudio *InputAudio `json:"input_audio,omitempty"`
}

// ImageURL holds an image, here always inlined as a data URL.
type ImageURL struct {
	URL string `json:"url"`
}

// InputAudio holds base64 audio and its format, such as "ogg".
type InputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

// ImagePart inlines an image of the given MIME type.
func ImagePart(mimeType string, data []byte) ContentPart {
	url := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
	return ContentPart{Type: "image_url", ImageURL: &ImageURL{URL: url}}
}

// PDFMimeType is the type of a PDF inlined by DocumentPart.
const PDFMimeType = "application/pdf"

// DocumentPart inlines a PDF. Gemini reads it, text and pages alike, from
// the same kind of part as an image.
func DocumentPart(data []byte) ContentPart {
	return ImagePart(PDFMimeType, data)
}

// TextPart is text sent alongside a message, such as a file's contents.
func TextPart(text string) ContentPart {
	return ContentPart{Type: "text", Text: text}
}

// IsDocument reports a PDF inlined by DocumentPart.
func (p ContentPart) IsDocument() bool {
	return p.ImageURL != nil && strings.HasPrefix(p.ImageURL.URL, "data:"+PDFMimeType+";")
}

// AudioPart inlines audio in the given format.
func AudioPart(format string, data []byte) ContentPart {
	return ContentPart{Type: "input_audio", InputAudio: &InputAudio{Data: base64.StdEncoding.EncodeToString(data), Format: format}}
}

// MarshalJSON sends a message with Parts in the multimodal form, where
// content is a list of parts with the text first. Without Parts, content
// stays a plain string, which every provider accepts.
func (m Message) MarshalJSON() ([]byte, error) {
	type plain Message // same fields, without this method
	if len(m.Parts) == 0 {
		return json.Marshal(plain(m))
	}
	parts := make([]ContentPart, 0, 1+len(m.Parts))
	if m.Content != "" {
		parts = append(parts, ContentPart{Type: "text", Text: m.Content})
	}
	parts = append(parts, m.Parts...)
	return json.Marshal(struct {
		plain
		Content []ContentPart `json:"content"`
	}{plain: plain(m), Content: parts})
}

// ToolCall is the model's request to run a tool.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
	// ExtraContent is provider data attached to the call, kept verbatim.
	// Gemini 3 puts a thought signature here and rejects the follow-up
	// request unless the call comes back with it.
	ExtraContent json.RawMessage `json:"extra_content,omitempty"`
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

// APIError is a failure reported by the model provider.
type APIError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("model provider: status %d: %s", e.Status, e.Message)
}
