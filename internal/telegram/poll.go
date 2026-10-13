package telegram

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"time"
)

const maxPollBackoff = 30 * time.Second

// Poll receives updates until ctx is cancelled and passes each one to handle,
// in order. handle must return quickly: Poll does not fetch more updates
// while it runs.
//
// Temporary failures are retried with exponential backoff. Poll returns an
// error only when retrying cannot help, which means the bot token is wrong.
// It returns nil when ctx is cancelled.
func (c *Client) Poll(ctx context.Context, handle func(Update)) error {
	var offset int64
	failures := 0
	for {
		updates, err := c.GetUpdates(ctx, offset, int(c.opts.PollTimeout.Seconds()))
		if ctx.Err() != nil {
			return nil
		}
		if isBadToken(err) {
			return err
		}
		if err != nil {
			failures++
			delay := c.pollDelay(failures, err)
			c.opts.Logger.Warn("telegram polling failed; retrying", "error", err, "retry_in", delay.String())
			if sleep(ctx, delay) != nil {
				return nil
			}
			continue
		}
		failures = 0
		for _, update := range updates {
			offset = update.UpdateID + 1
			handle(update)
		}
	}
}

func isBadToken(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Code == http.StatusUnauthorized || apiErr.Code == http.StatusNotFound
}

// pollDelay honours Telegram's retry_after and otherwise doubles the delay on
// each consecutive failure up to maxPollBackoff, with jitter.
func (c *Client) pollDelay(failures int, err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	backoff := maxPollBackoff
	if shift := failures - 1; shift < 10 {
		backoff = min(c.opts.BaseDelay<<shift, maxPollBackoff)
	}
	return backoff/2 + rand.N(backoff/2+1)
}
