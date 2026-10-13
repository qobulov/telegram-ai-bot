package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultPollTimeout = 30 * time.Second
	defaultBaseDelay   = time.Second
	maxSendAttempts    = 3
	maxResponseBytes   = 4 << 20
)

// Options configures a Client. Token and BaseURL are required.
type Options struct {
	Token   string
	BaseURL string
	// HTTPClient must allow requests to last longer than PollTimeout.
	HTTPClient *http.Client
	// PollTimeout is how long Telegram holds a getUpdates request open.
	PollTimeout time.Duration
	// BaseDelay is the first retry delay; later retries back off from it.
	BaseDelay time.Duration
	Logger    *slog.Logger
}

// Client calls the Telegram Bot API. It is safe for concurrent use.
type Client struct {
	opts Options
}

// NewClient returns a Client, filling in defaults for optional settings.
func NewClient(opts Options) *Client {
	if opts.PollTimeout <= 0 {
		opts.PollTimeout = defaultPollTimeout
	}
	if opts.BaseDelay <= 0 {
		opts.BaseDelay = defaultBaseDelay
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: opts.PollTimeout + 10*time.Second}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	return &Client{opts: opts}
}

type getUpdatesParams struct {
	Offset         int64    `json:"offset"`
	Timeout        int      `json:"timeout"`
	AllowedUpdates []string `json:"allowed_updates"`
}

// GetUpdates long-polls for updates with an ID of at least offset.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]Update, error) {
	params := getUpdatesParams{
		Offset:         offset,
		Timeout:        timeoutSeconds,
		AllowedUpdates: []string{"message"},
	}
	var updates []Update
	if err := c.call(ctx, "getUpdates", params, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

type sendMessageParams struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
}

// SendMessage sends plain text, splitting it into several messages when it
// is longer than Telegram allows. Text is sent without a parse mode so that
// stray Markdown or HTML in a model reply can never make the send fail.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	for _, chunk := range Split(text, MessageLimit) {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		if err := c.sendChunk(ctx, sendMessageParams{ChatID: chatID, Text: chunk}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) sendChunk(ctx context.Context, params sendMessageParams) error {
	for attempt := 1; ; attempt++ {
		err := c.call(ctx, "sendMessage", params, nil)
		if err == nil || attempt >= maxSendAttempts || !isRateLimited(err) {
			return err
		}
		if err := sleep(ctx, c.retryDelay(attempt, err)); err != nil {
			return err
		}
	}
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (c *Client) call(ctx context.Context, method string, params, result any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("telegram %s: encode: %w", method, err)
	}
	endpoint := c.opts.BaseURL + "/bot" + c.opts.Token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram %s: build request", method)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, withoutURL(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("telegram %s: read response: %w", method, err)
	}
	return decode(method, resp.StatusCode, raw, result)
}

func decode(method string, status int, raw []byte, result any) error {
	var parsed apiResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return &APIError{Code: status, Description: "unreadable response from " + method}
	}
	if !parsed.OK {
		return &APIError{
			Code:        parsed.ErrorCode,
			Description: parsed.Description,
			RetryAfter:  time.Duration(parsed.Parameters.RetryAfter) * time.Second,
		}
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(parsed.Result, result); err != nil {
		return fmt.Errorf("telegram %s: decode result: %w", method, err)
	}
	return nil
}

// withoutURL drops the request URL from a transport error. The URL contains
// the bot token, which must never reach logs.
func withoutURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

func isRateLimited(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusTooManyRequests
}

func (c *Client) retryDelay(attempt int, err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	return c.opts.BaseDelay << (attempt - 1)
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
