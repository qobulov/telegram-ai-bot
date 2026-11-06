package maydon

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

// prize is the Dev jangi coins each fighter of the winning side gets.
const prize = 100

// ShortName is Maydon's short name, as it is registered with @BotFather's
// /newgame.
const ShortName = game.MaydonShortName

// Sessions reads the session a press of Play opened the page with, and
// gives the address of a player's photo.
type Sessions interface {
	Session(token string) (game.Player, string, error)
	AvatarPath(userID int64) string
}

// Store keeps the rooms, knows the players' Dev jangi levels and pays the
// winners.
type Store interface {
	ChangeMaydonRoom(ctx context.Context, chatID int64, change func(state []byte) ([]byte, error)) error
	RaidLevels(ctx context.Context, userID int64) (sword, crit int, err error)
	AddRaidCoins(ctx context.Context, userIDs []int64, coins int64) error
}

// Telegram tells the group how the battle goes.
type Telegram interface {
	SendMessage(ctx context.Context, msg telegram.Outgoing) (int64, error)
}

// Options are what the handler needs; Now and Seed are the clock and the
// battle's randomness, set for tests.
type Options struct {
	Sessions Sessions
	Store    Store
	Telegram Telegram
	Logger   *slog.Logger
	Now      func() time.Time
	Seed     func() uint32
}

type handler struct{ Options }

// NewHandler serves Maydon's page: "state" is the room and the hits since
// the page's last; "join" (with a class), "leave" and "start" are the
// lobby's; "attack", "skill", "block" and "aim" (with a target) are the
// battle's. Each plays the battle on to the moment it came first.
func NewHandler(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Seed == nil {
		opts.Seed = newSeed
	}
	return &handler{opts}
}

type request struct {
	Session string `json:"session"`
	Action  string `json:"action"`
	Class   string `json:"class"`
	Target  int64  `json:"target"`
	Since   int    `json:"since"`
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
	ErrStarted: "started", ErrFull: "full", ErrClass: "class", ErrNotIn: "not_in", ErrTooFew: "too_few", ErrPhase: "phase",
	ErrDead: "dead", ErrEarly: "early", ErrEnergy: "energy", ErrBlock: "block", ErrTarget: "target",
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
	if err != nil || name != ShortName {
		return nil, errSession
	}
	if player.ChatID >= 0 {
		// The battle is a group's, not a lone player's or an inline one's.
		return nil, errGroup
	}
	ctx := r.Context()
	sword, crit := 1, 0
	if req.Action == "join" {
		if sword, crit, err = h.Store.RaidLevels(ctx, player.UserID); err != nil {
			h.Logger.Warn("could not read raid levels", "user_id", player.UserID, "error", err)
			sword, crit = 1, 0
		}
	}
	now := h.Now()
	var view map[string]any
	var events []Event
	var refused error
	err = h.Store.ChangeMaydonRoom(ctx, player.ChatID, func(state []byte) ([]byte, error) {
		r := Room{Winner: -1}
		if err := json.Unmarshal(state, &r); err != nil {
			return nil, err
		}
		events = r.Advance(now)
		var happened []Event
		happened, refused = h.apply(&r, req, player, sword, crit, now)
		events = append(events, happened...)
		view = h.view(&r, player.UserID, req.Since, now)
		return json.Marshal(r)
	})
	if err != nil {
		h.Logger.Error("could not change a maydon room", "chat_id", player.ChatID, "error", err)
		return nil, errUnavailable
	}
	h.tell(ctx, player.ChatID, events)
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
func (h *handler) apply(r *Room, req request, p game.Player, sword, crit int, now time.Time) ([]Event, error) {
	switch req.Action {
	case "state":
		return nil, nil
	case "join":
		opened := r.Phase != Lobby
		if err := r.Join(p.UserID, nameOf(p), req.Class, sword, crit, now); err != nil {
			return nil, err
		}
		if opened {
			return []Event{{Text: fmt.Sprintf("⚔️ %s Maydon xonasini ochdi! Qo'shilish uchun o'yindagi «Play» ni bosing (1v1 dan 5v5 gacha).", nameOf(p))}}, nil
		}
		return nil, nil
	case "leave":
		return nil, r.Leave(p.UserID)
	case "start":
		if err := r.Start(p.UserID, now, h.Seed()); err != nil {
			return nil, err
		}
		return []Event{{Text: "⚔️ Maydon boshlandi!\n🔴 " + strings.Join(r.names(0), ", ") + "\n🔵 " + strings.Join(r.names(1), ", ")}}, nil
	case "attack":
		return nil, r.Attack(p.UserID)
	case "skill":
		return nil, r.Skill(p.UserID)
	case "block":
		return nil, r.Block(p.UserID)
	case "aim":
		return nil, r.Aim(p.UserID, req.Target)
	}
	return nil, errors.New("maydon: no such action")
}

func (r *Room) names(team int) []string {
	var names []string
	for _, f := range r.Fighters {
		if f.Team == team {
			names = append(names, f.Name)
		}
	}
	return names
}

// tell sends the group the battle's news, and pays the winners when it
// ends; a failure does not undo the battle, so it is only logged.
func (h *handler) tell(ctx context.Context, chatID int64, events []Event) {
	for _, e := range events {
		text := e.Text
		if len(e.Winners) > 0 {
			if err := h.Store.AddRaidCoins(ctx, e.Winners, prize); err != nil {
				h.Logger.Error("could not pay maydon winners", "chat_id", chatID, "error", err)
			} else {
				text += fmt.Sprintf("\n\n🏆 G'oliblarga +%d 💰 Dev jangi tangasi. Yana jang: /maydon", prize)
			}
		}
		if _, err := h.Telegram.SendMessage(ctx, telegram.Outgoing{ChatID: chatID, Text: text}); err != nil {
			h.Logger.Warn("could not tell a group of its maydon battle", "chat_id", chatID, "error", err)
		}
	}
}

// view is the room as any page sees it, the hits since the page's last, and
// the player's own attack, energy, block and target.
func (h *handler) view(r *Room, me int64, since int, now time.Time) map[string]any {
	fighters := make([]map[string]any, 0, len(r.Fighters))
	for _, f := range r.Fighters {
		row := map[string]any{"id": f.ID, "name": f.Name, "class": f.Class, "sword": f.Sword, "crit": f.Crit, "avatar": h.Sessions.AvatarPath(f.ID)}
		if r.Phase != Lobby {
			row["team"], row["col"], row["row"] = f.Team, f.Col, f.Row
			row["hp"], row["max"], row["energy"], row["alive"] = f.HP, f.Max, f.Energy, f.Alive
			row["blocking"], row["dmg"], row["heal"] = f.BlockUntil > r.T, f.Dmg, f.Heal
			if f.ReviveAt > 0 {
				row["revive_in"] = f.ReviveAt - r.T
			}
		}
		fighters = append(fighters, row)
	}
	hits := []Hit{}
	for _, hit := range r.Hits {
		if hit.ID > since {
			hits = append(hits, hit)
		}
	}
	view := map[string]any{
		"phase": r.Phase, "round": r.Round, "seq": r.Seq, "t": r.T, "fighters": fighters, "hits": hits, "last_hit": r.NextHit,
		"winner": r.Winner, "best": r.Best, "max_players": MaxPlayers, "min_players": MinPlayers,
	}
	if r.Phase == Battle {
		view["left_ms"] = max(0, battleMs-int(now.Sub(r.Started).Milliseconds()))
	}
	f := r.fighter(me)
	if f == nil {
		view["me"] = map[string]any{"joined": false}
		return view
	}
	view["me"] = map[string]any{"joined": true, "id": f.ID, "class": f.Class, "team": f.Team, "cd": f.CD, "energy": f.Energy,
		"block_in": max(0, f.BlockReady-r.T), "target": f.Target, "alive": f.Alive || r.Phase == Lobby}
	return view
}

// nameOf is how a player is named to the others: their first name, at most
// 24 letters.
func nameOf(p game.Player) string {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return "Jangchi"
	}
	for utf8.RuneCountInString(name) > 24 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

func newSeed() uint32 {
	var b [4]byte
	_, _ = crand.Read(b[:])
	return binary.LittleEndian.Uint32(b[:])
}
