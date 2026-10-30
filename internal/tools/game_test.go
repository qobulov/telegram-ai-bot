package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/telegram"
)

type gameSent struct {
	chatID, replyTo int64
	shortName       string
}

type fakeGameSender struct {
	mu    sync.Mutex
	games []gameSent
	err   error
}

func (f *fakeGameSender) SendGame(_ context.Context, chatID int64, shortName string, replyTo int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.games = append(f.games, gameSent{chatID: chatID, replyTo: replyTo, shortName: shortName})
	if f.err != nil {
		return 0, f.err
	}
	return int64(900 + len(f.games)), nil
}

func (f *fakeGameSender) sent() []gameSent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]gameSent(nil), f.games...)
}

type gameEnv struct {
	sender   *fakeGameSender
	audit    *fakePollAudit
	now      time.Time
	registry *Registry
}

func newGameEnv(t *testing.T) *gameEnv {
	t.Helper()
	env := &gameEnv{sender: &fakeGameSender{}, now: rulesNow}
	env.audit = &fakePollAudit{now: func() time.Time { return env.now }}
	env.registry = mustRegistry(t, GameTool(GameOptions{Sender: env.sender, Store: env.audit, Games: []string{"tarozi", "ilon"}, Now: func() time.Time { return env.now }}))
	return env
}

func (env *gameEnv) send(ctx context.Context) envelope {
	caller, _ := CallerFrom(ctx)
	var result envelope
	_ = json.Unmarshal([]byte(env.registry.Execute(ctx, caller.UserID, "send_game", `{}`)), &result)
	return result
}

// The game is posted as a reply to the request, in the chat the turn is in.
func TestGameIsSent(t *testing.T) {
	env := newGameEnv(t)

	if got := env.send(memberAsking(groupA, 100)); !got.OK || !strings.Contains(string(got.Result), "Play") {
		t.Fatalf("send_game = %+v %s", got, got.Result)
	}
	if games := env.sender.sent(); len(games) != 1 || games[0] != (gameSent{chatID: groupA, replyTo: 60, shortName: "tarozi"}) {
		t.Errorf("games = %+v", games)
	}
	if audit := env.audit.recorded(); len(audit) != 1 || audit[0].Action != "game" || audit[0].UpdateID != 100 {
		t.Errorf("audit = %+v", audit)
	}
}

// One message gets one game, and a chat gameLimit in gameWindow.
func TestGamesArePaced(t *testing.T) {
	env := newGameEnv(t)
	ctx := memberAsking(groupA, 100)
	env.send(ctx)
	if got := env.send(ctx); !got.OK || !strings.Contains(string(got.Result), "already_handled") {
		t.Errorf("second game in one message = %+v %s", got, got.Result)
	}
	env.send(memberAsking(groupA, 101))
	if got := env.send(memberAsking(groupA, 102)); got.OK || !strings.Contains(got.Error.Message, "2 games in the last 10 minutes") {
		t.Errorf("game over the limit = %+v", got)
	}
	if games := len(env.sender.sent()); games != gameLimit {
		t.Errorf("%d games, want %d", games, gameLimit)
	}
	env.now = env.now.Add(gameWindow + time.Second)
	if got := env.send(memberAsking(groupA, 103)); !got.OK {
		t.Errorf("game after the window = %+v", got)
	}
}

// A game Telegram refused is forgotten; one it did not answer is kept.
func TestGameTelegramFailures(t *testing.T) {
	env := newGameEnv(t)
	env.sender.err = &telegram.APIError{Code: 400, Description: "Bad Request: GAME_SHORTNAME_INVALID"}
	if got := env.send(memberAsking(groupA, 100)); got.OK || !strings.Contains(got.Error.Message, "GAME_SHORTNAME_INVALID") {
		t.Errorf("refused game = %+v", got)
	}
	if audit := env.audit.recorded(); len(audit) != 0 {
		t.Errorf("a refused game is still recorded: %+v", audit)
	}
	env.sender.err = errors.New("connection reset")
	if got := env.send(memberAsking(groupA, 101)); got.OK || !strings.Contains(got.Error.Message, "may be in the chat") {
		t.Errorf("unanswered game = %+v", got)
	}
	if audit := env.audit.recorded(); len(audit) != 1 {
		t.Errorf("audit = %+v, want the unanswered game kept", audit)
	}
}
