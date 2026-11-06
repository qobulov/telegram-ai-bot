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
	// ReasoningEffort is how hard a thinking model thinks before it
	// answers, such as "low"; "" leaves it to the model. Thinking is paid
	// as output and takes from MaxTokens.
	ReasoningEffort string
	HTTPClient      *http.Client
	Logger          *slog.Logger
}

// Client talks to the model provider. It is safe for concurrent use.
type Client struct {
	opts Options
	now  func() time.Time
	// outOfQuota holds, per model, until when it is skipped because its
	// quota ran out, the provider does not have it or it did not answer in
	// time: trying it on every turn only adds a wasted request or wait.
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

// completeWithRetry goes through the models in order, and when every one
// failed, waits and goes round again while attempts and time remain. Models
// found out of quota or missing are left out of later rounds.
func (c *Client) completeWithRetry(ctx context.Context, req Request, withTools bool) (Message, error) {
	models := c.models()
	next := 0
	for attempt := 1; ; attempt++ {
		model := models[next]
		body, err := json.Marshal(c.payload(req, model, withTools))
		if err != nil {
			return Message{}, fmt.Errorf("encode request: %w", err)
		}
		msg, used, err := c.attempt(ctx, body)
		if err == nil {
			c.logUsage(model, used)
		}
		c.noteUnusable(ctx, model, err)
		if err == nil || attempt >= c.opts.MaxAttempts || !c.isRetryable(ctx, err) {
			return msg, err
		}
		next++
		if next < len(models) {
			// Another model is not slowed by this one's load: no need to wait.
			c.opts.Logger.Warn("model unavailable; trying another", "model", model, "next", models[next], "error", err)
			continue
		}
		models, next = c.models(), 0
		if !c.timeForAnotherRound(ctx) {
			return msg, err
		}
		if err := sleep(ctx, c.retryDelay(attempt, err)); err != nil {
			return Message{}, err
		}
	}
}

// noteUnusable skips a model for a while when its quota ran out, the
// provider does not have it, or it did not answer within AttemptTimeout
// while the caller still had time.
func (c *Client) noteUnusable(ctx context.Context, model string, err error) {
	switch {
	case isOutOfQuota(err):
		c.outOfQuota.Store(model, c.now().Add(quotaCooldown))
	case isModelNotFound(err):
		c.outOfQuota.Store(model, c.now().Add(missingModelCooldown))
	case ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded):
		c.outOfQuota.Store(model, c.now().Add(slowModelCooldown))
	}
}

func (c *Client) isRetryable(ctx context.Context, err error) bool {
	// A missing model is worth retrying only on another one.
	if isModelNotFound(err) {
		return len(c.opts.FallbackModels) > 0 && ctx.Err() == nil
	}
	return isRetryable(ctx, err)
}

// timeForAnotherRound reports whether a new round of attempts can finish
// before the caller's deadline.
func (c *Client) timeForAnotherRound(ctx context.Context) bool {
	deadline, ok := ctx.Deadline()
	if !ok || c.opts.AttemptTimeout <= 0 {
		return true
	}
	return time.Until(deadline) > c.opts.AttemptTimeout
}

const (
	// quotaCooldown is how long a model out of quota is skipped.
	quotaCooldown = 15 * time.Minute
	// missingModelCooldown is how long a model the provider does not have
	// is skipped.
	missingModelCooldown = time.Hour
	// slowModelCooldown is how long a model that did not answer in time is
	// skipped.
	slowModelCooldown = 10 * time.Minute
)

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

func isModelNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// isOutOfQuota reports a 429 that is about the quota, such as Gemini's
// daily free limit, rather than a short burst of load.
func isOutOfQuota(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusTooManyRequests &&
		strings.Contains(strings.ToLower(apiErr.Message), "quota")
}

// attempt sends the request once, within AttemptTimeout.
func (c *Client) attempt(ctx context.Context, body []byte) (Message, usage, error) {
	if c.opts.AttemptTimeout <= 0 {
		return c.send(ctx, body)
	}
	ctx, cancel := context.WithTimeout(ctx, c.opts.AttemptTimeout)
	defer cancel()
	return c.send(ctx, body)
}

// usage is what a request cost, as the provider counts it. Cached prompt
// tokens are a part of the prompt's, paid at a fraction of the price.
type usage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// logUsage logs a request's tokens, so that what the bot costs can be read
// from the logs.
func (c *Client) logUsage(model string, u usage) {
	c.opts.Logger.Info("model usage", "model", model,
		"prompt_tokens", u.PromptTokens, "cached_tokens", u.PromptTokensDetails.CachedTokens,
		"completion_tokens", u.CompletionTokens, "reasoning_tokens", u.reasoning())
}

// reasoning is the thinking tokens: as the provider names them, or, as
// Gemini counts them, what the total has beyond the prompt and the answer.
func (u usage) reasoning() int {
	if u.CompletionTokensDetails.ReasoningTokens > 0 {
		return u.CompletionTokensDetails.ReasoningTokens
	}
	return max(0, u.TotalTokens-u.PromptTokens-u.CompletionTokens)
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
	Model           string           `json:"model"`
	Messages        []Message        `json:"messages"`
	MaxTokens       int              `json:"max_tokens,omitempty"`
	ReasoningEffort string           `json:"reasoning_effort,omitempty"`
	Tools           []ToolDefinition `json:"tools,omitempty"`
	ToolChoice      string           `json:"tool_choice,omitempty"`
}

func (c *Client) payload(req Request, model string, withTools bool) chatRequest {
	payload := chatRequest{
		Model:           model,
		Messages:        req.Messages,
		MaxTokens:       c.opts.MaxTokens,
		ReasoningEffort: c.opts.ReasoningEffort,
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
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *errorBody `json:"error"`
}

// errorBody is the provider's error object. Code is a number for HTTP-like
// failures and a string for some provider errors, so it is decoded loosely.
type errorBody struct {
	Message string `json:"message"`
	Code    any    `json:"code"`
}

func (c *Client) send(ctx context.Context, body []byte) (Message, usage, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.opts.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, usage{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.opts.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.opts.HTTPClient.Do(httpReq)
	if err != nil {
		return Message{}, usage{}, fmt.Errorf("model provider request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Message{}, usage{}, fmt.Errorf("read model provider response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Message{}, usage{}, statusError(resp, raw)
	}
	var counted struct {
		Usage usage `json:"usage"`
	}
	_ = json.Unmarshal(raw, &counted)
	msg, err := decodeReply(raw)
	return msg, counted.Usage, err
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

// errorMessage finds the provider's explanation in an error body. Most
// providers send {"error":{...}}; Gemini's OpenAI-compatible endpoint sometimes sends
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
	if parsed.Choices[0].FinishReason == "length" && len(msg.ToolCalls) == 0 {
		return Message{}, ErrTruncated
	}
	msg.Role = RoleAssistant
	msg.FinishReason = parsed.Choices[0].FinishReason
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
	return !errors.Is(err, ErrEmptyResponse) && !errors.Is(err, ErrTruncated) && !isDecodeError(err)
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
