package raid

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// maxBodyBytes bounds a request, which is a session and a few numbers.
const maxBodyBytes = 8 << 10

// topSize is how many of a chat's members the page shows, most damage
// first.
const topSize = 8

// Sessions reads the session a press of Play opened the page with.
type Sessions interface {
	Session(token string) (game.Player, string, error)
}

// Store keeps the raids.
type Store interface {
	RaidState(ctx context.Context, chatID, userID int64, rules storage.RaidRules, seed uint32, top int) (storage.RaidBoss, storage.RaidHero, storage.RaidMember, []storage.RaidMember, error)
	HitRaid(ctx context.Context, chatID, userID int64, name string, taps int, now time.Time, rules storage.RaidRules, seed uint32) (storage.RaidHit, error)
	UpgradeRaidHero(ctx context.Context, userID int64, item string, cost func(level int) int64, most int, seed uint32) (storage.RaidHero, error)
}

// Telegram sets a member's score on their game message, and tells a
// group its boss was killed.
type Telegram interface {
	SetGameScore(ctx context.Context, game telegram.GameMessage, score int) (improved bool, err error)
	SendMessage(ctx context.Context, msg telegram.Outgoing) (int64, error)
}

// Options configures the handler. Now and Seed default to the clock and
// crypto/rand.
type Options struct {
	Sessions Sessions
	Store    Store
	Telegram Telegram
	Logger   *slog.Logger
	Now      func() time.Time
	Seed     func() uint32
}

type handler struct{ Options }

// NewHandler serves Dev jangi's page: "state" is the chat's boss, the
// player's hero and the chat's top members; "hit" counts taps; "upgrade"
// buys a level of an upgrade.
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
	Taps    int    `json:"taps"`
	Item    string `json:"item"`
}

type failure struct {
	status int
	code   string
}

var (
	errMethod      = failure{http.StatusMethodNotAllowed, "method"}
	errBadRequest  = failure{http.StatusBadRequest, "bad_request"}
	errSession     = failure{http.StatusUnauthorized, "session"}
	errCoins       = failure{http.StatusConflict, "coins"}
	errMax         = failure{http.StatusConflict, "max"}
	errUnavailable = failure{http.StatusBadGateway, "unavailable"}
)

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	result, fail := h.serve(r)
	if fail != (failure{}) {
		w.WriteHeader(fail.status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fail.code})
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
	if err != nil || name != game.BossShortName {
		return nil, errSession
	}
	switch req.Action {
	case "state":
		return h.state(r.Context(), player)
	case "hit":
		return h.hit(r.Context(), player, req.Taps)
	case "upgrade":
		return h.upgrade(r.Context(), player, req.Item)
	}
	return nil, errBadRequest
}

func (h *handler) state(ctx context.Context, p game.Player) (any, failure) {
	boss, hero, me, top, err := h.Store.RaidState(ctx, chatOf(p), p.UserID, Rules, h.Seed(), topSize)
	if err != nil {
		h.Logger.Error("could not read a raid", "user_id", p.UserID, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{
		"boss": bossView(boss), "hero": heroView(hero), "me": memberView(me),
		"top": topView(top, p.UserID), "taps_per_second": TapsPerSecond, "crit_times": critTimes,
	}, failure{}
}

// hit counts the taps, then sets the member's total as their Telegram
// score when it is due and tells a group its boss was killed; neither
// keeps the hit from counting if Telegram fails.
func (h *handler) hit(ctx context.Context, p game.Player, taps int) (any, failure) {
	hit, err := h.Store.HitRaid(ctx, chatOf(p), p.UserID, p.Name, taps, h.Now(), Rules, h.Seed())
	if err != nil {
		h.Logger.Error("could not count a raid hit", "user_id", p.UserID, "error", err)
		return nil, errUnavailable
	}
	if hit.ScoreDue {
		score := int(min(hit.Member.Total, math.MaxInt32))
		if _, err := h.Telegram.SetGameScore(ctx, message(p), score); err != nil {
			h.Logger.Warn("could not set a raid score", "user_id", p.UserID, "error", err)
		}
	}
	view := map[string]any{
		"boss": bossView(hit.Boss), "hero": heroView(hit.Hero), "me": memberView(hit.Member),
		"counted": hit.Counted, "damage": hit.Damage,
	}
	if hit.Killed != nil {
		view["killed"] = killView(*hit.Killed, p.UserID)
		if chatOf(p) < 0 {
			h.announce(ctx, chatOf(p), *hit.Killed)
		}
	}
	return view, failure{}
}

func (h *handler) upgrade(ctx context.Context, p game.Player, item string) (any, failure) {
	if Most(item) == 0 {
		return nil, errBadRequest
	}
	hero, err := h.Store.UpgradeRaidHero(ctx, p.UserID, item, func(level int) int64 { return Cost(item, level) }, Most(item), h.Seed())
	switch {
	case errors.Is(err, storage.ErrRaidCoins):
		return nil, errCoins
	case errors.Is(err, storage.ErrRaidMax):
		return nil, errMax
	case err != nil:
		h.Logger.Error("could not upgrade a raid hero", "user_id", p.UserID, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{"hero": heroView(hero)}, failure{}
}

// announce tells the group its boss was killed, by whom, and what comes
// next.
func (h *handler) announce(ctx context.Context, chatID int64, kill storage.RaidKill) {
	var b strings.Builder
	fmt.Fprintf(&b, "⚔️ %s (%d-daraja) yengildi!\n\n🏆 Eng ko'p urganlar:\n", bossNames[kind(kill.Level)], kill.Level)
	medals := []string{"🥇", "🥈", "🥉"}
	for i, f := range kill.Fighters[:min(len(kill.Fighters), 5)] {
		medal := "▫️"
		if i < len(medals) {
			medal = medals[i]
		}
		fmt.Fprintf(&b, "%s %s — %s zarba (+%d 💰)\n", medal, plain(f.Name), short(f.Damage), f.Reward)
	}
	if more := len(kill.Fighters) - 5; more > 0 {
		fmt.Fprintf(&b, "…va yana %d kishi\n", more)
	}
	fmt.Fprintf(&b, "\nKeyingisi: %s (%d-daraja). Jangga: /boss", bossNames[kind(kill.Level+1)], kill.Level+1)
	if _, err := h.Telegram.SendMessage(ctx, telegram.Outgoing{ChatID: chatID, Text: b.String()}); err != nil {
		h.Logger.Warn("could not announce a raid kill", "chat_id", chatID, "error", err)
	}
}

// bossNames are the bosses, taken in turn by level.
var bossNames = [Kinds]string{"Ko'k dev", "Qizil dev", "Bir ko'zli dev", "Ajdar", "Qora dev"}

func kind(level int) int {
	return (level - 1) % Kinds
}

// chatOf is the chat whose boss the player hits: a game sent inline has
// none, so its player fights their own.
func chatOf(p game.Player) int64 {
	if p.ChatID != 0 {
		return p.ChatID
	}
	return p.UserID
}

func message(p game.Player) telegram.GameMessage {
	return telegram.GameMessage{UserID: p.UserID, ChatID: p.ChatID, MessageID: p.MessageID, InlineMessageID: p.InlineMessageID}
}

func bossView(b storage.RaidBoss) map[string]any {
	return map[string]any{"level": b.Level, "hp": b.HP, "max_hp": b.MaxHP, "kind": kind(b.Level)}
}

func heroView(h storage.RaidHero) map[string]any {
	return map[string]any{
		"coins": h.Coins, "sword": h.Sword, "crit": h.Crit, "helper": h.Helper, "seed": h.Seed, "taps": h.Taps,
		"tap_damage": TapDamage(h.Sword), "crit_chance": CritChance(h.Crit), "helper_dps": HelperDPS(h.Helper),
		"next": map[string]any{
			"tap_damage": TapDamage(h.Sword + 1), "crit_chance": CritChance(h.Crit + 1), "helper_dps": HelperDPS(h.Helper + 1),
		},
		"costs": map[string]int64{"sword": Cost("sword", h.Sword), "crit": Cost("crit", h.Crit), "helper": Cost("helper", h.Helper)},
		"max":   map[string]bool{"sword": h.Sword >= maxSword, "crit": h.Crit >= maxCrit, "helper": h.Helper >= maxHelper},
	}
}

func memberView(m storage.RaidMember) map[string]any {
	return map[string]any{"damage": m.Damage, "total": m.Total}
}

func topView(top []storage.RaidMember, userID int64) []map[string]any {
	rows := make([]map[string]any, 0, len(top))
	for _, m := range top {
		rows = append(rows, map[string]any{"id": m.UserID, "name": m.Name, "damage": m.Damage, "me": m.UserID == userID})
	}
	return rows
}

func killView(k storage.RaidKill, userID int64) map[string]any {
	fighters := make([]map[string]any, 0, len(k.Fighters))
	for _, f := range k.Fighters[:min(len(k.Fighters), topSize)] {
		fighters = append(fighters, map[string]any{"name": f.Name, "damage": f.Damage, "reward": f.Reward, "me": f.UserID == userID})
	}
	var reward int64
	for _, f := range k.Fighters {
		if f.UserID == userID {
			reward = f.Reward
		}
	}
	return map[string]any{"level": k.Level, "kind": kind(k.Level), "fighters": fighters, "reward": reward}
}

// plain is a name with the characters that would make a message rich
// taken out.
func plain(name string) string {
	name = strings.NewReplacer("*", "", "_", "", "`", "", "[", "", "]", "").Replace(name)
	if name == "" {
		return "?"
	}
	return name
}

// short writes damage the way the page does: 52.3k, 1.24M.
func short(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%dk", n/1000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

func newSeed() uint32 {
	var b [4]byte
	_, _ = crand.Read(b[:])
	return binary.LittleEndian.Uint32(b[:]) | 1
}
