package storage

import (
	"context"
	"fmt"

	"telegram-ai-bot/internal/ai"
)

// RecordModelUsage saves what one model request cost.
func (s *Store) RecordModelUsage(ctx context.Context, u ai.Usage) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO model_usage (purpose, model, chat_id, user_id, prompt_tokens, cached_tokens, output_tokens, reasoning_tokens)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		u.Purpose, u.Model, u.ChatID, u.UserID, u.PromptTokens, u.CachedTokens, u.OutputTokens, u.ReasoningTokens)
	if err != nil {
		return fmt.Errorf("storage: record model usage: %w", err)
	}
	return nil
}
