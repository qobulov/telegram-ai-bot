package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// VoiceTranscript is the transcript kept for a voice file, if there is one.
func (s *Store) VoiceTranscript(ctx context.Context, fileUniqueID string) (string, bool, error) {
	var words string
	err := s.pool.QueryRow(ctx, `SELECT words FROM voice_transcripts WHERE file_unique_id = $1`, fileUniqueID).Scan(&words)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("storage: voice transcript: %w", err)
	}
	return words, true, nil
}

// SaveVoiceTranscript keeps a voice file's transcript. The first one
// stands: two instances transcribing the same file at once keep one.
func (s *Store) SaveVoiceTranscript(ctx context.Context, fileUniqueID, words string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO voice_transcripts (file_unique_id, words) VALUES ($1, $2)
		ON CONFLICT (file_unique_id) DO NOTHING`, fileUniqueID, words)
	if err != nil {
		return fmt.Errorf("storage: save voice transcript: %w", err)
	}
	return nil
}
