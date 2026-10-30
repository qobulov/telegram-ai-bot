package shashkaapp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	token = "123:secret"
	alice = int64(2001)
	bob   = int64(2002)
)

// fakeMessenger records what the bot writes.
type fakeMessenger struct {
	mu   sync.Mutex
	sent []telegram.Outgoing
}

func (f *fakeMessenger) SendMessage(_ context.Context, msg telegram.Outgoing) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return 1, nil
}

// clock is a time the test moves.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fixture struct {
	t         *testing.T
	handler   http.Handler
	messenger *fakeMessenger
	clock     *clock
}

// newFixture serves the API over the database TEST_DB_POSTGRES_URL names,
// migrated, with the draughts tables emptied; it skips without one.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("TEST_DB_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_DB_POSTGRES_URL is not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := storage.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if _, err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `TRUNCATE shashka_players, shashka_games, shashka_queue`); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, messenger: &fakeMessenger{}, clock: &clock{now: time.Now()}}
	f.handler = NewHandler(Options{
		BotToken: token, BotUsername: "MariaBot", PageURL: "https://example.app/shashka/",
		Store: store, Messenger: f.messenger, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: f.clock.Now, Pick: func(int) int { return 0 },
	})
	return f
}

// initData is what Telegram gives the page opened by userID.
func initData(userID int64, name string, at time.Time) string {
	fields := map[string]string{
		"auth_date": fmt.Sprint(at.Unix()),
		"user":      fmt.Sprintf(`{"id":%d,"first_name":%q,"language_code":"uz"}`, userID, name),
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	values := url.Values{}
	for _, k := range keys {
		lines = append(lines, k+"="+fields[k])
		values.Set(k, fields[k])
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	h := hmac.New(sha256.New, secret.Sum(nil))
	h.Write([]byte(strings.Join(lines, "\n")))
	values.Set("hash", hex.EncodeToString(h.Sum(nil)))
	return values.Encode()
}

// answer is what the API answered, as the page reads it.
type answer struct {
	OK     bool       `json:"ok"`
	Error  string     `json:"error"`
	Queued bool       `json:"queued"`
	Game   *gameView  `json:"game"`
	Me     playerJSON `json:"me"`
}

type playerJSON struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Rating int    `json:"rating"`
	Wins   int    `json:"wins"`
	Losses int    `json:"losses"`
}

type gameView struct {
	ID       int64   `json:"id"`
	Kind     string  `json:"kind"`
	Status   string  `json:"status"`
	You      string  `json:"you"`
	Board    string  `json:"board"`
	Turn     string  `json:"turn"`
	Moves    [][]int `json:"moves"`
	Last     []int   `json:"last"`
	Version  int     `json:"version"`
	Deadline int64   `json:"deadline_ms"`
	Black    *struct {
		Name string `json:"name"`
		Bot  bool   `json:"bot"`
	} `json:"black"`
	Invite *struct {
		Code string `json:"code"`
		Link string `json:"link"`
	} `json:"invite"`
	Result *struct {
		Winner       *string `json:"winner"`
		Reason       string  `json:"reason"`
		RatingChange int     `json:"rating_change"`
	} `json:"result"`
}

func (f *fixture) do(userID int64, name string, body map[string]any) answer {
	f.t.Helper()
	body["init_data"] = initData(userID, name, f.clock.Now())
	encoded, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/shashka", bytes.NewReader(encoded)))
	var a answer
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		f.t.Fatalf("%s: unreadable answer %q", body["action"], rec.Body.String())
	}
	return a
}

func (f *fixture) must(userID int64, name string, body map[string]any) answer {
	f.t.Helper()
	a := f.do(userID, name, body)
	if !a.OK {
		f.t.Fatalf("%v: %s", body["action"], a.Error)
	}
	return a
}

// A friend takes an invite by its code, the inviter is told, moves are
// checked against the rules and the version, and a resignation moves both
// ratings.
func TestAFriendGame(t *testing.T) {
	f := newFixture(t)
	hello := f.must(alice, "Alice", map[string]any{"action": "hello"})
	if hello.Me.Rating != 1200 || hello.Game != nil {
		t.Fatalf("hello = %+v", hello)
	}
	invite := f.must(alice, "Alice", map[string]any{"action": "invite"}).Game
	if invite.Status != "waiting" || invite.Invite == nil || invite.Invite.Link != "https://t.me/MariaBot?start=sh_"+invite.Invite.Code || invite.Black != nil {
		t.Fatalf("invite = %+v", invite)
	}
	if a := f.do(alice, "Alice", map[string]any{"action": "join", "code": invite.Invite.Code}); a.Error != "own_game" {
		t.Fatalf("joining one's own invite = %+v", a)
	}
	joined := f.must(bob, "Bob", map[string]any{"action": "join", "code": strings.ToUpper(invite.Invite.Code)}).Game
	if joined.Status != "playing" || joined.You != "black" || len(joined.Moves) != 0 || joined.Deadline <= 0 {
		t.Fatalf("joined = %+v", joined)
	}
	if len(f.messenger.sent) != 1 || f.messenger.sent[0].ChatID != alice || !strings.Contains(f.messenger.sent[0].Text, "Bob") {
		t.Fatalf("the inviter was told %+v", f.messenger.sent)
	}

	game := f.must(alice, "Alice", map[string]any{"action": "state", "game": joined.ID}).Game
	if game.You != "white" || game.Turn != "white" || len(game.Moves) != 7 {
		t.Fatalf("white's view = %+v", game)
	}
	if a := f.do(bob, "Bob", map[string]any{"action": "move", "game": game.ID, "version": game.Version, "path": game.Moves[0]}); a.Error != "not_your_turn" {
		t.Errorf("black moving first = %+v", a)
	}
	if a := f.do(alice, "Alice", map[string]any{"action": "move", "game": game.ID, "version": game.Version, "path": []int{20, 12}}); a.Error != "illegal" {
		t.Errorf("an illegal move = %+v", a)
	}
	if a := f.do(bob+1, "Eve", map[string]any{"action": "state", "game": game.ID}); a.Error != "not_found" {
		t.Errorf("someone else's game = %+v", a)
	}
	moved := f.must(alice, "Alice", map[string]any{"action": "move", "game": game.ID, "version": game.Version, "path": game.Moves[0]}).Game
	if moved.Turn != "black" || moved.Version != game.Version+1 || len(moved.Last) != 2 || len(moved.Moves) != 0 {
		t.Fatalf("after white's move = %+v", moved)
	}
	if a := f.do(alice, "Alice", map[string]any{"action": "move", "game": game.ID, "version": game.Version, "path": game.Moves[1]}); a.Error != "stale" {
		t.Errorf("a move from an old version = %+v", a)
	}

	over := f.must(bob, "Bob", map[string]any{"action": "resign", "game": game.ID}).Game
	if over.Status != "over" || over.Result == nil || over.Result.Reason != "resign" || *over.Result.Winner != "white" || over.Result.RatingChange != -16 {
		t.Fatalf("after black resigned = %+v", over)
	}
	me := f.must(alice, "Alice", map[string]any{"action": "hello"})
	if me.Me.Rating != 1216 || me.Me.Wins != 1 || me.Game != nil {
		t.Fatalf("Alice after the win = %+v", me)
	}
}

// Against the bot each move is answered in the same request, and a game
// left for another is given up.
func TestABotGame(t *testing.T) {
	f := newFixture(t)
	game := f.must(alice, "Alice", map[string]any{"action": "bot", "level": 2}).Game
	if game.Kind != "bot" || game.You != "white" || game.Black == nil || !game.Black.Bot || game.Deadline != 0 {
		t.Fatalf("bot game = %+v", game)
	}
	moved := f.must(alice, "Alice", map[string]any{"action": "move", "game": game.ID, "version": game.Version, "path": game.Moves[0]}).Game
	if moved.Turn != "white" || len(moved.Moves) == 0 || moved.Board == game.Board {
		t.Fatalf("after the bot's answer = %+v", moved)
	}
	if moved.Last[0] >= 20 {
		t.Errorf("the last move shown is white's, %v, not the bot's", moved.Last)
	}
	next := f.must(alice, "Alice", map[string]any{"action": "bot", "level": 3}).Game
	if next.ID == game.ID {
		t.Fatal("a new bot game reused the old one")
	}
	if old := f.must(alice, "Alice", map[string]any{"action": "state", "game": game.ID}).Game; old.Status != "over" || old.Result.Reason != "resign" {
		t.Errorf("the game left = %+v", old)
	}
	if a := f.do(alice, "Alice", map[string]any{"action": "bot", "level": 9}); a.Error != "bad_request" {
		t.Errorf("a level that is not = %+v", a)
	}
}

// Two players asking for a random opponent are paired, and the one who
// asked first is given the game on asking again; a clock run out loses.
func TestARandomGameAndItsClock(t *testing.T) {
	f := newFixture(t)
	if a := f.must(alice, "Alice", map[string]any{"action": "queue"}); !a.Queued || a.Game != nil {
		t.Fatalf("the first to ask = %+v", a)
	}
	paired := f.must(bob, "Bob", map[string]any{"action": "queue"}).Game
	if paired == nil || paired.Kind != "random" || paired.Status != "playing" {
		t.Fatalf("the second to ask = %+v", paired)
	}
	again := f.must(alice, "Alice", map[string]any{"action": "queue"}).Game
	if again == nil || again.ID != paired.ID {
		t.Fatalf("the first asking again = %+v, want game %d", again, paired.ID)
	}
	// Pick is 0: the first in the queue, Alice, is white and moves first.
	if again.You != "white" || len(again.Moves) == 0 {
		t.Fatalf("Alice's side = %+v", again)
	}
	f.clock.add(moveTime + time.Second)
	over := f.must(bob, "Bob", map[string]any{"action": "state", "game": paired.ID}).Game
	if over.Status != "over" || over.Result.Reason != "timeout" || *over.Result.Winner != "black" || over.Result.RatingChange != 16 {
		t.Fatalf("after white's clock ran out = %+v", over)
	}
}

// Only init data Telegram signed is taken.
func TestForgedInitDataIsRefused(t *testing.T) {
	f := newFixture(t)
	body, _ := json.Marshal(map[string]any{"action": "hello", "init_data": strings.Replace(initData(alice, "Alice", f.clock.Now()), "2001", "2002", 1)})
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/shashka", bytes.NewReader(body)))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"forged"`) {
		t.Fatalf("forged init data = %d %s", rec.Code, rec.Body)
	}
	if a := f.do(alice, "Alice", map[string]any{"action": "nope"}); a.Error != "bad_request" {
		t.Errorf("an unknown action = %+v", a)
	}
}
