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
	"sync"
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
	// AttemptTimeout bounds one attempt. A provider under load sometimes
	// holds a request without answering; past this, the attempt is given up
	// and retried. Zero means only the caller's context limits it.
	AttemptTimeout time.Duration
	// FallbackModels are tried in order when a model is overloaded, over its
	// limits or does not answer: each retry goes to the next one. Every
	// request starts again with Model.
	FallbackModels []string
	HTTPClient     *http.Client
	Logger         *slog.Logger
}

// Client talks to OpenRouter. It is safe for concurrent use.
type Client struct {
	opts Options
	now  func() time.Time
	// outOfQuota holds, per model, until when it is skipped because its
	// quota ran out: retrying it on every turn only adds a wasted request.
	outOfQuota sync.Map
	// noTools is set once the configured model turns out not to support tool
	// calling, so later requests skip the doomed first attempt.
	noTools atomic.Bool
}

// NewClient returns a Client, filling in defaults for optional settings.
func NewClient(opts Options) *Client {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = defaultMaxAttempts
	}
	// Enough attempts to try every model once.
	opts.MaxAttempts = max(opts.MaxAttempts, 1+len(opts.FallbackModels))
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
	return &Client{opts: opts, now: time.Now}
}

// Complete sends the conversation and returns the assistant's next message.
// The caller bounds the total time, including retries, through ctx.
func (c *Client) Complete(ctx context.Context, req Request) (Message, error) {
	withTools := len(req.Tools) > 0 && !c.noTools.Load()
	msg, err := c.completeWithRetry(ctx, req, withTools)
	if !withTools || !isToolsUnsupported(err) {
		return msg, err
	}
	msg, err = c.completeWithRetry(ctx, req, false)
	if err != nil {
		return Message{}, err
	}
	// Remember the result only now that the request is known to work without
	// tools; a failure here may have had nothing to do with tool support.
	if c.noTools.CompareAndSwap(false, true) {
		c.opts.Logger.Warn("model does not support tool calling; continuing without tools", "model", c.opts.Model)
	}
	return msg, nil
}

func (c *Client) completeWithRetry(ctx context.Context, req Request, withTools bool) (Message, error) {
	models := c.models()
	for attempt := 1; ; attempt++ {
		model := models[min(attempt, len(models))-1]
		body, err := json.Marshal(c.payload(req, model, withTools))
		if err != nil {
			return Message{}, fmt.Errorf("encode request: %w", err)
		}
		msg, err := c.attempt(ctx, body)
		if isOutOfQuota(err) {
			c.outOfQuota.Store(model, c.now().Add(quotaCooldown))
		}
		if err == nil || attempt >= c.opts.MaxAttempts || !isRetryable(ctx, err) {
			return msg, err
		}
		next := models[min(attempt+1, len(models))-1]
		if next != model {
			// Another model is not slowed by this one's load: no need to wait.
			c.opts.Logger.Warn("model unavailable; trying another", "model", model, "next", next, "error", err)
			continue
		}
		if err := sleep(ctx, c.retryDelay(attempt, err)); err != nil {
			return Message{}, err
		}
	}
}

// quotaCooldown is how long a model out of quota is skipped.
const quotaCooldown = 15 * time.Minute

// models lists the models to try, in order: the configured one first, then
// each fallback, leaving out those whose quota recently ran out. If that
// leaves none, all are tried.
func (c *Client) models() []string {
	all := append([]string{c.opts.Model}, c.opts.FallbackModels...)
	var usable []string
	for _, model := range all {
		if until, ok := c.outOfQuota.Load(model); ok && c.now().Before(until.(time.Time)) {
			continue
		}
		usable = append(usable, model)
	}
	if len(usable) == 0 {
		return all
	}
	return usable
}

// isOutOfQuota reports a 429 that is about the quota, such as Gemini's
// daily free limit, rather than a short burst of load.
func isOutOfQuota(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusTooManyRequests &&
		strings.Contains(strings.ToLower(apiErr.Message), "quota")
}

// attempt sends the request once, within AttemptTimeout.
func (c *Client) attempt(ctx context.Context, body []byte) (Message, error) {
	if c.opts.AttemptTimeout <= 0 {
		return c.send(ctx, body)
	}
	ctx, cancel := context.WithTimeout(ctx, c.opts.AttemptTimeout)
	defer cancel()
	return c.send(ctx, body)
}

// AudioLabel is the format name sent with audio. Telegram voice is
// OGG/Opus, but Gemini's OpenAI-compatible endpoint refuses the label "ogg"
// ("Valid formats are: [wav, mp3]"). It reads the audio from its bytes
// whatever the label says: an OGG/Opus file sent as "mp3" was understood in
// testing, so the accepted label is used and the bytes are left as they are.
const AudioLabel = "mp3"

const transcribePrompt = "Transcribe this voice message word for word, in the language it is spoken. Reply with the transcript only, without quotes or comments. If there is no speech, reply with nothing."

// Transcribe turns a voice message into text with the configured model.
func (c *Client) Transcribe(ctx context.Context, audio []byte) (string, error) {
	msg, err := c.Complete(ctx, Request{Messages: []Message{{
		Role:    RoleUser,
		Content: transcribePrompt,
		Parts:   []ContentPart{AudioPart(AudioLabel, audio)},
	}}})
	if err != nil {
		return "", fmt.Errorf("transcribe: %w", err)
	}
	return strings.TrimSpace(msg.Content), nil
}

// IsOverloaded reports that the provider refused because it is busy or the
// account is over its limits: 429 or 503. Trying again later may work.
func IsOverloaded(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusTooManyRequests || apiErr.Status == http.StatusServiceUnavailable
}

type chatRequest struct {
	Model      string           `json:"model"`
	Messages   []Message        `json:"messages"`
	MaxTokens  int              `json:"max_tokens,omitempty"`
	Tools      []ToolDefinition `json:"tools,omitempty"`
	ToolChoice string           `json:"tool_choice,omitempty"`
}

func (c *Client) payload(req Request, model string, withTools bool) chatRequest {
	payload := chatRequest{
		Model:     model,
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
		return Message{}, fmt.Errorf("model provider request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Message{}, fmt.Errorf("read model provider response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Message{}, statusError(resp, raw)
	}
	return decodeReply(raw)
}

func statusError(resp *http.Response, raw []byte) *APIError {
	message := http.StatusText(resp.StatusCode)
	if body := errorMessage(raw); body != "" {
		message = body
	}
	return &APIError{
		Status:     resp.StatusCode,
		Message:    truncate(message),
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
}

// errorMessage finds the provider's explanation in an error body. OpenRouter
// sends {"error":{...}}; Gemini's OpenAI-compatible endpoint sometimes sends
// the same object wrapped in an array.
func errorMessage(raw []byte) string {
	var single chatResponse
	if json.Unmarshal(raw, &single) == nil && single.Error != nil {
		return single.Error.Message
	}
	var wrapped []chatResponse
	if json.Unmarshal(raw, &wrapped) == nil && len(wrapped) > 0 && wrapped[0].Error != nil {
		return wrapped[0].Error.Message
	}
	return ""
}

func decodeReply(raw []byte) (Message, error) {
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Message{}, fmt.Errorf("decode model provider response: %w", err)
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
	// For example: "No endpoints found that support tool use." Requiring both
	// words avoids matching unrelated errors that merely mention tools.
	message := strings.ToLower(apiErr.Message)
	return strings.Contains(message, "support") && strings.Contains(message, "tool")
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
