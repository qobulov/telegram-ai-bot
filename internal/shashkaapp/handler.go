// Package shashkaapp serves the draughts Mini App's API: the page in
// public/shashka is static and sends every action here with the init data
// Telegram signed, which alone says who is playing. The rules are
// internal/shashka's, so a move is checked here whatever the page lets
// through, and the games, ratings and the queue for a random opponent are
// kept in the database, as any instance may answer the next request.
package shashkaapp

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/shashka"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	// initDataMaxAge is how long the page may keep using the init data it
	// was opened with: a game can be long, and the page is not reloaded.
	initDataMaxAge = 24 * time.Hour
	// moveTime is how long a player of a game against a person has for
	// each move before losing on time.
	moveTime = 90 * time.Second
	// queueFresh is how long a wait for a random opponent lasts unless the
	// page asks again, which it does every few seconds.
	queueFresh = 10 * time.Second
	// topSize is how many players the table of ratings shows.
	topSize      = 20
	maxBodyBytes = 16 << 10
	maxNameChars = 32
	requestTime  = 10 * time.Second
)

// Store keeps the players, the games and the queue.
type Store interface {
	SaveShashkaPlayer(ctx context.Context, userID int64, name, language string) (storage.ShashkaPlayer, error)
	ShashkaPlayer(ctx context.Context, userID int64) (storage.ShashkaPlayer, error)
	ShashkaTop(ctx context.Context, limit int) ([]storage.ShashkaPlayer, error)
	ShashkaGame(ctx context.Context, id int64) (storage.ShashkaGame, error)
	OpenShashkaGame(ctx context.Context, userID int64) (storage.ShashkaGame, bool, error)
	CreateShashkaGame(ctx context.Context, g storage.ShashkaGame) (storage.ShashkaGame, error)
	JoinShashkaGame(ctx context.Context, code string, userID int64, deadline time.Time) (storage.ShashkaGame, error)
	UpdateShashkaGame(ctx context.Context, g storage.ShashkaGame) (storage.ShashkaGame, error)
	DeleteShashkaInvite(ctx context.Context, id, userID int64) error
	QueueShashka(ctx context.Context, userID int64, rating int, fresh time.Duration, newGame func(first, second int64) storage.ShashkaGame) (storage.ShashkaGame, bool, error)
	LeaveShashkaQueue(ctx context.Context, userID int64) error
	InShashkaQueue(ctx context.Context, userID int64, fresh time.Duration) (bool, error)
}

// Messenger tells an inviter in their private chat that their invite was
// taken.
type Messenger interface {
	SendMessage(ctx context.Context, msg telegram.Outgoing) (int64, error)
}

// Options configures the handler. Messenger may be nil; Now and Pick
// default to the clock and math/rand.
type Options struct {
	BotToken    string
	BotUsername string
	// PageURL is the page's address, which the inviter's message opens.
	PageURL   string
	Store     Store
	Messenger Messenger
	Logger    *slog.Logger
	Now       func() time.Time
	Pick      func(n int) int
}

// NewHandler is the API, at POST /api/shashka.
func NewHandler(opts Options) http.Handler {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Pick == nil {
		opts.Pick = randomPick
	}
	return &handler{opts}
}

type handler struct{ opts Options }

// request is what the page sends; each action reads the fields it needs.
type request struct {
	InitData string `json:"init_data"`
	Action   string `json:"action"`
	Level    int    `json:"level"`
	Code     string `json:"code"`
	Game     int64  `json:"game"`
	Version  int    `json:"version"`
	Path     []int  `json:"path"`
}

// failure is an error the page is told of by its code.
type failure struct {
	status int
	code   string
}

func (f failure) Error() string { return f.code }

var (
	errNoInitData    = failure{http.StatusUnauthorized, "no_init_data"}
	errForged        = failure{http.StatusUnauthorized, "forged"}
	errStaleInitData = failure{http.StatusUnauthorized, "stale_init_data"}
	errBadRequest    = failure{http.StatusBadRequest, "bad_request"}
	errNotFound      = failure{http.StatusNotFound, "not_found"}
	errOwnGame       = failure{http.StatusConflict, "own_game"}
	errTaken         = failure{http.StatusConflict, "taken"}
	errStale         = failure{http.StatusConflict, "stale"}
	errIllegal       = failure{http.StatusUnprocessableEntity, "illegal"}
	errNotYourTurn   = failure{http.StatusConflict, "not_your_turn"}
	errOver          = failure{http.StatusConflict, "over"}
	errBusy          = failure{http.StatusServiceUnavailable, "busy"}
)

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
		return
	}
	var req request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		writeFailure(w, errBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), requestTime)
	defer cancel()
	p, err := h.player(ctx, req.InitData)
	if err != nil {
		h.fail(w, req.Action, err)
		return
	}
	answer, err := h.act(ctx, p, req)
	if err != nil {
		h.fail(w, req.Action, err)
		return
	}
	answer["ok"] = true
	writeJSON(w, http.StatusOK, answer)
}

// player is who the init data says opened the page, with their name and
// language kept as Telegram gives them now.
type player struct {
	storage.ShashkaPlayer
}

func (h *handler) player(ctx context.Context, raw string) (player, error) {
	if raw == "" {
		return player{}, errNoInitData
	}
	data, err := miniapp.Validate(raw, h.opts.BotToken, h.opts.Now(), initDataMaxAge)
	switch {
	case errors.Is(err, miniapp.ErrStale):
		return player{}, errStaleInitData
	case err != nil:
		return player{}, errForged
	}
	saved, err := h.opts.Store.SaveShashkaPlayer(ctx, data.User.ID, nameOf(data.User), data.User.Language)
	if err != nil {
		return player{}, err
	}
	return player{saved}, nil
}

func (h *handler) act(ctx context.Context, p player, req request) (map[string]any, error) {
	switch req.Action {
	case "hello":
		return h.hello(ctx, p)
	case "bot":
		return h.newBotGame(ctx, p, req.Level)
	case "invite":
		return h.invite(ctx, p)
	case "join":
		return h.join(ctx, p, req.Code)
	case "queue":
		return h.queue(ctx, p)
	case "unqueue":
		return map[string]any{}, h.opts.Store.LeaveShashkaQueue(ctx, p.UserID)
	case "state":
		g, err := h.game(ctx, p, req.Game)
		if err != nil {
			return nil, err
		}
		return h.answer(ctx, p, g)
	case "move":
		return h.move(ctx, p, req)
	case "resign":
		return h.resign(ctx, p, req.Game)
	case "cancel":
		return map[string]any{}, h.opts.Store.DeleteShashkaInvite(ctx, req.Game, p.UserID)
	case "top":
		top, err := h.top(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"top": top, "me": playerView(p.ShashkaPlayer)}, nil
	}
	return nil, errBadRequest
}

func (h *handler) hello(ctx context.Context, p player) (map[string]any, error) {
	answer := map[string]any{"me": playerView(p.ShashkaPlayer), "game": nil}
	g, open, err := h.opts.Store.OpenShashkaGame(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	if open {
		if g, err = h.timeOut(ctx, g); err != nil {
			return nil, err
		}
		view, err := h.view(ctx, p, g)
		if err != nil {
			return nil, err
		}
		answer["game"] = view
	}
	top, err := h.top(ctx)
	if err != nil {
		return nil, err
	}
	queued, err := h.opts.Store.InShashkaQueue(ctx, p.UserID, queueFresh)
	if err != nil {
		return nil, err
	}
	answer["top"], answer["queued"] = top, queued
	return answer, nil
}

// free readies the player for a new game: their invite no one took is
// taken back, a game against the bot is given up and the wait for a random
// opponent left. A game against a person in play is not given up for
// another; it is returned, and the page shows it instead.
func (h *handler) free(ctx context.Context, p player) (storage.ShashkaGame, bool, error) {
	if err := h.opts.Store.LeaveShashkaQueue(ctx, p.UserID); err != nil {
		return storage.ShashkaGame{}, false, err
	}
	for range 3 {
		g, open, err := h.opts.Store.OpenShashkaGame(ctx, p.UserID)
		if err != nil || !open {
			return storage.ShashkaGame{}, false, err
		}
		switch {
		case g.Status == storage.ShashkaWaiting:
			err = h.opts.Store.DeleteShashkaInvite(ctx, g.ID, p.UserID)
		case g.Kind == kindBot:
			_, err = h.opts.Store.UpdateShashkaGame(ctx, ended(g, -sideOf(g, p.UserID), reasonResign))
		default:
			if g, err = h.timeOut(ctx, g); err != nil || g.Status != storage.ShashkaOver {
				return g, err == nil, err
			}
		}
		if err != nil && !errors.Is(err, storage.ErrShashkaStale) {
			return storage.ShashkaGame{}, false, err
		}
	}
	return storage.ShashkaGame{}, false, errBusy
}

func (h *handler) newBotGame(ctx context.Context, p player, level int) (map[string]any, error) {
	if level < int(shashka.Easy) || level > int(shashka.Hard) {
		return nil, errBadRequest
	}
	if busy, ok, err := h.free(ctx, p); err != nil || ok {
		return h.answerOr(ctx, p, busy, err)
	}
	g, err := h.opts.Store.CreateShashkaGame(ctx, storage.ShashkaGame{
		Kind: kindBot, Level: level, WhiteID: p.UserID, Position: shashka.Start().Encode(), Status: storage.ShashkaPlaying,
	})
	if err != nil {
		return nil, err
	}
	return h.answer(ctx, p, g)
}

func (h *handler) invite(ctx context.Context, p player) (map[string]any, error) {
	if busy, ok, err := h.free(ctx, p); err != nil || ok {
		return h.answerOr(ctx, p, busy, err)
	}
	g, err := NewInvite(ctx, h.opts.Store, p.UserID)
	if err != nil {
		return nil, err
	}
	return h.answer(ctx, p, g)
}

// InviteMaker keeps a new game; it is all NewInvite needs.
type InviteMaker interface {
	CreateShashkaGame(ctx context.Context, g storage.ShashkaGame) (storage.ShashkaGame, error)
}

// NewInvite keeps an invite of the player's, for a friend to take with its
// code; /shashka in a group makes one too.
func NewInvite(ctx context.Context, store InviteMaker, userID int64) (storage.ShashkaGame, error) {
	return store.CreateShashkaGame(ctx, storage.ShashkaGame{
		Kind: kindFriend, WhiteID: userID, Position: shashka.Start().Encode(), Status: storage.ShashkaWaiting,
		InviteCode: inviteCode(), Rated: true,
	})
}

func (h *handler) join(ctx context.Context, p player, code string) (map[string]any, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" || len(code) > 32 {
		return nil, errNotFound
	}
	// The player's own invite of this code is shown as it waits.
	if g, open, err := h.opts.Store.OpenShashkaGame(ctx, p.UserID); err != nil {
		return nil, err
	} else if open && g.InviteCode == code {
		return nil, errOwnGame
	}
	if busy, ok, err := h.free(ctx, p); err != nil || ok {
		return h.answerOr(ctx, p, busy, err)
	}
	g, err := h.opts.Store.JoinShashkaGame(ctx, code, p.UserID, h.opts.Now().Add(moveTime))
	switch {
	case errors.Is(err, storage.ErrNoShashkaGame):
		return nil, errNotFound
	case errors.Is(err, storage.ErrShashkaTaken):
		return nil, errTaken
	case errors.Is(err, storage.ErrShashkaOwnInvite):
		return nil, errOwnGame
	case err != nil:
		return nil, err
	}
	h.tellInviter(ctx, g, p)
	return h.answer(ctx, p, g)
}

// tellInviter writes to whoever sent the invite that it was taken, with a
// button back into the game, as they may have closed the page.
func (h *handler) tellInviter(ctx context.Context, g storage.ShashkaGame, joiner player) {
	if h.opts.Messenger == nil || h.opts.PageURL == "" {
		return
	}
	inviter, err := h.opts.Store.ShashkaPlayer(ctx, g.WhiteID)
	if err != nil {
		h.opts.Logger.Warn("could not read the shashka inviter", "user_id", g.WhiteID, "error", err)
		return
	}
	language := languageOf(inviter.Language)
	msg := telegram.Outgoing{
		ChatID:  g.WhiteID,
		Text:    textJoined.in(language, joiner.Name),
		Buttons: [][]telegram.Button{{{Text: textOpenGame.in(language), WebApp: &telegram.WebAppInfo{URL: h.opts.PageURL}}}},
	}
	if _, err := h.opts.Messenger.SendMessage(ctx, msg); err != nil {
		// They may never have written to the bot; the page shows it anyway.
		h.opts.Logger.Info("could not tell the shashka inviter", "user_id", g.WhiteID, "error", err)
	}
}

func (h *handler) queue(ctx context.Context, p player) (map[string]any, error) {
	// A game against a person in play is the one asked for, as the other
	// side's asking may have made it; anything else gives way to the wait.
	if g, open, err := h.opts.Store.OpenShashkaGame(ctx, p.UserID); err != nil {
		return nil, err
	} else if !open || g.Status != storage.ShashkaPlaying || g.Kind == kindBot {
		if busy, ok, err := h.free(ctx, p); err != nil || ok {
			return h.answerOr(ctx, p, busy, err)
		}
	}
	deadline := h.opts.Now().Add(moveTime)
	g, paired, err := h.opts.Store.QueueShashka(ctx, p.UserID, p.Rating, queueFresh, func(first, second int64) storage.ShashkaGame {
		white, black := first, second
		if h.opts.Pick(2) == 1 {
			white, black = second, first
		}
		return storage.ShashkaGame{
			Kind: kindRandom, WhiteID: white, BlackID: black, Position: shashka.Start().Encode(), Status: storage.ShashkaPlaying,
			Rated: true, Deadline: deadline,
		}
	})
	if err != nil {
		return nil, err
	}
	if !paired {
		return map[string]any{"queued": true}, nil
	}
	return h.answer(ctx, p, g)
}

// game is the game with this ID, if the player is in it, with a clock
// that ran out ended.
func (h *handler) game(ctx context.Context, p player, id int64) (storage.ShashkaGame, error) {
	g, err := h.opts.Store.ShashkaGame(ctx, id)
	if errors.Is(err, storage.ErrNoShashkaGame) || (err == nil && sideOf(g, p.UserID) == 0) {
		return storage.ShashkaGame{}, errNotFound
	}
	if err != nil {
		return storage.ShashkaGame{}, err
	}
	return h.timeOut(ctx, g)
}

// timeOut ends a game whose side to move let its clock run out: the other
// side wins. A game changed meanwhile is read again.
func (h *handler) timeOut(ctx context.Context, g storage.ShashkaGame) (storage.ShashkaGame, error) {
	for range 3 {
		if g.Status != storage.ShashkaPlaying || g.Deadline.IsZero() || h.opts.Now().Before(g.Deadline) {
			return g, nil
		}
		pos, err := shashka.Decode(g.Position)
		if err != nil {
			return storage.ShashkaGame{}, err
		}
		saved, err := h.opts.Store.UpdateShashkaGame(ctx, ended(g, -int(pos.Turn), reasonTimeout))
		if !errors.Is(err, storage.ErrShashkaStale) {
			return saved, err
		}
		if g, err = h.opts.Store.ShashkaGame(ctx, g.ID); err != nil {
			return storage.ShashkaGame{}, err
		}
	}
	return storage.ShashkaGame{}, errBusy
}

func (h *handler) move(ctx context.Context, p player, req request) (map[string]any, error) {
	g, err := h.game(ctx, p, req.Game)
	if err != nil {
		return nil, err
	}
	if g.Status != storage.ShashkaPlaying {
		return nil, errOver
	}
	if g.Version != req.Version {
		return nil, errStale
	}
	pos, err := shashka.Decode(g.Position)
	if err != nil {
		return nil, err
	}
	if int(pos.Turn) != sideOf(g, p.UserID) {
		return nil, errNotYourTurn
	}
	pos, played, err := pos.Play(req.Path)
	if err != nil {
		return nil, errIllegal
	}
	g = h.played(g, pos, played)
	if g.Status == storage.ShashkaPlaying && g.Kind == kindBot {
		reply := shashka.BestMove(pos, shashka.Level(g.Level), h.opts.Pick)
		if pos, played, err = pos.Play(reply.Path); err != nil {
			return nil, err
		}
		g = h.played(g, pos, played)
	}
	saved, err := h.opts.Store.UpdateShashkaGame(ctx, g)
	if errors.Is(err, storage.ErrShashkaStale) {
		return nil, errStale
	}
	if err != nil {
		return nil, err
	}
	return h.answer(ctx, p, saved)
}

// played is the game after a move: its position, the move to show, and
// either the next move's deadline or how the game ended.
func (h *handler) played(g storage.ShashkaGame, pos shashka.Position, m shashka.Move) storage.ShashkaGame {
	g.Position = pos.Encode()
	g.LastPath, g.Captured = slices.Clone(m.Path), slices.Clone(m.Captured)
	if g.Captured == nil {
		g.Captured = []int{}
	}
	switch pos.Result() {
	case shashka.WhiteWins:
		return ended(g, 1, reasonNoMoves)
	case shashka.BlackWins:
		return ended(g, -1, reasonNoMoves)
	case shashka.Draw:
		return ended(g, 0, reasonDrawRule)
	}
	if g.Kind != kindBot {
		g.Deadline = h.opts.Now().Add(moveTime)
	}
	return g
}

func (h *handler) resign(ctx context.Context, p player, id int64) (map[string]any, error) {
	g, err := h.game(ctx, p, id)
	if err != nil {
		return nil, err
	}
	switch g.Status {
	case storage.ShashkaWaiting:
		return map[string]any{}, h.opts.Store.DeleteShashkaInvite(ctx, g.ID, p.UserID)
	case storage.ShashkaOver:
		return h.answer(ctx, p, g)
	}
	saved, err := h.opts.Store.UpdateShashkaGame(ctx, ended(g, -sideOf(g, p.UserID), reasonResign))
	if errors.Is(err, storage.ErrShashkaStale) {
		return nil, errStale
	}
	if err != nil {
		return nil, err
	}
	return h.answer(ctx, p, saved)
}

// ended is the game won by winner, 1 white, -1 black, 0 neither.
func ended(g storage.ShashkaGame, winner int, reason string) storage.ShashkaGame {
	g.Status, g.Winner, g.Reason, g.Deadline = storage.ShashkaOver, winner, reason, time.Time{}
	return g
}

// sideOf is the player's side in the game: 1 white, -1 black, 0 not in it.
func sideOf(g storage.ShashkaGame, userID int64) int {
	switch userID {
	case g.WhiteID:
		return 1
	case g.BlackID:
		return -1
	}
	return 0
}

func (h *handler) top(ctx context.Context) ([]map[string]any, error) {
	top, err := h.opts.Store.ShashkaTop(ctx, topSize)
	if err != nil {
		return nil, err
	}
	views := make([]map[string]any, 0, len(top))
	for _, p := range top {
		views = append(views, playerView(p))
	}
	return views, nil
}

// answerOr answers with the game in play that stood in the way of a new
// one, or the error.
func (h *handler) answerOr(ctx context.Context, p player, g storage.ShashkaGame, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	return h.answer(ctx, p, g)
}

func (h *handler) answer(ctx context.Context, p player, g storage.ShashkaGame) (map[string]any, error) {
	view, err := h.view(ctx, p, g)
	if err != nil {
		return nil, err
	}
	return map[string]any{"game": view}, nil
}

func (h *handler) fail(w http.ResponseWriter, action string, err error) {
	var f failure
	if errors.As(err, &f) {
		writeFailure(w, f)
		return
	}
	h.opts.Logger.Error("shashka request failed", "action", action, "error", err)
	writeFailure(w, errBusy)
}

func writeFailure(w http.ResponseWriter, f failure) {
	writeJSON(w, f.status, map[string]any{"ok": false, "error": f.code})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// inviteCode is a code no one can guess, short enough for a /start link.
func inviteCode() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b) // never fails
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}

// nameOf is how a player is shown: their first name, or username, short.
func nameOf(u miniapp.User) string {
	name := strings.Join(strings.Fields(u.FirstName), " ")
	if name == "" {
		name = u.Username
	}
	if utf8.RuneCountInString(name) > maxNameChars {
		name = string([]rune(name)[:maxNameChars-1]) + "…"
	}
	return name
}
