package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"telegram-ai-bot/internal/quizfile"
)

// QuizTest is a test someone sent as a file.
type QuizTest struct {
	ID        int64
	OwnerID   int64
	Title     string
	Questions []quizfile.Question
}

// Quiz run states: made and waiting for "ready", asking, paused after
// questions went unanswered, finished, or stopped before the end.
const (
	QuizReady   = "ready"
	QuizRunning = "running"
	QuizPaused  = "paused"
	QuizDone    = "done"
	QuizStopped = "stopped"
)

// QuizRun is one person going through a part of a test.
type QuizRun struct {
	ID, TestID, UserID, ChatID int64
	// First and Count are the part: questions First to First+Count-1.
	First, Count int
	// Position is how many of them were asked and answered or missed.
	Position                         int
	Correct, Wrong, Skipped          int
	MissedInRow                      int
	PollID                           string
	Status                           string
	StartedAt, FinishedAt, CreatedAt time.Time
}

// ErrNoQuizRun reports a run that does not exist or is not the caller's.
var ErrNoQuizRun = errors.New("storage: no such quiz run")

// SaveQuizTest keeps a test and returns its ID; the same questions sent
// again, by anyone, are the test already kept.
func (s *Store) SaveQuizTest(ctx context.Context, ownerID int64, title string, questions []quizfile.Question) (int64, error) {
	encoded, err := json.Marshal(questions)
	if err != nil {
		return 0, err
	}
	sum := sha256.Sum256(encoded)
	var id int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO quiz_tests (owner_id, title, questions, hash) VALUES ($1, $2, $3, $4)
		ON CONFLICT (hash) DO UPDATE SET hash = EXCLUDED.hash
		RETURNING id`, ownerID, title, encoded, hex.EncodeToString(sum[:])).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("storage: save quiz test: %w", err)
	}
	return id, nil
}

// QuizTest reads a test.
func (s *Store) QuizTest(ctx context.Context, id int64) (QuizTest, error) {
	t := QuizTest{ID: id}
	var encoded []byte
	err := s.pool.QueryRow(ctx, `SELECT owner_id, title, questions FROM quiz_tests WHERE id = $1`, id).Scan(&t.OwnerID, &t.Title, &encoded)
	if err != nil {
		return QuizTest{}, fmt.Errorf("storage: read quiz test: %w", err)
	}
	if err := json.Unmarshal(encoded, &t.Questions); err != nil {
		return QuizTest{}, fmt.Errorf("storage: read quiz test: %w", err)
	}
	return t, nil
}

const quizRunColumns = `id, test_id, user_id, chat_id, first_question, question_count, position, correct, wrong, skipped,
	missed_in_row, coalesce(poll_id, ''), status, coalesce(started_at, 'epoch'), coalesce(finished_at, 'epoch'), created_at`

func scanQuizRun(row pgx.Row) (QuizRun, error) {
	var r QuizRun
	err := row.Scan(&r.ID, &r.TestID, &r.UserID, &r.ChatID, &r.First, &r.Count, &r.Position, &r.Correct, &r.Wrong, &r.Skipped,
		&r.MissedInRow, &r.PollID, &r.Status, &r.StartedAt, &r.FinishedAt, &r.CreatedAt)
	return r, err
}

// quizRunOrNone is the run a statement returned, or false when it touched
// none.
func quizRunOrNone(row pgx.Row, op string) (QuizRun, bool, error) {
	r, err := scanQuizRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return QuizRun{}, false, nil
	}
	if err != nil {
		return QuizRun{}, false, fmt.Errorf("storage: %s: %w", op, err)
	}
	return r, true, nil
}

// NewQuizRun makes a run of a part of a test, waiting for "ready". The
// user's other unfinished runs are stopped: one goes at a time.
func (s *Store) NewQuizRun(ctx context.Context, testID, userID, chatID int64, first, count int) (QuizRun, error) {
	var run QuizRun
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE quiz_runs SET status = 'stopped', poll_id = NULL, finished_at = now()
			WHERE user_id = $1 AND status IN ('ready', 'running', 'paused')`, userID); err != nil {
			return err
		}
		var err error
		run, err = scanQuizRun(tx.QueryRow(ctx, `
			INSERT INTO quiz_runs (test_id, user_id, chat_id, first_question, question_count)
			VALUES ($1, $2, $3, $4, $5) RETURNING `+quizRunColumns, testID, userID, chatID, first, count))
		return err
	})
	if err != nil {
		return QuizRun{}, fmt.Errorf("storage: new quiz run: %w", err)
	}
	return run, nil
}

// StartQuizRun sets the user's ready or paused run going, and reports
// false for one that is not theirs or not waiting.
func (s *Store) StartQuizRun(ctx context.Context, id, userID int64) (QuizRun, bool, error) {
	return quizRunOrNone(s.pool.QueryRow(ctx, `
		UPDATE quiz_runs SET status = 'running', started_at = coalesce(started_at, now()), missed_in_row = 0
		WHERE id = $1 AND user_id = $2 AND status IN ('ready', 'paused')
		RETURNING `+quizRunColumns, id, userID), "start quiz run")
}

// AskedQuizQuestion notes the quiz now waiting for the run's answer, and
// its right option.
func (s *Store) AskedQuizQuestion(ctx context.Context, id int64, pollID string, answer int) error {
	if _, err := s.pool.Exec(ctx, `UPDATE quiz_runs SET poll_id = $2, answer = $3 WHERE id = $1 AND status = 'running'`, id, pollID, answer); err != nil {
		return fmt.Errorf("storage: note quiz question: %w", err)
	}
	return nil
}

// AnswerQuizQuestion scores the quiz with pollID: chosen is the option
// picked, or -1 when time ran out. It reports false when that quiz is not
// waiting for an answer, so that an answer and the end of its time, or
// an update Telegram sent twice, count once.
func (s *Store) AnswerQuizQuestion(ctx context.Context, pollID string, chosen int) (QuizRun, bool, error) {
	return quizRunOrNone(s.pool.QueryRow(ctx, `
		UPDATE quiz_runs SET
			position = position + 1,
			correct = correct + (CASE WHEN $2 >= 0 AND $2 = answer THEN 1 ELSE 0 END),
			wrong = wrong + (CASE WHEN $2 >= 0 AND $2 <> answer THEN 1 ELSE 0 END),
			skipped = skipped + (CASE WHEN $2 < 0 THEN 1 ELSE 0 END),
			missed_in_row = CASE WHEN $2 < 0 THEN missed_in_row + 1 ELSE 0 END,
			poll_id = NULL
		WHERE poll_id = $1 AND status = 'running'
		RETURNING `+quizRunColumns, pollID, chosen), "answer quiz question")
}

// EndQuizRun pauses, finishes or stops the user's run, from the states it
// may leave. It reports false for a run already past them.
func (s *Store) EndQuizRun(ctx context.Context, id, userID int64, status string) (QuizRun, bool, error) {
	from := []string{QuizRunning}
	if status == QuizStopped {
		from = []string{QuizReady, QuizRunning, QuizPaused}
	}
	return quizRunOrNone(s.pool.QueryRow(ctx, `
		UPDATE quiz_runs SET status = $3, poll_id = NULL,
			finished_at = CASE WHEN $3 IN ('done', 'stopped') THEN now() ELSE finished_at END
		WHERE id = $1 AND user_id = $2 AND status = ANY($4)
		RETURNING `+quizRunColumns, id, userID, status, from), "end quiz run")
}

// QuizRun reads one of the user's runs.
func (s *Store) QuizRun(ctx context.Context, id, userID int64) (QuizRun, error) {
	r, err := scanQuizRun(s.pool.QueryRow(ctx, `SELECT `+quizRunColumns+` FROM quiz_runs WHERE id = $1 AND user_id = $2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return QuizRun{}, ErrNoQuizRun
	}
	if err != nil {
		return QuizRun{}, fmt.Errorf("storage: read quiz run: %w", err)
	}
	return r, nil
}

// ActiveQuizRun is the user's unfinished run, if any.
func (s *Store) ActiveQuizRun(ctx context.Context, userID int64) (QuizRun, bool, error) {
	return quizRunOrNone(s.pool.QueryRow(ctx, `
		SELECT `+quizRunColumns+` FROM quiz_runs
		WHERE user_id = $1 AND status IN ('ready', 'running', 'paused')
		ORDER BY id DESC LIMIT 1`, userID), "read active quiz run")
}

// QuizRank is where the user's first finished run of a part stands among
// everyone's first, by right answers and then time, and how many there
// are. Later tries do not move it, as on other quiz bots.
func (s *Store) QuizRank(ctx context.Context, testID int64, first int, userID int64) (rank, total int, err error) {
	err = s.pool.QueryRow(ctx, `
		WITH firsts AS (
			SELECT DISTINCT ON (user_id) user_id, correct, finished_at - started_at AS took
			FROM quiz_runs WHERE test_id = $1 AND first_question = $2 AND status = 'done'
			ORDER BY user_id, finished_at)
		SELECT
			(SELECT count(*) + 1 FROM firsts f WHERE f.correct > me.correct OR (f.correct = me.correct AND f.took < me.took)),
			(SELECT count(*) FROM firsts)
		FROM firsts me WHERE me.user_id = $3`, testID, first, userID).Scan(&rank, &total)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("storage: rank quiz run: %w", err)
	}
	return rank, total, nil
}
