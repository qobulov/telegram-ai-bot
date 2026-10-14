package ai

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"
)

// backupReserve is the time kept for the backup when its client has no
// AttemptTimeout of its own.
const backupReserve = 15 * time.Second

// Chain asks a backup provider when every model of the primary one failed,
// so a reply is still possible when, say, Gemini's free quota ran out.
type Chain struct {
	primary, backup *Client
	logger          *slog.Logger
}

// NewChain returns a Chain. It is safe for concurrent use.
func NewChain(primary, backup *Client, logger *slog.Logger) *Chain {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Chain{primary: primary, backup: backup, logger: logger}
}

// Complete asks the primary provider, then the backup if the primary was
// busy, out of quota or did not answer in time. A turn that moved to the
// backup stays there: Gemini rejects tool calls it did not make itself.
func (c *Chain) Complete(ctx context.Context, req Request) (Message, error) {
	if movedToBackup(req) {
		return c.backup.Complete(ctx, forBackup(req))
	}
	primaryCtx, cancel := c.leaveTimeForBackup(ctx)
	msg, err := c.primary.Complete(primaryCtx, req)
	cancel()
	if err == nil || !worthABackup(ctx, err) {
		return msg, err
	}
	c.logger.Warn("main provider unavailable; asking the backup", "model", c.backup.opts.Model, "error", err)
	return c.backup.Complete(ctx, forBackup(req))
}

// leaveTimeForBackup shortens the primary's deadline so the backup still has
// time for one attempt.
func (c *Chain) leaveTimeForBackup(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	reserve := c.backup.opts.AttemptTimeout
	if reserve <= 0 {
		reserve = backupReserve
	}
	if !ok || time.Until(deadline) <= 2*reserve {
		return ctx, func() {}
	}
	return context.WithDeadline(ctx, deadline.Add(-reserve))
}

// worthABackup reports a failure another provider might not have: load,
// quota, a missing model or no answer in time. A request the primary
// rejected as bad would be rejected by the backup too.
func worthABackup(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	return isModelNotFound(err) || errors.Is(err, context.DeadlineExceeded) || isRetryable(context.Background(), err)
}

// movedToBackup reports whether this turn's tool calls were made by the
// backup, which leaves them without Gemini's thought signature.
func movedToBackup(req Request) bool {
	lastUser := -1
	for i, m := range req.Messages {
		if m.Role == RoleUser {
			lastUser = i
		}
	}
	for _, m := range req.Messages[lastUser+1:] {
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 && !signed(m.ToolCalls) {
			return true
		}
	}
	return false
}

func signed(calls []ToolCall) bool {
	for _, call := range calls {
		if len(call.ExtraContent) > 0 {
			return true
		}
	}
	return false
}

// forBackup copies the request without what only Gemini understands: audio,
// whose words are already in the text as a transcript, and the thought
// signatures on tool calls.
func forBackup(req Request) Request {
	messages := make([]Message, len(req.Messages))
	for i, m := range req.Messages {
		var parts []ContentPart
		for _, part := range m.Parts {
			if part.InputAudio == nil {
				parts = append(parts, part)
			}
		}
		m.Parts = parts
		if len(m.ToolCalls) > 0 {
			calls := make([]ToolCall, len(m.ToolCalls))
			for j, call := range m.ToolCalls {
				call.ExtraContent = nil
				calls[j] = call
			}
			m.ToolCalls = calls
		}
		messages[i] = m
	}
	req.Messages = messages
	return req
}
