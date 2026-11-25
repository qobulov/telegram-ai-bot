package ai

import "context"

// Usage is what one model request cost, as the provider counts it, and for
// what: the client's Purpose, and the chat and member of the turn when the
// request was made under WithCaller.
type Usage struct {
	Purpose string
	Model   string
	ChatID  int64
	UserID  int64
	// PromptTokens include CachedTokens, which are paid at a fraction.
	PromptTokens int
	CachedTokens int
	// OutputTokens are the answer's; ReasoningTokens, the thinking before
	// it, are paid as output too.
	OutputTokens    int
	ReasoningTokens int
}

// Caller is the chat and member a model request is made for.
type Caller struct {
	ChatID int64
	UserID int64
}

type callerKey struct{}

// WithCaller marks the requests made under ctx as made for caller, so that
// their Usage names the chat and the member.
func WithCaller(ctx context.Context, caller Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, caller)
}

func callerOf(ctx context.Context) Caller {
	caller, _ := ctx.Value(callerKey{}).(Caller)
	return caller
}

// record hands a successful request's usage to Options.Record, if set.
func (c *Client) record(ctx context.Context, model string, u usage) {
	if c.opts.Record == nil {
		return
	}
	caller := callerOf(ctx)
	c.opts.Record(ctx, Usage{
		Purpose:         c.opts.Purpose,
		Model:           model,
		ChatID:          caller.ChatID,
		UserID:          caller.UserID,
		PromptTokens:    u.PromptTokens,
		CachedTokens:    u.PromptTokensDetails.CachedTokens,
		OutputTokens:    u.CompletionTokens,
		ReasoningTokens: u.reasoning(),
	})
}
