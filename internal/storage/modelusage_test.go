package storage

import (
	"context"
	"testing"

	"telegram-ai-bot/internal/ai"
)

func TestRecordModelUsage(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	u := ai.Usage{Purpose: "chat", Model: "gemini-x", ChatID: -1001234567890, UserID: 8611760780,
		PromptTokens: 12000, CachedTokens: 9000, OutputTokens: 150, ReasoningTokens: 30}
	if err := store.RecordModelUsage(ctx, u); err != nil {
		t.Fatal(err)
	}
	var got ai.Usage
	err := store.pool.QueryRow(ctx, `
		SELECT purpose, model, chat_id, user_id, prompt_tokens, cached_tokens, output_tokens, reasoning_tokens FROM model_usage`).
		Scan(&got.Purpose, &got.Model, &got.ChatID, &got.UserID, &got.PromptTokens, &got.CachedTokens, &got.OutputTokens, &got.ReasoningTokens)
	if err != nil {
		t.Fatal(err)
	}
	if got != u {
		t.Fatalf("saved %+v, want %+v", got, u)
	}
}
