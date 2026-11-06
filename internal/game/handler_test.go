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

	"telegram-ai-bot/internal/storage"
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

func (f *fakeScores) Avatar(_ context.Context, userID, _ int64) ([]byte, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	if userID == 42 {
		return []byte("\xff\xd8\xff\xe0 a photo"), true, nil
	}
	return nil, false, nil
}

func (f *fakeScores) GetGameHighScores(_ context.Context, game telegram.GameMessage) ([]telegram.GameHighScore, error) {
	return []telegram.GameHighScore{
		{Position: 1, User: telegram.User{ID: 7, FirstName: "Ali"}, Score: 300},
		{Position: 2, User: telegram.User{ID: game.UserID, FirstName: "Bek"}, Score: f.score},
	}, nil
}

type fakeRecords struct {
	got      []storage.GameScore
	err      error
	tableErr error
	asked    []string
}

func (f *fakeRecords) RecordGameScore(_ context.Context, score storage.GameScore) error {
	if f.err != nil {
		return f.err
	}
	f.got = append(f.got, score)
	return nil
}

func (f *fakeRecords) GameTable(_ context.Context, game string, userID int64, limit int) ([]storage.GameRank, error) {
	f.asked = append(f.asked, game)
	if f.tableErr != nil {
		return nil, f.tableErr
	}
	return []storage.GameRank{{Position: 1, UserID: 9, Name: "Vali", Best: 900}, {Position: limit + 2, UserID: userID, Name: "Bek", Best: 5}}, nil
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
	h := g.Handler(scores, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	session := sessionOf(t, playURL(t, g, ShortName, player))

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
	h := g.Handler(scores, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	session := sessionOf(t, playURL(t, g, ShortName, player))
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
	h := g.Handler(&fakeScores{err: errors.New("timeout")}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	run, dealt, _ := g.Start(sessionOf(t, playURL(t, g, ShortName, player)))
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
	h := g.Handler(scores, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	run, _, _ := g.Start(sessionOf(t, playURL(t, g, ShortName, player)))
	c.now = c.now.Add(time.Second)

	if status, got := post(t, h, request{Action: "finish", Run: run}); status != http.StatusOK || got["score"] != float64(0) || got["improved"] != false {
		t.Errorf("%d %v", status, got)
	}
	if len(scores.set) != 0 {
		t.Errorf("set %+v", scores.set)
	}
}

// Each play scored is recorded under its game, for the session's player in
// their chat; one Telegram does not take is not, and one the database does
// not take is the player's all the same.
func TestHandlerRecordsThePlays(t *testing.T) {
	g, c := newTestGame()
	records := &fakeRecords{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := g.Handler(&fakeScores{}, records, logger)
	named := player
	named.Name = "Bek"

	run, dealt, _ := g.Start(sessionOf(t, playURL(t, g, ShortName, named)))
	answers, times := rightAnswers(dealt, 3)
	c.now = c.now.Add(5 * time.Second)
	if status, got := post(t, h, request{Action: "finish", Run: run, Answers: answers, Times: times}); status != http.StatusOK {
		t.Fatalf("Tarozi: %d %v", status, got)
	}
	run, play, _ := g.StartOtinchi(sessionOf(t, playURL(t, g, OtinchiShortName, named)))
	sides, chops, want := woodcutter(play.Seed, 20, 200)
	c.now = c.now.Add(time.Duration(chops[len(chops)-1])*time.Millisecond + time.Second)
	if status, got := post(t, h, request{Action: "finish", Run: run, Sides: sides, Times: chops}); status != http.StatusOK {
		t.Fatalf("O'tinchi: %d %v", status, got)
	}
	if len(records.got) != 2 {
		t.Fatalf("recorded %+v", records.got)
	}
	tarozi, otinchi := records.got[0], records.got[1]
	if tarozi.Game != ShortName || tarozi.Score != 36 || tarozi.UserID != 42 || tarozi.ChatID != -100123 || tarozi.Name != "Bek" {
		t.Errorf("Tarozi's play: %+v", tarozi)
	}
	if otinchi.Game != OtinchiShortName || otinchi.Score != want || !otinchi.At.Equal(c.now) {
		t.Errorf("O'tinchi's play: %+v, want %d at %v", otinchi, want, c.now)
	}

	run, dealt, _ = g.Start(sessionOf(t, playURL(t, g, ShortName, named)))
	answers, times = rightAnswers(dealt, 1)
	c.now = c.now.Add(2 * time.Second)
	refused := g.Handler(&fakeScores{err: errors.New("timeout")}, records, logger)
	if status, _ := post(t, refused, request{Action: "finish", Run: run, Answers: answers, Times: times}); status != http.StatusBadGateway || len(records.got) != 2 {
		t.Errorf("a score Telegram refused: %d, recorded %d", status, len(records.got))
	}
	records.err = errors.New("database down")
	if status, got := post(t, h, request{Action: "finish", Run: run, Answers: answers, Times: times}); status != http.StatusOK || got["score"].(float64) <= 0 {
		t.Errorf("a play the database refused: %d %v", status, got)
	}
}

// The table comes from the records across all chats, for the session's
// game, the player's row marked; when they fail it is Telegram's.
func TestHandlerTableFromTheRecords(t *testing.T) {
	g, _ := newTestGame()
	records := &fakeRecords{}
	h := g.Handler(&fakeScores{}, records, slog.New(slog.NewTextHandler(io.Discard, nil)))
	session := sessionOf(t, playURL(t, g, OtinchiShortName, player))

	status, got := post(t, h, request{Action: "table", Session: session})
	table, _ := got["table"].([]any)
	if status != http.StatusOK || len(table) != 2 || len(records.asked) != 1 || records.asked[0] != OtinchiShortName {
		t.Fatalf("table: %d %v, asked %v", status, got, records.asked)
	}
	first, mine := table[0].(map[string]any), table[1].(map[string]any)
	if first["name"] != "Vali" || first["score"] != 900.0 || first["me"] != false || mine["position"] != float64(tableSize+2) || mine["me"] != true {
		t.Errorf("table = %v", table)
	}
	if _, got := post(t, h, request{Action: "table", Session: sessionOf(t, playURL(t, g, ShortName, player))}); records.asked[1] != ShortName {
		t.Errorf("Tarozi's table asked for %v: %v", records.asked, got)
	}

	records.tableErr = errors.New("unreachable")
	_, got = post(t, h, request{Action: "table", Session: session})
	if table, _ := got["table"].([]any); len(table) != 2 || table[0].(map[string]any)["name"] != "Ali" {
		t.Errorf("Telegram's table = %v", got)
	}
}

// A row of the table carries its player's photo address; the address gives
// the photo, kept a day, a player without one a 404, and a forged or old
// address nothing.
func TestHandlerServesAvatars(t *testing.T) {
	g, c := newTestGame()
	h := g.Handler(&fakeScores{}, &fakeRecords{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, got := post(t, h, request{Action: "table", Session: sessionOf(t, playURL(t, g, OtinchiShortName, player))})
	mine := got["table"].([]any)[1].(map[string]any)["avatar"].(string)
	if !strings.HasPrefix(mine, "/api/game?avatar=") {
		t.Fatalf("avatar = %q", mine)
	}
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if rec := get(mine); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" || !strings.Contains(rec.Header().Get("Cache-Control"), "max-age=86400") {
		t.Errorf("my photo: %d %v", rec.Code, rec.Header())
	}
	if rec := get(g.AvatarPath(9)); rec.Code != http.StatusNotFound {
		t.Errorf("no photo: %d", rec.Code)
	}
	if rec := get(mine + "x"); rec.Code != http.StatusUnauthorized {
		t.Errorf("forged: %d", rec.Code)
	}
	c.now = c.now.Add(72 * time.Hour)
	if rec := get(mine); rec.Code != http.StatusUnauthorized {
		t.Errorf("three days on: %d", rec.Code)
	}
}
