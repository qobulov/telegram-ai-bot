package service

import (
	"context"
	"fmt"
	"math/rand/v2"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/doctext"
	"telegram-ai-bot/internal/quizfile"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// A test someone sends as a file, in the format universities write them
// in, is read in code, not by the model, and asked as timed quizzes in
// parts: no question costs a token, and none is reworded.

const (
	// quizPartSize is how many questions a part of a test has.
	quizPartSize = 30
	// quizSeconds is how long each question stays open.
	quizSeconds = 30
	// minQuizQuestions is how many questions make a file a test.
	minQuizQuestions = 5
	// maxMissedInRow pauses a run nobody is answering.
	maxMissedInRow = 2
	// Telegram's limits on a quiz: longer texts are sent as a message,
	// and the quiz then has letters for options.
	maxQuizQuestionRunes = 300
	maxQuizOptionRunes   = 100
	quizTestMemory       = 10 * time.Minute
)

// QuizStore keeps the tests and their runs.
type QuizStore interface {
	SaveQuizTest(ctx context.Context, ownerID int64, title string, questions []quizfile.Question) (int64, error)
	QuizTest(ctx context.Context, id int64) (storage.QuizTest, error)
	NewQuizRun(ctx context.Context, testID, userID, chatID int64, first, count int) (storage.QuizRun, error)
	StartQuizRun(ctx context.Context, id, userID int64) (storage.QuizRun, bool, error)
	AskedQuizQuestion(ctx context.Context, id int64, pollID string, answer int) error
	AnswerQuizQuestion(ctx context.Context, pollID string, chosen int) (storage.QuizRun, bool, error)
	EndQuizRun(ctx context.Context, id, userID int64, status string) (storage.QuizRun, bool, error)
	ActiveQuizRun(ctx context.Context, userID int64) (storage.QuizRun, bool, error)
	QuizRank(ctx context.Context, testID int64, first int, userID int64) (rank, total int, err error)
}

// QuizPoller sends a timed quiz question and returns its poll's ID.
type QuizPoller interface {
	SendQuizQuestion(ctx context.Context, poll telegram.OutgoingPoll) (string, error)
}

// quizzesOn reports whether tests can be run here.
func (s *Service) quizzesOn() bool {
	return s.opts.Quizzes != nil && s.opts.QuizPolls != nil && s.opts.Media != nil
}

// quizFromFile answers a test file in a private chat with its parts to
// choose from, and reports false for any other message, which goes on to
// the model.
func (s *Service) quizFromFile(ctx context.Context, in incoming) (outcome, bool) {
	doc := in.msg.Document
	if in.group || !s.quizzesOn() || doc == nil {
		return outcome{}, false
	}
	kind := doctext.Kind(doc.FileName, doc.MimeType)
	if kind == "" || doc.FileSize > s.opts.MaxMediaBytes {
		return outcome{}, false
	}
	data, err := s.opts.Media.DownloadFile(ctx, doc.FileID, s.opts.MaxMediaBytes)
	if err != nil {
		s.opts.Logger.Warn("could not download a file to look for a test", "user_id", in.userID, "error", err)
		return outcome{}, false
	}
	text, err := doctext.Extract(kind, data)
	if err != nil {
		s.opts.Logger.Warn("could not read a file to look for a test", "user_id", in.userID, "kind", kind, "error", err)
		return outcome{}, false
	}
	questions, broken := quizfile.Parse(text)
	if len(questions) < minQuizQuestions {
		return outcome{}, false
	}
	title := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(doc.FileName), path.Ext(doc.FileName)))
	if title == "" {
		title = textQuizUntitled.to(in)
	}
	testID, err := s.opts.Quizzes.SaveQuizTest(ctx, in.userID, title, questions)
	if err != nil {
		s.opts.Logger.Error("could not keep a test", "user_id", in.userID, "error", err)
		return say(textQuizFailed.to(in)), true
	}
	s.opts.Logger.Info("test read from a file", "user_id", in.userID, "test_id", testID, "questions", len(questions), "broken", broken)
	text = fmt.Sprintf(textQuizParts.to(in), title, len(questions), quizParts(len(questions)), quizPartSize)
	if broken > 0 {
		text += "\n\n" + fmt.Sprintf(textQuizBroken.to(in), broken)
	}
	return outcome{text: text, buttons: partButtons(in, testID, len(questions))}, true
}

// quizParts is how many parts a test of n questions has.
func quizParts(n int) int {
	return (n + quizPartSize - 1) / quizPartSize
}

// partButtons are a button for each part, two to a row.
func partButtons(in incoming, testID int64, questions int) [][]telegram.Button {
	var rows [][]telegram.Button
	for part := range quizParts(questions) {
		first, last := part*quizPartSize+1, min((part+1)*quizPartSize, questions)
		button := telegram.Button{Text: fmt.Sprintf(textQuizPartButton.to(in), part+1, first, last), Data: fmt.Sprintf("qp:%d:%d", testID, part)}
		if len(rows) == 0 || len(rows[len(rows)-1]) == 2 {
			rows = append(rows, nil)
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], button)
	}
	return rows
}

// isQuizButton reports a test button's data.
func isQuizButton(data string) bool {
	return strings.HasPrefix(data, "qp:") || strings.HasPrefix(data, "qs:")
}

// pressedQuiz handles the test buttons: a part chosen ("qp:test:part"),
// and ready or go on ("qs:run").
func (s *Service) pressedQuiz(q *telegram.CallbackQuery) {
	if !s.quizzesOn() || s.opts.Buttons == nil || q.From == nil || q.Message == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), quizSeconds*time.Second)
	defer cancel()
	in := incoming{
		msg:    &telegram.Message{MessageID: q.Message.MessageID, From: q.From, Chat: q.Message.Chat},
		scope:  storage.Scope{ChatID: q.Message.Chat.ID, UserID: q.From.ID},
		userID: q.From.ID,
	}
	fields := strings.Split(q.Data, ":")
	var notice string
	switch {
	case fields[0] == "qp" && len(fields) == 3:
		notice = s.chooseQuizPart(ctx, in, fields[1], fields[2])
	case fields[0] == "qs" && len(fields) == 2:
		notice = s.startQuizRun(ctx, in, fields[1])
	}
	if err := s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, notice, notice != ""); err != nil {
		s.opts.Logger.Warn("could not answer a test button", "user_id", in.userID, "error", err)
	}
}

// chooseQuizPart makes a run of the part and asks whether they are ready.
// It returns what to say in a box when it cannot.
func (s *Service) chooseQuizPart(ctx context.Context, in incoming, testField, partField string) string {
	testID, err1 := strconv.ParseInt(testField, 10, 64)
	part, err2 := strconv.Atoi(partField)
	if err1 != nil || err2 != nil || part < 0 {
		return textQuizGone.to(in)
	}
	test, err := s.quizTest(ctx, testID)
	if err != nil || part*quizPartSize >= len(test.Questions) {
		return textQuizGone.to(in)
	}
	first := part * quizPartSize
	count := min(quizPartSize, len(test.Questions)-first)
	run, err := s.opts.Quizzes.NewQuizRun(ctx, testID, in.userID, in.scope.ChatID, first, count)
	if err != nil {
		s.opts.Logger.Error("could not make a test run", "user_id", in.userID, "error", err)
		return textQuizFailed.to(in)
	}
	text := fmt.Sprintf(textQuizReady.to(in), test.Title, part+1, count, quizSeconds)
	s.sendQuizMessage(in, text, [][]telegram.Button{{{Text: textQuizReadyButton.to(in), Data: fmt.Sprintf("qs:%d", run.ID)}}})
	return ""
}

// startQuizRun sets a ready or paused run going with its next question.
func (s *Service) startQuizRun(ctx context.Context, in incoming, runField string) string {
	runID, err := strconv.ParseInt(runField, 10, 64)
	if err != nil {
		return textQuizGone.to(in)
	}
	run, ok, err := s.opts.Quizzes.StartQuizRun(ctx, runID, in.userID)
	if err != nil {
		s.opts.Logger.Error("could not start a test run", "user_id", in.userID, "error", err)
		return textQuizFailed.to(in)
	}
	if !ok {
		return textQuizGone.to(in)
	}
	s.nextQuizStep(ctx, in, run)
	return ""
}

// quizPollUpdate scores a test question: answered, or closed with no
// answer when its time ran out; then the run goes on. It reports whether
// the update was about a poll.
func (s *Service) quizPollUpdate(update telegram.Update) bool {
	var pollID string
	chosen := -1
	var from *telegram.User
	switch {
	case update.PollAnswer != nil:
		pollID, from = update.PollAnswer.PollID, update.PollAnswer.User
		if len(update.PollAnswer.OptionIDs) > 0 {
			chosen = update.PollAnswer.OptionIDs[0]
		}
	case update.Poll != nil && update.Poll.IsClosed:
		pollID = update.Poll.ID
	case update.Poll != nil:
		return true // a new state of an open poll: nothing to do
	default:
		return false
	}
	if !s.quizzesOn() || pollID == "" {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), quizSeconds*time.Second)
	defer cancel()
	run, ok, err := s.opts.Quizzes.AnswerQuizQuestion(ctx, pollID, chosen)
	if err != nil {
		s.opts.Logger.Error("could not score a test question", "poll_id", pollID, "error", err)
		return true
	}
	if !ok {
		return true // a poll of no run, or one already scored
	}
	if from == nil {
		from = &telegram.User{ID: run.UserID}
	}
	in := incoming{
		msg:    &telegram.Message{From: from, Chat: telegram.Chat{ID: run.ChatID, Type: telegram.ChatTypePrivate}},
		scope:  storage.Scope{ChatID: run.ChatID, UserID: run.UserID},
		userID: run.UserID,
	}
	s.nextQuizStep(ctx, in, run)
	return true
}

// nextQuizStep asks the run's next question, pauses a run nobody answers,
// or finishes one with its results.
func (s *Service) nextQuizStep(ctx context.Context, in incoming, run storage.QuizRun) {
	switch {
	case run.Position >= run.Count:
		s.finishQuizRun(ctx, in, run, storage.QuizDone)
	case run.MissedInRow >= maxMissedInRow:
		s.pauseQuizRun(ctx, in, run)
	default:
		s.askQuizQuestion(ctx, in, run)
	}
}

func (s *Service) pauseQuizRun(ctx context.Context, in incoming, run storage.QuizRun) {
	if _, ok, err := s.opts.Quizzes.EndQuizRun(ctx, run.ID, run.UserID, storage.QuizPaused); err != nil || !ok {
		return
	}
	s.sendQuizMessage(in, textQuizPaused.to(in), [][]telegram.Button{{{Text: textQuizResumeButton.to(in), Data: fmt.Sprintf("qs:%d", run.ID)}}})
}

// askQuizQuestion sends the run's next question as a timed quiz, its
// options shuffled: in the files the right one is often first.
func (s *Service) askQuizQuestion(ctx context.Context, in incoming, run storage.QuizRun) {
	test, err := s.quizTest(ctx, run.TestID)
	if err != nil || run.First+run.Position >= len(test.Questions) {
		s.opts.Logger.Error("could not read a test to ask its question", "run_id", run.ID, "error", err)
		return
	}
	poll, text := quizQuestionPoll(in, run, test.Questions[run.First+run.Position])
	if text != "" {
		s.sendQuizMessage(in, text, nil)
	}
	pollID, err := s.opts.QuizPolls.SendQuizQuestion(ctx, poll)
	if err != nil {
		s.opts.Logger.Error("could not send a test question", "run_id", run.ID, "error", err)
		s.pauseQuizRun(ctx, in, run)
		return
	}
	if err := s.opts.Quizzes.AskedQuizQuestion(ctx, run.ID, pollID, poll.Correct); err != nil {
		s.opts.Logger.Error("could not note a test question", "run_id", run.ID, "error", err)
	}
}

// quizQuestionPoll is the quiz for a question, its options shuffled, and,
// for a question too long for a quiz, the message to send before it with
// the text and lettered options, the quiz then having the letters.
func quizQuestionPoll(in incoming, run storage.QuizRun, q quizfile.Question) (telegram.OutgoingPoll, string) {
	order := rand.Perm(len(q.Options))
	options := make([]string, len(order))
	answer := 0
	for i, from := range order {
		options[i] = q.Options[from]
		if from == q.Correct {
			answer = i
		}
	}
	number := fmt.Sprintf("[%d/%d] ", run.Position+1, run.Count)
	poll := telegram.OutgoingPoll{ChatID: run.ChatID, Question: number + q.Text, Options: options, Quiz: true, Correct: answer, OpenPeriod: quizSeconds}
	if fitsAQuiz(poll.Question, options) {
		return poll, ""
	}
	var b strings.Builder
	b.WriteString(number + q.Text + "\n")
	letters := make([]string, len(options))
	for i, option := range options {
		letters[i] = string(rune('A' + i))
		fmt.Fprintf(&b, "\n%s) %s", letters[i], option)
	}
	poll.Question, poll.Options = number+textQuizPickLetter.to(in), letters
	return poll, b.String()
}

func fitsAQuiz(question string, options []string) bool {
	if utf8.RuneCountInString(question) > maxQuizQuestionRunes {
		return false
	}
	for _, option := range options {
		if utf8.RuneCountInString(option) > maxQuizOptionRunes {
			return false
		}
	}
	return true
}

// finishQuizRun ends a run as done or stopped and sends its results, with
// buttons to try the part again and to go to the next one.
func (s *Service) finishQuizRun(ctx context.Context, in incoming, run storage.QuizRun, status string) {
	ended, ok, err := s.opts.Quizzes.EndQuizRun(ctx, run.ID, run.UserID, status)
	if err != nil || !ok {
		return
	}
	test, err := s.quizTest(ctx, ended.TestID)
	if err != nil {
		return
	}
	part := ended.First / quizPartSize
	took := time.Duration(0)
	if !ended.StartedAt.IsZero() && ended.FinishedAt.After(ended.StartedAt) {
		took = ended.FinishedAt.Sub(ended.StartedAt).Round(time.Second)
	}
	text := fmt.Sprintf(textQuizResults.to(in), test.Title, part+1, ended.Correct+ended.Wrong, ended.Correct, ended.Wrong, ended.Skipped,
		int(took.Minutes()), int(took.Seconds())%60)
	if status == storage.QuizDone {
		if rank, total, err := s.opts.Quizzes.QuizRank(ctx, ended.TestID, ended.First, ended.UserID); err == nil && total > 0 {
			text += "\n\n" + fmt.Sprintf(textQuizRank.to(in), total, rank)
		}
	}
	buttons := [][]telegram.Button{{{Text: textQuizAgainButton.to(in), Data: fmt.Sprintf("qp:%d:%d", ended.TestID, part)}}}
	if (part+1)*quizPartSize < len(test.Questions) {
		buttons[0] = append(buttons[0], telegram.Button{Text: textQuizNextButton.to(in), Data: fmt.Sprintf("qp:%d:%d", ended.TestID, part+1)})
	}
	s.sendQuizMessage(in, text, buttons)
}

// stopQuiz ends the sender's unfinished run for /stop, with its results so
// far; it reports false when there is none.
func (s *Service) stopQuiz(ctx context.Context, in incoming) bool {
	if !s.quizzesOn() || in.group {
		return false
	}
	run, ok, err := s.opts.Quizzes.ActiveQuizRun(ctx, in.userID)
	if err != nil || !ok {
		return false
	}
	s.finishQuizRun(ctx, in, run, storage.QuizStopped)
	return true
}

// quizTest reads a test, lately read ones from memory: a run reads its
// test for every question.
func (s *Service) quizTest(ctx context.Context, id int64) (storage.QuizTest, error) {
	if test, ok := s.quizTests.get(id); ok {
		return test, nil
	}
	test, err := s.opts.Quizzes.QuizTest(ctx, id)
	if err != nil {
		return storage.QuizTest{}, err
	}
	s.quizTests.set(id, test)
	return test, nil
}

// sendQuizMessage sends a test's message in the private chat, not as a
// reply.
func (s *Service) sendQuizMessage(in incoming, text string, buttons [][]telegram.Button) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if _, err := s.opts.Sender.SendMessage(ctx, telegram.Outgoing{ChatID: in.scope.ChatID, Text: text, Buttons: buttons}); err != nil {
		s.opts.Logger.Error("could not send a test message", "user_id", in.userID, "error", err)
	}
}
