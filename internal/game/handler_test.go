package game

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/telegram"
)

type fakeScores struct {
	set   []telegram.GameMessage
	score int
	err   error
}

func (f *fakeScores) SetGameScore(_ context.Context, game telegram.GameMessage, score int) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.set = append(f.set, game)
	f.score = score
	return true, nil
}

func (f *fakeScores) GetGameHighScores(_ context.Context, game telegram.GameMessage) ([]telegram.GameHighScore, error) {
	return []telegram.GameHighScore{
		{Position: 1, User: telegram.User{ID: 7, FirstName: "Ali"}, Score: 300},
		{Position: 2, User: telegram.User{ID: game.UserID, FirstName: "Bek"}, Score: f.score},
	}, nil
}

func post(t *testing.T, h http.Handler, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/game", strings.NewReader(string(raw))))
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response %q: %v", rec.Body.String(), err)
	}
	return rec.Code, got
}

// The page starts a play, sends its answers, and gets the score the server
// counted, set on the game message for the session's player, with the
// table.
func TestHandlerPlaysAGame(t *testing.T) {
	g, c := newTestGame()
	scores := &fakeScores{}
	h := g.Handler(scores, slog.New(slog.NewTextHandler(io.Discard, nil)))
	session := sessionOf(t, g.PlayURL(player))

	status, start := post(t, h, request{Action: "start", Session: session})
	if status != http.StatusOK || start["seconds"] != float64(Seconds) || len(start["rounds"].([]any)) != rounds {
		t.Fatalf("start: %d %v", status, start["seconds"])
	}
	run := start["run"].(string)
	var r struct {
		Rounds []Round `json:"rounds"`
	}
	raw, _ := json.Marshal(start)
	_ = json.Unmarshal(raw, &r)
	answers, times := rightAnswers(r.Rounds, 3)
	c.now = c.now.Add(5 * time.Second)

	status, done := post(t, h, request{Action: "finish", Run: run, Answers: answers, Times: times})
	if status != http.StatusOK || done["score"] != float64(36) || done["improved"] != true {
		t.Fatalf("finish: %d %v", status, done)
	}
	want := telegram.GameMessage{UserID: 42, ChatID: -100123, MessageID: 77}
	if len(scores.set) != 1 || scores.set[0] != want || scores.score != 36 {
		t.Errorf("set %+v = %d, want %+v = 36", scores.set, scores.score, want)
	}
	table := done["table"].([]any)
	if len(table) != 2 || table[1].(map[string]any)["me"] != true || table[0].(map[string]any)["name"] != "Ali" {
		t.Errorf("table = %v", table)
	}

	status, got := post(t, h, request{Action: "table", Session: session})
	if status != http.StatusOK || len(got["table"].([]any)) != 2 {
		t.Errorf("table: %d %v", status, got)
	}
}

// What the page invents is refused, and no score is set for it.
func TestHandlerRefusesWhatIsNotAPlay(t *testing.T) {
	g, _ := newTestGame()
	scores := &fakeScores{}
	h := g.Handler(scores, slog.New(slog.NewTextHandler(io.Discard, nil)))
	session := sessionOf(t, g.PlayURL(player))
	_, start := post(t, h, request{Action: "start", Session: session})
	run := start["run"].(string)

	cases := map[string]struct {
		body   request
		status int
		code   string
	}{
		"forged session": {request{Action: "start", Session: "e30.AAAA"}, http.StatusUnauthorized, "session"},
		"session as run": {request{Action: "finish", Run: session}, http.StatusUnauthorized, "session"},
		"too fast":       {request{Action: "finish", Run: run, Answers: "LL", Times: []int{10, 20}}, http.StatusUnprocessableEntity, "implausible"},
		"no action":      {request{Session: session}, http.StatusBadRequest, "bad_request"},
	}
	for name, c := range cases {
		if status, got := post(t, h, c.body); status != c.status || got["error"] != c.code {
			t.Errorf("%s: %d %v; want %d %s", name, status, got, c.status, c.code)
		}
	}
	if len(scores.set) != 0 {
		t.Errorf("scores set: %+v", scores.set)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/game", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", rec.Code)
	}
}

// When Telegram does not take the score, the page is told to try again.
func TestHandlerWhenTheScoreIsNotSet(t *testing.T) {
	g, c := newTestGame()
	h := g.Handler(&fakeScores{err: errors.New("timeout")}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	run, dealt, _ := g.Start(sessionOf(t, g.PlayURL(player)))
	answers, times := rightAnswers(dealt, 1)
	c.now = c.now.Add(2 * time.Second)

	if status, got := post(t, h, request{Action: "finish", Run: run, Answers: answers, Times: times}); status != http.StatusBadGateway || got["error"] != "unavailable" {
		t.Errorf("%d %v", status, got)
	}
}

// A play that scored nothing sets no score, which would only put the player
// at the bottom of the table.
func TestHandlerSetsNoZero(t *testing.T) {
	g, c := newTestGame()
	scores := &fakeScores{}
	h := g.Handler(scores, slog.New(slog.NewTextHandler(io.Discard, nil)))
	run, _, _ := g.Start(sessionOf(t, g.PlayURL(player)))
	c.now = c.now.Add(time.Second)

	if status, got := post(t, h, request{Action: "finish", Run: run}); status != http.StatusOK || got["score"] != float64(0) || got["improved"] != false {
		t.Errorf("%d %v", status, got)
	}
	if len(scores.set) != 0 {
		t.Errorf("set %+v", scores.set)
	}
}
