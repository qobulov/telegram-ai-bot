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

// ClaimMediaSlot notes that chatID gets a sticker or a GIF now and reports
// true, or reports false when it had one within gap: the two share it, so
// that they stay a seasoning. The chat's row is locked while it decides.
func (s *Store) ClaimMediaSlot(ctx context.Context, chatID int64, gap time.Duration) (bool, error) {
	var claimed bool
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
			return nil
		}
		claimed = true
		_, err := tx.Exec(ctx, `UPDATE sticker_sends SET sent_at = now() WHERE chat_id = $1`, chatID)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("storage: claim a media slot: %w", err)
	}
	return claimed, nil
}

// HasStickers reports whether any sticker may be sent at all.
func (s *Store) HasStickers(ctx context.Context) (bool, error) {
	var has bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM bot_stickers WHERE enabled)`).Scan(&has); err != nil {
		return false, fmt.Errorf("storage: any stickers: %w", err)
	}
	return has, nil
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

// ForgetSticker drops a sticker, such as one taken out of the bot's own
// set, so that it is never sent again.
func (s *Store) ForgetSticker(ctx context.Context, fileUniqueID string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM bot_stickers WHERE file_unique_id = $1`, fileUniqueID); err != nil {
		return fmt.Errorf("storage: forget sticker: %w", err)
	}
	return nil
}

// ErrNoStickerSet means the bot has not made its own sticker set yet.
var ErrNoStickerSet = errors.New("storage: no sticker set of the bot's own")

// OwnStickerSetOwner is the user the bot's sticker set name belongs to on
// Telegram, or ErrNoStickerSet when the set was not made.
func (s *Store) OwnStickerSetOwner(ctx context.Context, name string) (int64, error) {
	var ownerID int64
	err := s.pool.QueryRow(ctx, `SELECT owner_id FROM own_sticker_sets WHERE name = $1`, name).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNoStickerSet
	}
	if err != nil {
		return 0, fmt.Errorf("storage: own sticker set: %w", err)
	}
	return ownerID, nil
}

// SaveOwnStickerSet remembers that the set name belongs to ownerID. A set
// already remembered keeps its owner: Telegram never changes it.
func (s *Store) SaveOwnStickerSet(ctx context.Context, name string, ownerID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO own_sticker_sets (name, owner_id) VALUES ($1, $2) ON CONFLICT (name) DO NOTHING`, name, ownerID)
	if err != nil {
		return fmt.Errorf("storage: save own sticker set: %w", err)
	}
	return nil
}

var dailyStickers = dailyKind{table: "sticker_uploads", salt: 0x5354434b} // "STCK"

// ReserveStickerUpload takes one of the user's stickers for the day, as
// ReserveImage takes a picture.
func (s *Store) ReserveStickerUpload(ctx context.Context, userID, chatID int64, since time.Time, limit int) (int64, bool, error) {
	return s.reserveDaily(ctx, dailyStickers, userID, chatID, since, limit)
}

// ReleaseStickerUpload gives back a reservation whose sticker was not
// added.
func (s *Store) ReleaseStickerUpload(ctx context.Context, id int64) error {
	return s.releaseDaily(ctx, dailyStickers, id)
}
