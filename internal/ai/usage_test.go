package ai

import (
	"context"
	"net/http"
	"testing"
)

// Each request that succeeds is recorded once, with the client's purpose,
// the caller from the context and the provider's counts; a failed one is not.
func TestCompleteRecordsUsage(t *testing.T) {
	client, _ := newTestClient(t,
		reply{status: http.StatusServiceUnavailable, body: `{"error":{"message":"busy"}}`},
		reply{status: http.StatusOK, body: `{"choices":[{"message":{"role":"assistant","content":"Salom!"}}],
			"usage":{"prompt_tokens":1200,"completion_tokens":40,"total_tokens":1300,"prompt_tokens_details":{"cached_tokens":1000}}}`},
	)
	var got []Usage
	client.opts.Purpose = "chat"
	client.opts.Record = func(_ context.Context, u Usage) { got = append(got, u) }

	ctx := WithCaller(context.Background(), Caller{ChatID: -100, UserID: 7})
	if _, err := client.Complete(ctx, userRequest()); err != nil {
		t.Fatal(err)
	}
	want := Usage{Purpose: "chat", Model: "test/model", ChatID: -100, UserID: 7,
		PromptTokens: 1200, CachedTokens: 1000, OutputTokens: 40, ReasoningTokens: 60}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("recorded %+v, want [%+v]", got, want)
	}
}
