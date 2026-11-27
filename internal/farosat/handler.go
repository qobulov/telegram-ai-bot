package farosat

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/telegram"
)

// Store is the groups' farosat.
type Store interface {
	FarosatOf(ctx context.Context, chatID, userID int64) (grams, place int, err error)
}

// Invoices makes the link a Stars payment is made at.
type Invoices interface {
	CreateInvoiceLink(ctx context.Context, inv telegram.Invoice) (string, error)
}

// Options configure the handler; Now defaults to time.Now.
type Options struct {
	BotToken string
	Store    Store
	Invoices Invoices
	Logger   *slog.Logger
	Now      func() time.Time
}

// Pack is farosat sold for Stars, a gram a Star, as the other bot sold it.
type Pack struct {
	ID    string `json:"id"`
	Stars int    `json:"stars"`
	Name  string `json:"name"`
}

// Grams are the grams a pack adds.
func (p Pack) Grams() int { return p.Stars }

// Packs are the packs on sale, the smallest first.
var Packs = []Pack{
	{"g50", 50, "Uchqun"},
	{"g100", 100, "Fikr"},
	{"g250", 250, "Zakovat"},
	{"g500", 500, "Daho"},
}

// PackByID is the pack sold under id.
func PackByID(id string) (Pack, bool) {
	for _, p := range Packs {
		if p.ID == id {
			return p, true
		}
	}
	return Pack{}, false
}

// payloadPrefix starts a farosat invoice's payload, which names who pays,
// for which group and which pack: "frs:<user>:<chat>:<pack>".
const payloadPrefix = "frs:"

// Payload is the payload of an invoice for a pack, made out to userID for
// the group chatID.
func Payload(userID, chatID int64, pack Pack) string {
	return fmt.Sprintf("%s%d:%d:%s", payloadPrefix, userID, chatID, pack.ID)
}

// ParsePayload reads a farosat invoice's payload; ok is false for any other.
func ParsePayload(payload string) (userID, chatID int64, pack Pack, ok bool) {
	rest, found := strings.CutPrefix(payload, payloadPrefix)
	if !found {
		return 0, 0, Pack{}, false
	}
	parts := strings.Split(rest, ":")
	if len(parts) != 3 {
		return 0, 0, Pack{}, false
	}
	userID, errUser := strconv.ParseInt(parts[0], 10, 64)
	chatID, errChat := strconv.ParseInt(parts[1], 10, 64)
	pack, ok = PackByID(parts[2])
	if errUser != nil || errChat != nil || chatID >= 0 || !ok {
		return 0, 0, Pack{}, false
	}
	return userID, chatID, pack, true
}

const (
	initDataAge  = 24 * time.Hour
	maxBodyBytes = 16 << 10
)

// NewHandler is the Mini App's API.
func NewHandler(opts Options) http.Handler {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	return &handler{opts}
}

type handler struct{ Options }

type request struct {
	InitData string `json:"init_data"`
	Group    string `json:"group"`
	Action   string `json:"action"`
	Pack     string `json:"pack"`
}

// galaxyView is a galaxy as the page shows it, open or not.
type galaxyView struct {
	Galaxy
	Open bool `json:"open"`
}

// state is the member's farosat in the group and the collection.
type state struct {
	Grams    int          `json:"grams"`
	Place    int          `json:"place"`
	Open     int          `json:"open"`
	Galaxies []galaxyView `json:"galaxies"`
	Packs    []Pack       `json:"packs"`
}

type failure struct {
	status int
	code   string
}

var (
	errMethod      = failure{http.StatusMethodNotAllowed, "method"}
	errBadRequest  = failure{http.StatusBadRequest, "bad_request"}
	errForged      = failure{http.StatusUnauthorized, "forged"}
	errStale       = failure{http.StatusUnauthorized, "stale"}
	errNoGroup     = failure{http.StatusBadRequest, "no_group"}
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
	opened, err := miniapp.Validate(req.InitData, h.BotToken, h.Now(), initDataAge)
	switch {
	case errors.Is(err, miniapp.ErrStale):
		return nil, errStale
	case err != nil:
		return nil, errForged
	}
	// The group is the one the button that opened the page was for: farosat
	// is kept a group at a time.
	chatID, ok := miniapp.ParseGroupParam(cmp.Or(req.Group, opened.StartParam))
	if !ok {
		return nil, errNoGroup
	}
	switch req.Action {
	case "state":
		return h.state(r.Context(), opened.User.ID, chatID)
	case "invoice":
		return h.invoice(r.Context(), opened.User, chatID, req.Pack)
	}
	return nil, errBadRequest
}

// state is the member's farosat, their place and the galaxies it opens.
func (h *handler) state(ctx context.Context, userID, chatID int64) (any, failure) {
	grams, place, err := h.Store.FarosatOf(ctx, chatID, userID)
	if err != nil {
		h.Logger.Error("farosat app: could not read farosat", "user_id", userID, "chat_id", chatID, "error", err)
		return nil, errUnavailable
	}
	out := state{Grams: grams, Place: place, Galaxies: make([]galaxyView, len(Galaxies)), Packs: Packs}
	for i, g := range Galaxies {
		open := grams >= g.Grams
		if open {
			out.Open++
		}
		out.Galaxies[i] = galaxyView{Galaxy: g, Open: open}
	}
	return out, failure{}
}

// invoice makes the link to pay for a pack in Stars, made out to the member
// for the group; the grams come when Telegram says the payment went through.
func (h *handler) invoice(ctx context.Context, user miniapp.User, chatID int64, id string) (any, failure) {
	pack, ok := PackByID(id)
	if !ok || h.Invoices == nil {
		return nil, errBadRequest
	}
	link, err := h.Invoices.CreateInvoiceLink(ctx, telegram.Invoice{
		Title:       fmt.Sprintf("🧠 %d gram farosat", pack.Grams()),
		Description: fmt.Sprintf("Guruhdagi farosatingizga %d gram qo'shiladi va reytingda hisoblanadi.", pack.Grams()),
		Payload:     Payload(user.ID, chatID, pack),
		Label:       fmt.Sprintf("%d gram farosat", pack.Grams()),
		Price:       pack.Stars,
	})
	if err != nil {
		h.Logger.Error("farosat app: could not make an invoice", "user_id", user.ID, "pack", id, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{"link": link}, failure{}
}
