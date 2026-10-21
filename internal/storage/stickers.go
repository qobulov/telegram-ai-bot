package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// PackSticker is a sticker of an imported pack.
type PackSticker struct {
	FileUniqueID, FileID, Pack, Emoji string
	// ImageFileID is what shows the sticker as a picture: the sticker itself
	// when it is static, its thumbnail when it moves; "" when it has none.
	ImageFileID string
}

// SaveStickers adds a pack's stickers, keeping what is known of those
// already there, and returns how many were new.
func (s *Store) SaveStickers(ctx context.Context, stickers []PackSticker) (int, error) {
	added := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, st := range stickers {
			var inserted bool
			err := tx.QueryRow(ctx, `
				INSERT INTO bot_stickers (file_unique_id, file_id, pack, emoji, image_file_id)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (file_unique_id) DO UPDATE
				SET file_id = EXCLUDED.file_id, pack = EXCLUDED.pack, emoji = EXCLUDED.emoji, image_file_id = EXCLUDED.image_file_id
				RETURNING (xmax = 0)`, st.FileUniqueID, st.FileID, st.Pack, st.Emoji, st.ImageFileID).Scan(&inserted)
			if err != nil {
				return err
			}
			if inserted {
				added++
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("storage: save stickers: %w", err)
	}
	return added, nil
}

// UnlabelledStickers are a pack's stickers the model has not looked at yet.
func (s *Store) UnlabelledStickers(ctx context.Context, pack string) ([]PackSticker, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT file_unique_id, file_id, pack, emoji, image_file_id
		FROM bot_stickers WHERE pack = $1 AND NOT labelled ORDER BY file_unique_id`, pack)
	if err != nil {
		return nil, fmt.Errorf("storage: unlabelled stickers: %w", err)
	}
	stickers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PackSticker, error) {
		var st PackSticker
		err := row.Scan(&st.FileUniqueID, &st.FileID, &st.Pack, &st.Emoji, &st.ImageFileID)
		return st, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: unlabelled stickers: %w", err)
	}
	return stickers, nil
}

// LabelSticker keeps what the model saw in a sticker: the categories it
// fits and a short description, and whether it is fit to send.
func (s *Store) LabelSticker(ctx context.Context, fileUniqueID string, categories []string, description string, enabled bool) error {
	if categories == nil {
		categories = []string{}
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE bot_stickers SET categories = $2, description = $3, enabled = $4, labelled = true
		WHERE file_unique_id = $1`, fileUniqueID, categories, description, enabled)
	if err != nil {
		return fmt.Errorf("storage: label sticker: %w", err)
	}
	return nil
}

// StickerCount is how far a pack's import has come.
type StickerCount struct {
	Total, Labelled, Enabled int
}

// PackStickers counts a pack's stickers.
func (s *Store) PackStickers(ctx context.Context, pack string) (StickerCount, error) {
	var c StickerCount
	err := s.pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE labelled), count(*) FILTER (WHERE enabled)
		FROM bot_stickers WHERE pack = $1`, pack).Scan(&c.Total, &c.Labelled, &c.Enabled)
	if err != nil {
		return StickerCount{}, fmt.Errorf("storage: count stickers: %w", err)
	}
	return c, nil
}

// ErrNoSticker means no sticker may be sent now: none fits, or the chat had
// one too recently.
var ErrNoSticker = errors.New("storage: no sticker to send")

// PickSticker chooses an enabled sticker of category for chatID, the one
// used longest ago, and notes it as sent; or ErrNoSticker when none fits or
// the chat had a sticker within gap. Two turns at once cannot both pass the
// gap: the chat's row is locked while one decides.
func (s *Store) PickSticker(ctx context.Context, chatID int64, category string, gap time.Duration) (string, error) {
	var fileID string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO sticker_sends (chat_id, sent_at) VALUES ($1, 'epoch') ON CONFLICT DO NOTHING`, chatID); err != nil {
			return err
		}
		var recent bool
		if err := tx.QueryRow(ctx, `
			SELECT sent_at > now() - $2::interval FROM sticker_sends WHERE chat_id = $1 FOR UPDATE`,
			chatID, gap.String()).Scan(&recent); err != nil {
			return err
		}
		if recent {
			return ErrNoSticker
		}
		var uniqueID string
		err := tx.QueryRow(ctx, `
			SELECT file_unique_id, file_id FROM bot_stickers
			WHERE enabled AND categories @> ARRAY[$1]::text[]
			ORDER BY last_used_at NULLS FIRST, random()
			LIMIT 1`, category).Scan(&uniqueID, &fileID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSticker
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE bot_stickers SET used_count = used_count + 1, last_used_at = now() WHERE file_unique_id = $1`, uniqueID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE sticker_sends SET sent_at = now() WHERE chat_id = $1`, chatID)
		return err
	})
	if errors.Is(err, ErrNoSticker) {
		return "", ErrNoSticker
	}
	if err != nil {
		return "", fmt.Errorf("storage: pick sticker: %w", err)
	}
	return fileID, nil
}
