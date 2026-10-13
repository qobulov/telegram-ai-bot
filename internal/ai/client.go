package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultMaxAttempts = 3
	defaultBaseDelay   = 500 * time.Millisecond
	maxResponseBytes   = 1 << 20
	maxErrorMessageLen = 300
)

// Options configures a Client. BaseURL, APIKey and Model are required.
type Options struct {
	BaseURL     string
	APIKey      string
	Model       string
	MaxTokens   int
	MaxAttempts int
	BaseDelay   time.Duration
	HTTPClient  *http.Client
	Logger      *slog.Logger
}

// Client talks to OpenRouter. It is safe for concurrent use.
type Client struct {
	opts Options
	// noTools is set once the configured model turns out not to support tool
	// calling, so later requests skip the doomed first attempt.
	noTools atomic.Bool
}

// NewClient returns a Client, filling in defaults for optional settings.
func NewClient(opts Options) *Client {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = defaultMaxAttempts
	}
	if opts.BaseDelay <= 0 {
		opts.BaseDelay = defaultBaseDelay
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	return &Client{opts: opts}
}

// Complete sends the conversation and returns the assistant's next message.
// The caller bounds the total time, including retries, through ctx.
func (c *Client) Complete(ctx context.Context, req Request) (Message, error) {
	withTools := len(req.Tools) > 0 && !c.noTools.Load()
	msg, err := c.completeWithRetry(ctx, req, withTools)
	if !withTools || !isToolsUnsupported(err) {
		return msg, err
	}
	c.noTools.Store(true)
	c.opts.Logger.Warn("model does not support tool calling; continuing without tools", "model", c.opts.Model)
	return c.completeWithRetry(ctx, req, false)
}

func (c *Client) completeWithRetry(ctx context.Context, req Request, withTools bool) (Message, error) {
	body, err := json.Marshal(c.payload(req, withTools))
	if err != nil {
		return Message{}, fmt.Errorf("encode request: %w", err)
	}
	for attempt := 1; ; attempt++ {
		msg, err := c.send(ctx, body)
		if err == nil || attempt >= c.opts.MaxAttempts || !isRetryable(ctx, err) {
			return msg, err
		}
		if err := sleep(ctx, c.retryDelay(attempt, err)); err != nil {
			return Message{}, err
		}
	}
}

type chatRequest struct {
	Model      string           `json:"model"`
	Messages   []Message        `json:"messages"`
	MaxTokens  int              `json:"max_tokens,omitempty"`
	Tools      []ToolDefinition `json:"tools,omitempty"`
	ToolChoice string           `json:"tool_choice,omitempty"`
}

func (c *Client) payload(req Request, withTools bool) chatRequest {
	payload := chatRequest{
		Model:     c.opts.Model,
		Messages:  req.Messages,
		MaxTokens: c.opts.MaxTokens,
	}
	if !withTools {
		return payload
	}
	payload.Tools = req.Tools
	if req.ForbidTools {
		payload.ToolChoice = "none"
	}
	return payload
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *errorBody `json:"error"`
}

// errorBody is OpenRouter's error object. Code is a number for HTTP-like
// failures and a string for some provider errors, so it is decoded loosely.
type errorBody struct {
	Message string `json:"message"`
	Code    any    `json:"code"`
}

func (c *Client) send(ctx context.Context, body []byte) (Message, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.opts.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.opts.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.opts.HTTPClient.Do(httpReq)
	if err != nil {
		return Message{}, fmt.Errorf("openrouter request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Message{}, fmt.Errorf("read openrouter response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Message{}, statusError(resp, raw)
	}
	return decodeReply(raw)
}

func statusError(resp *http.Response, raw []byte) *APIError {
	var parsed chatResponse
	message := http.StatusText(resp.StatusCode)
	if json.Unmarshal(raw, &parsed) == nil && parsed.Error != nil && parsed.Error.Message != "" {
		message = parsed.Error.Message
	}
	return &APIError{
		Status:     resp.StatusCode,
		Message:    truncate(message),
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
}

func decodeReply(raw []byte) (Message, error) {
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Message{}, fmt.Errorf("decode openrouter response: %w", err)
	}
	if parsed.Error != nil {
		status, _ := parsed.Error.Code.(float64)
		return Message{}, &APIError{Status: int(status), Message: truncate(parsed.Error.Message)}
	}
	if len(parsed.Choices) == 0 {
		return Message{}, ErrEmptyResponse
	}
	msg := parsed.Choices[0].Message
	msg.Role = RoleAssistant
	return msg, nil
}

func isRetryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusTooManyRequests || apiErr.Status >= 500
	}
	// Anything else that reached here is a transport failure: the request may
	// not have arrived, and a completion has no side effects, so retry.
	return !errors.Is(err, ErrEmptyResponse) && !isDecodeError(err)
}

func isDecodeError(err error) bool {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	return errors.As(err, &syntaxErr) || errors.As(err, &typeErr)
}

func isToolsUnsupported(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Status != http.StatusBadRequest && apiErr.Status != http.StatusNotFound {
		return false
	}
	return strings.Contains(strings.ToLower(apiErr.Message), "tool")
}

// retryDelay prefers the server's Retry-After and otherwise backs off
// exponentially, with jitter so that many clients do not retry in step.
func (c *Client) retryDelay(attempt int, err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	backoff := c.opts.BaseDelay << (attempt - 1)
	return backoff/2 + rand.N(backoff/2+1)
}

func parseRetryAfter(header string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(header); err == nil {
		return max(time.Until(when), 0)
	}
	return 0
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func truncate(message string) string {
	runes := []rune(message)
	if len(runes) <= maxErrorMessageLen {
		return message
	}
	return string(runes[:maxErrorMessageLen]) + "…"
}
