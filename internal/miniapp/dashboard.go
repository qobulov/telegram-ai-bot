package miniapp

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
)

const (
	// initDataAge is how long init data opens the dashboard: a page left
	// open longer asks to be opened again, and a copy of the data stops
	// working as soon.
	initDataAge = time.Hour
	// listed is how many top writers and swearers are shown, and
	// groupsListed how many of a user's groups are offered.
	listed       = 10
	groupsListed = 20
	maxBodyBytes = 16 << 10
)

// Store reads what the dashboard shows: a group's statistics and guards,
// and the groups the bot is in.
type Store interface {
	GroupStats(ctx context.Context, chatID, botID int64, since time.Time, timeZone string, limit int) (storage.GroupStats, error)
	GroupRules(ctx context.Context, chatID int64) ([]storage.GroupRule, error)
	Groups(ctx context.Context, since time.Time, limit int) ([]storage.GroupSummary, error)
}

// Telegram says what a group is called.
type Telegram interface {
	GetChat(ctx context.Context, chatID int64) (telegram.Chat, error)
}

// Options configures the dashboard's handler. All but Now and Logger are
// required; without Owners nobody is let in.
type Options struct {
	// BotToken is what Telegram signs init data with.
	BotToken string
	// BotID is the bot's own user ID, whose messages are not counted.
	BotID    int64
	Store    Store
	Telegram Telegram
	// Owners are the bot's owners by Telegram ID, the only ones shown the
	// dashboard: it names who writes and who swears most, which the
	// groups' members are not to see.
	Owners map[int64]bool
	// Location is the time zone of "today" and of the hours.
	Location *time.Location
	Now      func() time.Time
	Logger   *slog.Logger
}

type handler struct {
	opts Options
}

// NewHandler returns the handler of the dashboard's requests.
func NewHandler(opts Options) (http.Handler, error) {
	if opts.BotToken == "" || opts.Store == nil || opts.Telegram == nil {
		return nil, errors.New("miniapp: a bot token, a store and Telegram are required")
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &handler{opts: opts}, nil
}

// request is what the page sends: its init data, as Telegram gave it, and
// the group and period it shows. Without a group, the one in the link that
// opened the app is shown, and without that, the bot's groups are listed.
type request struct {
	InitData string `json:"init_data"`
	Group    string `json:"group"`
	Period   string `json:"period"`
}

type groupLink struct {
	Group string `json:"group"`
	Title string `json:"title"`
}

type groupList struct {
	Groups []groupLink `json:"groups"`
}

type entry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type dashboard struct {
	Group    string   `json:"group"`
	Title    string   `json:"title"`
	Period   string   `json:"period"`
	Since    string   `json:"since"`
	Messages int      `json:"messages"`
	Members  int      `json:"members"`
	Hours    [24]int  `json:"hours"`
	Top      []entry  `json:"top"`
	Swearers []entry  `json:"swearers"`
	Guards   []string `json:"guards"`
}

// failure is an answer the page understands by its code.
type failure struct {
	status int
	code   string
}

var (
	errMethod      = failure{http.StatusMethodNotAllowed, "method"}
	errBadRequest  = failure{http.StatusBadRequest, "bad_request"}
	errForged      = failure{http.StatusUnauthorized, "unauthorized"}
	errStale       = failure{http.StatusUnauthorized, "expired"}
	errNotOwner    = failure{http.StatusForbidden, "not_owner"}
	errUnavailable = failure{http.StatusBadGateway, "unavailable"}
)

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	result, fail := h.serve(r)
	if fail != (failure{}) {
		writeJSON(w, fail.status, map[string]string{"error": fail.code})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *handler) serve(r *http.Request) (any, failure) {
	if r.Method != http.MethodPost {
		return nil, errMethod
	}
	var req request
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes)).Decode(&req); err != nil {
		return nil, errBadRequest
	}
	opened, err := Validate(req.InitData, h.opts.BotToken, h.opts.Now(), initDataAge)
	switch {
	case errors.Is(err, ErrStale):
		return nil, errStale
	case err != nil:
		return nil, errForged
	}
	if !h.opts.Owners[opened.User.ID] {
		return nil, errNotOwner
	}
	param := cmp.Or(req.Group, opened.StartParam)
	if param == "" {
		return h.groups(r.Context())
	}
	chatID, ok := ParseGroupParam(param)
	if !ok {
		return nil, errBadRequest
	}
	return h.dashboard(r.Context(), chatID, req.Period)
}

// groups lists the groups the bot is in, to choose from, the most
// recently active first.
func (h *handler) groups(ctx context.Context) (any, failure) {
	groups, err := h.opts.Store.Groups(ctx, h.opts.Now(), groupsListed)
	if err != nil {
		h.opts.Logger.Error("dashboard could not list the groups", "error", err)
		return nil, errUnavailable
	}
	list := groupList{Groups: []groupLink{}}
	for _, g := range groups {
		if g.BotStatus == "left" || g.BotStatus == "kicked" {
			continue
		}
		list.Groups = append(list.Groups, groupLink{Group: GroupParam(g.ChatID), Title: g.Title})
	}
	return list, failure{}
}

// dashboard is one group's statistics, for an owner.
func (h *handler) dashboard(ctx context.Context, chatID int64, period string) (any, failure) {
	now := h.opts.Now().In(h.opts.Location)
	since, err := tools.StatsSince(period, now)
	if err != nil {
		return nil, errBadRequest
	}
	stats, err := h.opts.Store.GroupStats(ctx, chatID, h.opts.BotID, since, h.opts.Location.String(), listed)
	if err != nil {
		h.opts.Logger.Error("dashboard could not count the group", "chat_id", chatID, "error", err)
		return nil, errUnavailable
	}
	rules, err := h.opts.Store.GroupRules(ctx, chatID)
	if err != nil {
		h.opts.Logger.Error("dashboard could not read the guards", "chat_id", chatID, "error", err)
		return nil, errUnavailable
	}
	title := ""
	if chat, err := h.opts.Telegram.GetChat(ctx, chatID); err == nil {
		title = chat.Title
	}
	return dashboard{
		Group:    GroupParam(chatID),
		Title:    title,
		Period:   cmp.Or(period, "today"),
		Since:    since.Format(time.RFC3339),
		Messages: stats.Messages,
		Members:  stats.Members,
		Hours:    stats.Hours,
		Top:      entries(stats.Top),
		Swearers: entries(stats.Swearers),
		Guards:   guardsOn(rules, chatID),
	}, failure{}
}

// guardNames are the guards an owner may turn on in a group, by the rule
// that turns each on, as the page names them.
var guardNames = []struct{ kind, name string }{
	{storage.RuleSwearGuard, "swear"},
	{storage.RuleSpamGuard, "spam"},
	{storage.RuleLinkGuard, "links"},
	{storage.RuleImageGuard, "images"},
	{storage.RuleJoinCaptcha, "joins"},
}

// guardsOn are the names of the guards on in a group, in guardNames' order.
func guardsOn(rules []storage.GroupRule, chatID int64) []string {
	on := []string{}
	for _, guard := range guardNames {
		for _, r := range rules {
			if r.ChatID == chatID && r.Kind == guard.kind {
				on = append(on, guard.name)
				break
			}
		}
	}
	return on
}

func entries(counts []storage.AuthorCount) []entry {
	list := make([]entry, 0, len(counts))
	for _, c := range counts {
		list = append(list, entry{Name: c.Name, Count: c.Count})
	}
	return list
}

// GroupParam is how a link to the dashboard names a group: "g" and the
// chat's ID without its minus, which a startapp parameter may not hold.
func GroupParam(chatID int64) string {
	return "g" + strconv.FormatInt(-chatID, 10)
}

// ParseGroupParam reads a group named by GroupParam.
func ParseGroupParam(param string) (int64, bool) {
	digits, ok := strings.CutPrefix(param, "g")
	if !ok || digits == "" || strings.ContainsFunc(digits, func(r rune) bool { return r < '0' || r > '9' }) {
		return 0, false
	}
	id, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return -id, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
