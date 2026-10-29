package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// fakeJoinBook keeps join requests in memory, as the store does.
type fakeJoinBook struct {
	mu       sync.Mutex
	requests map[[2]int64]storage.JoinRequest
}

func (f *fakeJoinBook) AddJoinRequest(_ context.Context, r storage.JoinRequest) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]int64{r.ChatID, r.UserID}
	if _, ok := f.requests[key]; ok {
		return false, nil
	}
	f.requests[key] = r
	return true, nil
}

func (f *fakeJoinBook) TakeJoinRequest(_ context.Context, chatID, userID int64) (storage.JoinRequest, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]int64{chatID, userID}
	r, ok := f.requests[key]
	delete(f.requests, key)
	return r, ok, nil
}

func (f *fakeJoinBook) TakeLateJoinRequests(_ context.Context, now time.Time, limit int) ([]storage.JoinRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var late []storage.JoinRequest
	for key, r := range f.requests {
		if !r.Expires.After(now) && len(late) < limit {
			late = append(late, r)
			delete(f.requests, key)
		}
	}
	return late, nil
}

func (f *fakeJoinBook) waiting() []storage.JoinRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []storage.JoinRequest
	for _, r := range f.requests {
		all = append(all, r)
	}
	return all
}

// fakeJoinGate answers join requests as Telegram would, and notes them.
type fakeJoinGate struct {
	mu         sync.Mutex
	bot        telegram.ChatMember
	approveErr error
	approved   []int64
	declined   []int64
}

func (f *fakeJoinGate) GetChatMember(context.Context, int64, int64) (telegram.ChatMember, error) {
	return f.bot, nil
}

func (f *fakeJoinGate) ApproveChatJoinRequest(_ context.Context, _, userID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.approveErr != nil {
		return f.approveErr
	}
	f.approved = append(f.approved, userID)
	return nil
}

func (f *fakeJoinGate) DeclineChatJoinRequest(_ context.Context, _, userID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.declined = append(f.declined, userID)
	return nil
}

func (f *fakeJoinGate) answered() (approved, declined []int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.approved), slices.Clone(f.declined)
}

// failingSender sends nothing, as when Telegram does not let the bot write
// to someone.
type failingSender struct{ fakeSender }

func (*failingSender) SendMessage(context.Context, telegram.Outgoing) (int64, error) {
	return 0, &telegram.APIError{Code: 403, Description: "Forbidden: bot can't initiate conversation with a user"}
}

const joinerID = int64(5005)

// joinFixture is a group with the guards of kinds on, the joins guard
// unless kinds name others, whose bot may invite users, with a clock that
// tests move; spammerID is on CAS.
func joinFixture(kinds ...string) (*fixture, *fakeJoinBook, *fakeJoinGate, *fakeButtons, *time.Time) {
	f := newFixture()
	book := &fakeJoinBook{requests: map[[2]int64]storage.JoinRequest{}}
	gate := &fakeJoinGate{bot: telegram.ChatMember{Status: "administrator", CanInviteUsers: true}}
	buttons := &fakeButtons{}
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	if len(kinds) == 0 {
		kinds = []string{storage.RuleJoinCaptcha}
	}
	var rules []storage.GroupRule
	for i, kind := range kinds {
		rules = append(rules, storage.GroupRule{ID: int64(i + 1), ChatID: groupID, Kind: kind})
	}
	f.opts.Rules = &fakeRuleBook{rules: rules}
	f.opts.Guards = &fakeGuards{spammers: map[int64]string{spammerID: "CAS"}, badHosts: map[string]string{}}
	f.opts.Joins, f.opts.JoinGate, f.opts.Buttons = book, gate, buttons
	f.opts.Now = func() time.Time { return now }
	return f, book, gate, buttons, &now
}

// turnOffJoins removes the joins guard, as unguard does, and moves the clock
// past the minute the guards are remembered.
func turnOffJoins(f *fixture, now *time.Time) {
	book := f.opts.Rules.(*fakeRuleBook)
	book.mu.Lock()
	book.rules = slices.DeleteFunc(book.rules, func(r storage.GroupRule) bool { return r.Kind == storage.RuleJoinCaptcha })
	book.mu.Unlock()
	*now = now.Add(2 * time.Minute)
}

func joinRequest(userID int64, language string) telegram.Update {
	nextUpdateID++
	return telegram.Update{UpdateID: nextUpdateID, ChatJoinRequest: &telegram.ChatJoinRequest{
		Chat:       telegram.Chat{ID: groupID, Type: telegram.ChatTypeSupergroup, Title: "Lobby"},
		From:       &telegram.User{ID: userID, FirstName: "Ann", LanguageCode: language},
		UserChatID: userID,
	}}
}

func joinPress(userID int64, data string) telegram.Update {
	nextUpdateID++
	return telegram.Update{UpdateID: nextUpdateID, CallbackQuery: &telegram.CallbackQuery{
		ID:      "press",
		From:    &telegram.User{ID: userID},
		Message: &telegram.Message{MessageID: 1001, Chat: telegram.Chat{ID: userID, Type: telegram.ChatTypePrivate}},
		Data:    data,
	}}
}

var joinData = "join:" + "-1001234567890"

func TestJoinRequestGetsAButtonInPrivate(t *testing.T) {
	f, book, gate, _, now := joinFixture()

	sent := f.run(joinRequest(joinerID, "ru"), joinRequest(joinerID, "ru"))

	if len(sent) != 1 || sent[0].chatID != joinerID || !strings.Contains(sent[0].text, "«Lobby»") || !strings.HasPrefix(sent[0].text, "Здравствуйте") {
		t.Fatalf("sent = %+v, want one private message in Russian", sent)
	}
	if want := [][]telegram.Button{{{Text: "Я человек ✅", Data: joinData}}}; !slices.EqualFunc(sent[0].buttons, want, slices.Equal) {
		t.Errorf("buttons = %+v, want %+v", sent[0].buttons, want)
	}
	waiting := book.waiting()
	if len(waiting) != 1 || waiting[0].UserID != joinerID || !waiting[0].Expires.Equal(now.Add(10*time.Minute)) {
		t.Errorf("waiting = %+v, want the request for ten minutes", waiting)
	}
	if approved, declined := gate.answered(); len(approved)+len(declined) != 0 {
		t.Errorf("approved %v, declined %v before any press", approved, declined)
	}
}

// With the spam guard on, a known spammer is declined at once, whether or
// not the joins guard is on; anyone else gets the button only with it.
func TestJoinRequestFromAKnownSpammerIsDeclinedWhereTheSpamGuardIsOn(t *testing.T) {
	for name, kinds := range map[string][]string{
		"both guards":     {storage.RuleSpamGuard, storage.RuleJoinCaptcha},
		"spam guard only": {storage.RuleSpamGuard},
	} {
		t.Run(name, func(t *testing.T) {
			f, book, gate, _, _ := joinFixture(kinds...)

			sent := f.run(joinRequest(spammerID, ""))

			if _, declined := gate.answered(); len(sent) != 0 || !slices.Equal(declined, []int64{spammerID}) || len(book.waiting()) != 0 {
				t.Errorf("sent %+v, declined %v, waiting %+v; want only a decline", sent, declined, book.waiting())
			}
		})
	}

	f, book, gate, _, _ := joinFixture(storage.RuleSpamGuard)
	sent := f.run(joinRequest(joinerID, ""))
	if approved, declined := gate.answered(); len(sent)+len(approved)+len(declined)+len(book.waiting()) != 0 {
		t.Errorf("with the spam guard only, a clean requester got sent %+v, approved %v, declined %v; want them left to the admins", sent, approved, declined)
	}
}

// With the joins guard alone, a spammer is not looked up: the spam guard
// decides that, and the button stops spam bots anyway.
func TestJoinsGuardAloneSendsEveryoneTheButton(t *testing.T) {
	f, book, gate, _, _ := joinFixture()

	sent := f.run(joinRequest(spammerID, ""))

	if _, declined := gate.answered(); len(sent) != 1 || len(declined) != 0 || len(book.waiting()) != 1 {
		t.Errorf("sent %+v, declined %v; want the button", sent, declined)
	}
}

// Without the joins guard, in a group whose admins vet requests by hand, a
// button anyone could press would let people past them: the bot does
// nothing; nor without the right to invite users, nor in a muted group.
func TestJoinRequestsAreLeftAloneWhenTheBotMayNotAnswer(t *testing.T) {
	cases := map[string]func(*fixture, *fakeJoinGate){
		"no guard": func(f *fixture, _ *fakeJoinGate) { f.opts.Rules = &fakeRuleBook{} },
		"other guard": func(f *fixture, _ *fakeJoinGate) {
			f.opts.Rules = &fakeRuleBook{rules: []storage.GroupRule{{ChatID: groupID, Kind: storage.RuleImageGuard}}}
		},
		"no right":    func(_ *fixture, g *fakeJoinGate) { g.bot = telegram.ChatMember{Status: "administrator"} },
		"not admin":   func(_ *fixture, g *fakeJoinGate) { g.bot = telegram.ChatMember{Status: "member"} },
		"muted group": func(f *fixture, _ *fakeJoinGate) { f.opts.MutedChats = []int64{groupID} },
	}
	for name, setUp := range cases {
		t.Run(name, func(t *testing.T) {
			f, book, gate, _, _ := joinFixture()
			setUp(f, gate)

			sent := f.run(joinRequest(joinerID, ""))

			if approved, declined := gate.answered(); len(sent)+len(approved)+len(declined)+len(book.waiting()) != 0 {
				t.Errorf("sent %+v, approved %v, declined %v, waiting %+v", sent, approved, declined, book.waiting())
			}
		})
	}
}

// When the button cannot be sent, the request is the admins' again: neither
// kept for a sweep to decline nor declined.
func TestJoinRequestIsLeftToTheAdminsWhenTheButtonCannotBeSent(t *testing.T) {
	f, book, gate, _, _ := joinFixture()
	f.opts.Sender = &failingSender{}

	f.run(joinRequest(joinerID, ""))

	if _, declined := gate.answered(); len(declined) != 0 || len(book.waiting()) != 0 {
		t.Errorf("declined %v, waiting %+v; want the request forgotten", declined, book.waiting())
	}
}

func TestPressingTheButtonLetsTheRequesterIn(t *testing.T) {
	f, book, gate, buttons, _ := joinFixture()

	f.run(joinRequest(joinerID, ""), joinPress(joinerID, joinData), joinPress(joinerID, joinData))

	if approved, _ := gate.answered(); !slices.Equal(approved, []int64{joinerID}) {
		t.Errorf("approved %v, want the requester once", approved)
	}
	if len(book.waiting()) != 0 {
		t.Errorf("waiting = %+v after the press", book.waiting())
	}
	want := []string{textAdmitted.uz, textAdmitGone.uz}
	if !slices.Equal(buttons.edits, want) {
		t.Errorf("edits = %q, want %q", buttons.edits, want)
	}
}

// Only the requester's own press counts: the person comes from Telegram's
// press, never from the button.
func TestSomeoneElsesPressLetsNobodyIn(t *testing.T) {
	f, book, gate, _, _ := joinFixture()

	f.run(joinRequest(joinerID, ""), joinPress(joinerID+1, joinData), joinPress(joinerID, "join:not-a-number"))

	if approved, _ := gate.answered(); len(approved) != 0 || len(book.waiting()) != 1 {
		t.Errorf("approved %v, waiting %+v; want the request still waiting", approved, book.waiting())
	}
}

func TestALatePressIsDeclined(t *testing.T) {
	f, _, gate, buttons, now := joinFixture()
	s := New(f.opts)
	s.Handle(joinRequest(joinerID, ""))
	*now = now.Add(10 * time.Minute)
	s.Handle(joinPress(joinerID, joinData))
	s.stop()

	if approved, declined := gate.answered(); len(approved) != 0 || !slices.Equal(declined, []int64{joinerID}) {
		t.Errorf("approved %v, declined %v; want a decline", approved, declined)
	}
	if !slices.Equal(buttons.edits, []string{textAdmitLate.uz}) {
		t.Errorf("edits = %q", buttons.edits)
	}
}

// Without an answer from Telegram, the request is kept again, for another
// press or the sweep; refused, it is gone.
func TestAFailedApprovalKeepsTheRequestOnlyWithoutAnAnswer(t *testing.T) {
	cases := map[string]struct {
		err     error
		waiting int
		answer  string
	}{
		"no answer": {errors.New("connection reset"), 1, textAdmitRetry.uz},
		"refused":   {&telegram.APIError{Code: 400, Description: "Bad Request: HIDE_REQUESTER_MISSING"}, 0, textAdmitGone.uz},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f, book, gate, buttons, _ := joinFixture()
			gate.approveErr = c.err

			f.run(joinRequest(joinerID, ""), joinPress(joinerID, joinData))

			if len(book.waiting()) != c.waiting {
				t.Errorf("waiting = %+v, want %d", book.waiting(), c.waiting)
			}
			if len(buttons.answers) != 1 || buttons.answers[0].text != c.answer {
				t.Errorf("answers = %+v, want %q", buttons.answers, c.answer)
			}
		})
	}
}

// The late requests are declined, those still in time kept; on serverless
// hosting the updates themselves sweep, at most once a minute.
func TestLateJoinRequestsAreDeclinedBySweeps(t *testing.T) {
	f, book, gate, _, now := joinFixture()
	s := New(f.opts)
	defer s.Close()
	s.HandleNow(joinRequest(joinerID, ""))
	*now = now.Add(5 * time.Minute)
	s.HandleNow(joinRequest(joinerID+1, ""))

	*now = now.Add(6 * time.Minute)
	s.HandleNow(inGroup(groupMessage("salom")))
	if _, declined := gate.answered(); !slices.Equal(declined, []int64{joinerID}) {
		t.Fatalf("declined %v, want only the first, late, request", declined)
	}

	*now = now.Add(5 * time.Minute)
	s.HandleNow(inGroup(groupMessage("salom")))
	if _, declined := gate.answered(); !slices.Equal(declined, []int64{joinerID, joinerID + 1}) || len(book.waiting()) != 0 {
		t.Errorf("declined %v, waiting %+v; want both declined", declined, book.waiting())
	}
}

// A late request of a group whose joins guard is off since, or whose rules
// cannot be read, is dropped undeclined; one still in time is kept.
func TestDeclineLateJoinsLeavesRequestsInTimeAndUnguardedGroups(t *testing.T) {
	const otherGroup = int64(-1009876)
	now := time.Now()
	book := &fakeJoinBook{requests: map[[2]int64]storage.JoinRequest{
		{groupID, 1}:    {ChatID: groupID, UserID: 1, Expires: now.Add(-time.Second)},
		{groupID, 2}:    {ChatID: groupID, UserID: 2, Expires: now.Add(time.Minute)},
		{otherGroup, 3}: {ChatID: otherGroup, UserID: 3, Expires: now.Add(-time.Second)},
	}}
	gate := &fakeJoinGate{}
	rules := &fakeRuleBook{rules: []storage.GroupRule{{ChatID: groupID, Kind: storage.RuleJoinCaptcha}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	declined, err := DeclineLateJoins(context.Background(), book, gate, rules, now, logger)

	if _, who := gate.answered(); err != nil || declined != 1 || !slices.Equal(who, []int64{1}) || len(book.waiting()) != 1 {
		t.Errorf("declined %d (%v) %v, waiting %+v", declined, who, err, book.waiting())
	}

	book.requests[[2]int64{groupID, 4}] = storage.JoinRequest{ChatID: groupID, UserID: 4, Expires: now.Add(-time.Second)}
	rules.err = errors.New("database down")
	if declined, _ := DeclineLateJoins(context.Background(), book, gate, rules, now, logger); declined != 0 {
		t.Errorf("declined %d without the rules, want none", declined)
	}
}

// A guard turned off between the request and the press hands the request
// back to the admins: the press lets nobody in, and no sweep declines it.
func TestTurningTheJoinsGuardOffLeavesWaitingRequestsToTheAdmins(t *testing.T) {
	f, _, gate, buttons, now := joinFixture()
	s := New(f.opts)
	defer s.Close()
	s.HandleNow(joinRequest(joinerID, ""))
	s.HandleNow(joinRequest(joinerID+1, ""))
	turnOffJoins(f, now)

	s.HandleNow(joinPress(joinerID, joinData))
	*now = now.Add(20 * time.Minute)
	s.DeclineLateJoins(context.Background())

	if approved, declined := gate.answered(); len(approved)+len(declined) != 0 {
		t.Errorf("approved %v, declined %v; want both left to the admins", approved, declined)
	}
	if !slices.Equal(buttons.edits, []string{textAdmitGone.uz}) {
		t.Errorf("edits = %q", buttons.edits)
	}
}
