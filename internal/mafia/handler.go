package mafia

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/telegram"
)

// maxBodyBytes bounds a request, which is a session and a few words.
const maxBodyBytes = 8 << 10

// prize is the Dev jangi coins each player of the winning side gets; the
// maniac, who wins alone, gets twice as many.
const prize = 150

// Sessions reads the session a press of Play opened the page with, and
// gives the address of a player's photo.
type Sessions interface {
	Session(token string) (game.Player, string, error)
	AvatarPath(userID int64) string
}

// Store keeps the games and pays the winners.
type Store interface {
	ChangeMafiaGame(ctx context.Context, chatID int64, change func(state []byte) ([]byte, error)) error
	AddRaidCoins(ctx context.Context, userIDs []int64, coins int64) error
}

// Telegram tells the group how the game goes, and keeps the fallen from
// writing in it till the game ends.
type Telegram interface {
	SendMessage(ctx context.Context, msg telegram.Outgoing) (int64, error)
	GetChatMember(ctx context.Context, chatID, userID int64) (telegram.ChatMember, error)
	RestrictChatMember(ctx context.Context, r telegram.Restriction) error
}

// Options are what the handler needs; Now and Intn are the clock and the
// shuffle, set for tests.
type Options struct {
	Sessions Sessions
	Store    Store
	Telegram Telegram
	Logger   *slog.Logger
	Now      func() time.Time
	Intn     func(n int) int
}

type handler struct{ Options }

// NewHandler serves Mafia's page: "state" is the game as the player may
// see it; "join", "leave" and "start" are the lobby's; "act" is a pick for
// one of the player's acts at night, "vote" a vote by day, "chat" a line in
// the page's chat open to them, and "speak" a fallen player's last word,
// told to the group. Each moves the game on first when its phase's time
// is up.
func NewHandler(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Intn == nil {
		opts.Intn = intn
	}
	return &handler{opts}
}

type request struct {
	Session string `json:"session"`
	Action  string `json:"action"`
	Act     string `json:"act"`
	Target  int64  `json:"target"`
	Words   string `json:"words"`
	Text    string `json:"text"`
	Room    string `json:"room"`
}

type failure struct {
	status int
	code   string
}

var (
	errMethod      = failure{http.StatusMethodNotAllowed, "method"}
	errBadRequest  = failure{http.StatusBadRequest, "bad_request"}
	errSession     = failure{http.StatusUnauthorized, "session"}
	errGroup       = failure{http.StatusConflict, "group"}
	errUnavailable = failure{http.StatusBadGateway, "unavailable"}
)

// refusals are the rules' refusals as the page reads them.
var refusals = map[error]string{
	ErrStarted: "started", ErrFull: "full", ErrNotIn: "not_in", ErrTooFew: "too_few", ErrPhase: "phase",
	ErrDead: "dead", ErrNoAct: "no_act", ErrTarget: "target", ErrRepeating: "repeating",
	ErrNoWords: "no_words", ErrWords: "words", ErrChat: "chat_closed", ErrLine: "line", ErrTooFast: "too_fast", ErrRoom: "room",
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	result, fail := h.serve(r)
	if fail != (failure{}) {
		w.WriteHeader(fail.status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": fail.code, "state": result})
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func (h *handler) serve(r *http.Request) (any, failure) {
	if r.Method != http.MethodPost {
		return nil, errMethod
	}
	var req request
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes)).Decode(&req); err != nil {
		return nil, errBadRequest
	}
	player, name, err := h.Sessions.Session(req.Session)
	if err != nil || name != game.MafiaShortName {
		return nil, errSession
	}
	if player.ChatID >= 0 {
		// A game of five or more is played in a group, not alone or inline.
		return nil, errGroup
	}
	ctx, now := r.Context(), h.Now()
	var view map[string]any
	var events []Event
	var refused error
	round := 0
	err = h.Store.ChangeMafiaGame(ctx, player.ChatID, func(state []byte) ([]byte, error) {
		var g Game
		if err := json.Unmarshal(state, &g); err != nil {
			return nil, err
		}
		events = g.Advance(now)
		var happened []Event
		happened, refused = h.apply(&g, req, player, now)
		events = append(events, happened...)
		events = append(events, g.Advance(now)...)
		view = h.view(&g, player.UserID, now)
		round = g.Round
		return json.Marshal(g)
	})
	if err != nil {
		h.Logger.Error("could not change a mafia game", "chat_id", player.ChatID, "error", err)
		return nil, errUnavailable
	}
	h.tell(ctx, player.ChatID, round, events)
	if refused != nil {
		code, ok := refusals[refused]
		if !ok {
			return nil, errBadRequest
		}
		return view, failure{http.StatusConflict, code}
	}
	return view, failure{}
}

// apply does what the player asked, and gives what the group is to be told
// of it.
func (h *handler) apply(g *Game, req request, p game.Player, now time.Time) ([]Event, error) {
	switch req.Action {
	case "state":
		return nil, nil
	case "join":
		opened := g.Phase != Lobby
		if err := g.Join(p.UserID, nameOf(p), now); err != nil {
			return nil, err
		}
		if opened {
			return []Event{{Text: fmt.Sprintf("🎭 %s Mafia xonasini ochdi! Qo'shilish uchun o'yindagi «Play» ni bosing (%d–%d kishi).", nameOf(p), MinPlayers, MaxPlayers)}}, nil
		}
		return nil, nil
	case "leave":
		return nil, g.Leave(p.UserID)
	case "start":
		if err := g.Start(p.UserID, now, h.Intn); err != nil {
			return nil, err
		}
		return []Event{{Text: fmt.Sprintf("🎭 Mafia boshlandi: %d o'yinchi. Rolingizni o'yin sahifasida ko'ring 🤫\n🌙 1-tun. Shahar uxlaydi, rollar uyg'onadi…", len(g.Players))}}, nil
	case "act":
		return nil, g.Act(p.UserID, req.Act, req.Target)
	case "vote":
		return nil, g.Vote(p.UserID, req.Target)
	case "chat":
		return nil, g.Say(p.UserID, req.Room, req.Text, now)
	case "speak":
		said, err := g.Speak(p.UserID, req.Words)
		if err != nil {
			return nil, err
		}
		return []Event{said}, nil
	}
	return nil, errors.New("mafia: no such action")
}

// tell sends the group the game's news, keeps the fallen from writing in
// it, and when the game ends pays the winners and lets the fallen write
// again; a failure does not undo the game, so it is only logged.
func (h *handler) tell(ctx context.Context, chatID int64, round int, events []Event) {
	for _, e := range events {
		text := e.Text
		if len(e.Winners) > 0 {
			coins := int64(prize)
			if e.Winner == Solo {
				coins *= 2
			}
			if err := h.Store.AddRaidCoins(ctx, e.Winners, coins); err != nil {
				h.Logger.Error("could not pay mafia winners", "chat_id", chatID, "error", err)
			} else {
				text += fmt.Sprintf("\n\n🏆 G'oliblarga +%d 💰 Dev jangi tangasi. Yangi o'yin: /mafia", coins)
			}
		}
		h.say(ctx, chatID, text)
		if len(e.Fallen) > 0 {
			h.silence(ctx, chatID, round, e.Fallen)
		}
		h.unmute(ctx, chatID, e.Unmute)
	}
}

func (h *handler) say(ctx context.Context, chatID int64, text string) {
	if _, err := h.Telegram.SendMessage(ctx, telegram.Outgoing{ChatID: chatID, Text: text}); err != nil {
		h.Logger.Warn("could not tell a group of its mafia game", "chat_id", chatID, "error", err)
	}
}

// view is the game as a player may see it: their own role, their mafia
// partners' and the dead's, what their checks found, their partners' picks
// for the kill, and their acts still to do; at the end, every role.
func (h *handler) view(g *Game, me int64, now time.Time) map[string]any {
	self := g.player(me)
	mafia := self != nil && TeamOf(self.Role) == Mafia
	players := make([]map[string]any, 0, len(g.Players))
	for _, p := range g.Players {
		row := map[string]any{"id": p.ID, "name": p.Name, "alive": p.Alive, "avatar": h.Sessions.AvatarPath(p.ID)}
		if p.Words != "" {
			row["words"] = p.Words
		}
		if p.Role != "" && (p.ID == me || !p.Alive || g.Phase == Over || (mafia && TeamOf(p.Role) == Mafia)) {
			row["role"], row["role_name"] = p.Role, RoleName(p.Role)
		}
		if mark, ok := g.Known[me][p.ID]; ok {
			row["mark"] = mark
		}
		players = append(players, row)
	}
	view := map[string]any{
		"phase": g.Phase, "day": g.Day, "seq": g.Seq, "round": g.Round, "players": players, "news": g.News, "winner": g.Winner,
		"left_ms": max(0, g.Ends.Sub(now).Milliseconds()), "min_players": MinPlayers, "max_players": MaxPlayers,
		"max_words": MaxWords, "phase_ms": phaseLength(g.Phase).Milliseconds(),
		"chat": g.Heard(me), "max_line": MaxLine,
	}
	if g.Phase == Vote {
		tally := map[int64]int{}
		for _, t := range g.Votes {
			tally[t]++
		}
		view["votes"] = tally
	}
	if self == nil {
		view["me"] = map[string]any{"joined": false}
		return view
	}
	mine := map[string]any{"joined": true, "alive": self.Alive, "voted": g.Votes[me], "can_speak": g.CanSpeak(me), "muted": self.Muted && g.Phase != Over, "room": g.RoomOf(me)}
	if self.Role != "" {
		mine["role"], mine["role_name"], mine["team"] = self.Role, RoleName(self.Role), TeamOf(self.Role)
	}
	var pending []map[string]any
	for _, act := range g.Pending(me) {
		pending = append(pending, map[string]any{"act": act, "targets": g.Targets(me, act)})
	}
	mine["pending"] = pending
	if mafia && g.Phase == Night {
		picks := map[int64]int{}
		for _, t := range g.Acts[actKill] {
			picks[t]++
		}
		mine["picks"] = picks
	}
	view["me"] = mine
	return view
}

// phaseLength is how long a phase lasts in all, for the page's bar of the
// time left.
func phaseLength(p Phase) time.Duration {
	return map[Phase]time.Duration{Lobby: lobbyFor, Night: nightFor, Day: dayFor, Vote: voteFor}[p]
}

// nameOf is how a player is named to the others: their first name, at most
// 32 letters.
func nameOf(p game.Player) string {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return "O'yinchi"
	}
	for utf8.RuneCountInString(name) > 32 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

func intn(n int) int {
	var b [8]byte
	_, _ = crand.Read(b[:])
	return int(binary.LittleEndian.Uint64(b[:]) % uint64(n))
}
