package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Who judged a picture.
const (
	JudgeSightengine = "sightengine"
	JudgeModel       = "model"
)

// PictureVerdict is what the picture guard decided about one picture.
type PictureVerdict struct {
	// FileUniqueID is Telegram's ID of the file, the same in every chat.
	FileUniqueID string
	Unsafe       bool
	// Reason is why it is unsafe, as the notice gives it.
	Reason string
	// Judge is JudgeSightengine or JudgeModel.
	Judge string
}

// PictureVerdict is the verdict kept for a picture, if there is one.
func (s *Store) PictureVerdict(ctx context.Context, fileUniqueID string) (PictureVerdict, bool, error) {
	v := PictureVerdict{FileUniqueID: fileUniqueID}
	err := s.pool.QueryRow(ctx, `
		SELECT unsafe, reason, judge FROM picture_verdicts WHERE file_unique_id = $1`, fileUniqueID).Scan(&v.Unsafe, &v.Reason, &v.Judge)
	if errors.Is(err, pgx.ErrNoRows) {
		return PictureVerdict{}, false, nil
	}
	if err != nil {
		return PictureVerdict{}, false, fmt.Errorf("storage: picture verdict: %w", err)
	}
	return v, true, nil
}

// SavePictureVerdict keeps a picture's verdict. The first one stands: two
// instances judging the same picture at once keep one.
func (s *Store) SavePictureVerdict(ctx context.Context, v PictureVerdict) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO picture_verdicts (file_unique_id, unsafe, reason, judge) VALUES ($1, $2, $3, $4)
		ON CONFLICT (file_unique_id) DO NOTHING`, v.FileUniqueID, v.Unsafe, v.Reason, v.Judge)
	if err != nil {
		return fmt.Errorf("storage: save picture verdict: %w", err)
	}
	return nil
}
