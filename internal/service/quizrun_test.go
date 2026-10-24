package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/quizfile"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeQuizStore keeps tests and runs in memory, scoring as the store does:
// a question once, by its poll.
type fakeQuizStore struct {
	mu      sync.Mutex
	tests   map[int64]storage.QuizTest
	runs    map[int64]*storage.QuizRun
	answers map[int64]int // the right option of each run's waiting poll
}

func newFakeQuizStore() *fakeQuizStore {
	return &fakeQuizStore{tests: map[int64]storage.QuizTest{}, runs: map[int64]*storage.QuizRun{}, answers: map[int64]int{}}
}

func (f *fakeQuizStore) SaveQuizTest(_ context.Context, ownerID int64, title string, questions []quizfile.Question) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := int64(len(f.tests) + 1)
	f.tests[id] = storage.QuizTest{ID: id, OwnerID: ownerID, Title: title, Questions: questions}
	return id, nil
}

func (f *fakeQuizStore) QuizTest(_ context.Context, id int64) (storage.QuizTest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tests[id]; ok {
		return t, nil
	}
	return storage.QuizTest{}, storage.ErrNoQuizRun
}

func (f *fakeQuizStore) NewQuizRun(_ context.Context, testID, userID, chatID int64, first, count int) (storage.QuizRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	run := &storage.QuizRun{ID: int64(len(f.runs) + 1), TestID: testID, UserID: userID, ChatID: chatID, First: first, Count: count, Status: storage.QuizReady}
	f.runs[run.ID] = run
	return *run, nil
}

func (f *fakeQuizStore) StartQuizRun(_ context.Context, id, userID int64) (storage.QuizRun, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	run, ok := f.runs[id]
	if !ok || run.UserID != userID || (run.Status != storage.QuizReady && run.Status != storage.QuizPaused) {
		return storage.QuizRun{}, false, nil
	}
	run.Status, run.MissedInRow = storage.QuizRunning, 0
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	return *run, true, nil
}

func (f *fakeQuizStore) AskedQuizQuestion(_ context.Context, id int64, pollID string, answer int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs[id].PollID, f.answers[id] = pollID, answer
	return nil
}

func (f *fakeQuizStore) AnswerQuizQuestion(_ context.Context, pollID string, chosen int) (storage.QuizRun, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, run := range f.runs {
		if run.PollID != pollID || run.Status != storage.QuizRunning {
			continue
		}
		run.Position++
		switch {
		case chosen < 0:
			run.Skipped++
			run.MissedInRow++
		case chosen == f.answers[run.ID]:
			run.Correct++
			run.MissedInRow = 0
		default:
			run.Wrong++
			run.MissedInRow = 0
		}
		run.PollID = ""
		return *run, true, nil
	}
	return storage.QuizRun{}, false, nil
}

func (f *fakeQuizStore) EndQuizRun(_ context.Context, id, userID int64, status string) (storage.QuizRun, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	run, ok := f.runs[id]
	if !ok || run.UserID != userID || run.Status == storage.QuizDone || run.Status == storage.QuizStopped {
		return storage.QuizRun{}, false, nil
	}
	run.Status, run.PollID = status, ""
	if status != storage.QuizPaused {
		run.FinishedAt = run.StartedAt.Add(6*time.Minute + 50*time.Second)
	}
	return *run, true, nil
}

func (f *fakeQuizStore) ActiveQuizRun(_ context.Context, userID int64) (storage.QuizRun, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, run := range f.runs {
		if run.UserID == userID && (run.Status == storage.QuizReady || run.Status == storage.QuizRunning || run.Status == storage.QuizPaused) {
			return *run, true, nil
		}
	}
	return storage.QuizRun{}, false, nil
}

func (f *fakeQuizStore) QuizRank(context.Context, int64, int, int64) (int, int, error) {
	return 11, 31, nil
}

// fakeQuizPoller records the quizzes sent and names their polls p1, p2...
type fakeQuizPoller struct {
	mu    sync.Mutex
	polls []telegram.OutgoingPoll
}

func (f *fakeQuizPoller) SendQuizQuestion(_ context.Context, poll telegram.OutgoingPoll) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls = append(f.polls, poll)
	return fmt.Sprintf("p%d", len(f.polls)), nil
}

func (f *fakeQuizPoller) last() telegram.OutgoingPoll {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.polls[len(f.polls)-1]
}

// testFile is a test of n questions in the HEMIS format; question i's
// right answer is "right i".
func testFile(n int) string {
	var b strings.Builder
	b.WriteString("TOSHKENT DAVLAT IQTISODIYOT UNIVERSITETI\nYAKUNIY NAZORAT TEST SAVOLLARI\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "Savol %d?\n====\n#right %d\n====\nwrong a %d\n====\nwrong b %d\n++++\n", i, i, i, i)
	}
	return b.String()
}

func quizFixture(n int) (*fixture, *fakeQuizStore, *fakeQuizPoller, *fakeButtons) {
	f := newFixture()
	store, polls, buttons := newFakeQuizStore(), &fakeQuizPoller{}, &fakeButtons{}
	f.opts.Quizzes, f.opts.QuizPolls, f.opts.Buttons = store, polls, buttons
	f.media.files["test-file"] = []byte(testFile(n))
	return f, store, polls, buttons
}

func sendTestFile() telegram.Update {
	return privateWith(func(m *telegram.Message) {
		m.Document = &telegram.Document{FileID: "test-file", FileName: "Iqtisodiyot.txt", FileSize: 100}
	})
}

func press(data string) telegram.Update {
	nextUpdateID++
	return telegram.Update{UpdateID: nextUpdateID, CallbackQuery: &telegram.CallbackQuery{
		ID: "cb", From: &telegram.User{ID: userID}, Data: data,
		Message: &telegram.Message{MessageID: 500, Chat: telegram.Chat{ID: userID, Type: telegram.ChatTypePrivate}},
	}}
}

func answer(pollID string, option int) telegram.Update {
	return telegram.Update{PollAnswer: &telegram.PollAnswer{PollID: pollID, User: &telegram.User{ID: userID}, OptionIDs: []int{option}}}
}

// rightOption is the index the last quiz's right answer was shuffled to.
func rightOption(polls *fakeQuizPoller) int {
	return polls.last().Correct
}

// A test file is split into parts of 30 in code, with no call to the model.
func TestTestFileIsSplitIntoParts(t *testing.T) {
	f, store, _, _ := quizFixture(65)

	sent := f.run(sendTestFile())

	if len(f.replier.recorded()) != 0 {
		t.Error("the model was asked")
	}
	if len(sent) != 1 || !strings.Contains(sent[0].text, "«Iqtisodiyot»") || !strings.Contains(sent[0].text, "65 ta savol, 3 qism (30 tadan)") {
		t.Fatalf("sent = %+v", sent)
	}
	var labels, data []string
	for _, row := range sent[0].buttons {
		for _, b := range row {
			labels, data = append(labels, b.Text), append(data, b.Data)
		}
	}
	if strings.Join(labels, "|") != "1-qism (1–30)|2-qism (31–60)|3-qism (61–65)" || data[2] != "qp:1:2" {
		t.Errorf("buttons = %v %v", labels, data)
	}
	if len(store.tests[1].Questions) != 65 {
		t.Errorf("kept %d questions", len(store.tests[1].Questions))
	}
}

// A file that is no test goes to the model as before.
func TestOrdinaryFileGoesToTheModel(t *testing.T) {
	f, _, _, _ := quizFixture(0)
	f.media.files["test-file"] = []byte("Bu oddiy hisobot, unda test yo'q.")
	f.run(sendTestFile())
	if len(f.replier.recorded()) != 1 {
		t.Error("an ordinary file did not reach the model")
	}
}

// A part is asked question by question, each scored once, and ends with
// the results and buttons to try again or go on.
func TestQuizPartRunsToItsResults(t *testing.T) {
	f, store, polls, _ := quizFixture(32)
	s := New(f.opts)
	defer s.Close()
	s.HandleNow(sendTestFile())

	s.HandleNow(press("qp:1:1")) // part 2: questions 31 and 32
	ready := f.sender.messages()[1]
	if !strings.Contains(ready.text, "2-qism") || !strings.Contains(ready.text, "2 ta savol") || ready.buttons[0][0].Data != "qs:1" {
		t.Fatalf("ready = %+v", ready)
	}
	s.HandleNow(press("qs:1"))
	first := polls.last()
	if first.Question != "[1/2] Savol 31?" || !first.Quiz || first.OpenPeriod != 30 || first.Options[first.Correct] != "right 31" {
		t.Fatalf("first question = %+v", first)
	}

	s.HandleNow(answer("p1", rightOption(polls)))
	s.HandleNow(answer("p1", rightOption(polls))) // Telegram sent it twice
	if second := polls.last(); second.Question != "[2/2] Savol 32?" || len(polls.polls) != 2 {
		t.Fatalf("polls = %+v", polls.polls)
	}
	s.HandleNow(telegram.Update{Poll: &telegram.Poll{ID: "p2", IsClosed: true}}) // time ran out

	msgs := f.sender.messages()
	results := msgs[len(msgs)-1]
	for _, want := range []string{"2-qism yakunlandi", "Siz 1 ta savolga javob berdingiz", "To'g'ri: 1", "Xato: 0", "Tashlab ketilgan: 1", "6 daqiqa 50 soniya", "31 tadan 11-o'rin"} {
		if !strings.Contains(results.text, want) {
			t.Errorf("results %q lack %q", results.text, want)
		}
	}
	if len(results.buttons[0]) != 1 || results.buttons[0][0].Data != "qp:1:1" {
		t.Errorf("buttons = %+v; the last part has no next", results.buttons)
	}
	if run := store.runs[1]; run.Status != storage.QuizDone || run.Correct != 1 || run.Skipped != 1 {
		t.Errorf("run = %+v", run)
	}
	if len(f.replier.recorded()) != 0 {
		t.Error("the model was asked")
	}
}

// Two questions missed in a row pause the run; the button goes on.
func TestQuizPausesWhenNobodyAnswers(t *testing.T) {
	f, store, polls, _ := quizFixture(10)
	s := New(f.opts)
	defer s.Close()
	s.HandleNow(sendTestFile())
	s.HandleNow(press("qp:1:0"))
	s.HandleNow(press("qs:1"))
	s.HandleNow(telegram.Update{Poll: &telegram.Poll{ID: "p1", IsClosed: true}})
	s.HandleNow(telegram.Update{Poll: &telegram.Poll{ID: "p2", IsClosed: true}})

	if store.runs[1].Status != storage.QuizPaused || len(polls.polls) != 2 {
		t.Fatalf("run %+v after %d polls", store.runs[1], len(polls.polls))
	}
	msgs := f.sender.messages()
	if paused := msgs[len(msgs)-1]; !strings.Contains(paused.text, "test to'xtatildi") || paused.buttons[0][0].Data != "qs:1" {
		t.Errorf("paused = %+v", paused)
	}
	s.HandleNow(press("qs:1"))
	if len(polls.polls) != 3 || polls.last().Question != "[3/10] Savol 3?" {
		t.Errorf("after going on: %+v", polls.last())
	}
}

// /stop ends the run with its results so far.
func TestStopEndsTheRun(t *testing.T) {
	f, store, polls, _ := quizFixture(10)
	s := New(f.opts)
	defer s.Close()
	s.HandleNow(sendTestFile())
	s.HandleNow(press("qp:1:0"))
	s.HandleNow(press("qs:1"))
	s.HandleNow(answer("p1", (rightOption(polls)+1)%3))

	s.HandleNow(textFrom(userID, "/stop"))

	msgs := f.sender.messages()
	if stopped := msgs[len(msgs)-1]; !strings.Contains(stopped.text, "Xato: 1") || strings.Contains(stopped.text, "o'rin") {
		t.Errorf("stopped = %+v", stopped)
	}
	if store.runs[1].Status != storage.QuizStopped {
		t.Errorf("run = %+v", store.runs[1])
	}
	s.HandleNow(answer("p2", 0))
	if len(polls.polls) != 2 {
		t.Error("a stopped run asked again")
	}
}

// A question too long for a quiz is sent as a message, the quiz then
// having its options' letters.
func TestLongQuestionGoesAsAMessage(t *testing.T) {
	long := strings.Repeat("uzun ", 80) + "?"
	poll, text := quizQuestionPoll(incoming{msg: &telegram.Message{From: &telegram.User{}}}, storage.QuizRun{ChatID: 5, Count: 30},
		quizfile.Question{Text: long, Options: []string{"bir", "ikki", "uch"}, Correct: 1})
	if !strings.HasPrefix(text, "[1/30] uzun") || !strings.Contains(text, "\nA) ") || !strings.Contains(text, "\nC) ") {
		t.Errorf("text = %q", text)
	}
	if poll.Question != "[1/30] To'g'ri javobni tanlang" || strings.Join(poll.Options, "") != "ABC" {
		t.Errorf("poll = %+v", poll)
	}
	if !strings.Contains(text, string(rune('A'+poll.Correct))+") ikki") {
		t.Errorf("the right letter %d is not ikki in %q", poll.Correct, text)
	}
}

// A press of another user's run, or of a test that is gone, does nothing
// but say so.
func TestQuizButtonsOfOthersAreRefused(t *testing.T) {
	f, _, polls, buttons := quizFixture(10)
	s := New(f.opts)
	defer s.Close()
	s.HandleNow(press("qp:99:0"))
	s.HandleNow(press("qs:42"))
	if len(polls.polls) != 0 || len(buttons.answers) != 2 || !buttons.answers[0].alert || !strings.Contains(buttons.answers[1].text, "topilmadi") {
		t.Errorf("answers = %+v", buttons.answers)
	}
}
