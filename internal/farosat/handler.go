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
	FarosatGalaxies(ctx context.Context, chatID, userID int64) ([]int, error)
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
	{"g5", 5, "Uchqun"},
	{"g10", 10, "Fikr"},
	{"g20", 20, "Zehn"},
	{"g50", 50, "Zakovat"},
	{"g100", 100, "Idrok"},
	{"g200", 200, "Donolik"},
	{"g300", 300, "Hikmat"},
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

// galaxyPrefix starts a galaxy invoice's payload, "frsg:<user>:<chat>:<id>".
const galaxyPrefix = "frsg:"

// GalaxyPayload is the payload of an invoice opening galaxy for userID in
// the group chatID.
func GalaxyPayload(userID, chatID int64, galaxy Galaxy) string {
	return fmt.Sprintf("%s%d:%d:%d", galaxyPrefix, userID, chatID, galaxy.ID)
}

// ParseGalaxyPayload reads a galaxy invoice's payload; ok is false for any
// other, or a galaxy not for sale.
func ParseGalaxyPayload(payload string) (userID, chatID int64, galaxy Galaxy, ok bool) {
	rest, found := strings.CutPrefix(payload, galaxyPrefix)
	if !found {
		return 0, 0, Galaxy{}, false
	}
	parts := strings.Split(rest, ":")
	if len(parts) != 3 {
		return 0, 0, Galaxy{}, false
	}
	userID, errUser := strconv.ParseInt(parts[0], 10, 64)
	chatID, errChat := strconv.ParseInt(parts[1], 10, 64)
	id, errID := strconv.Atoi(parts[2])
	galaxy, ok = GalaxyByID(id)
	if errUser != nil || errChat != nil || errID != nil || chatID >= 0 || !ok || galaxy.Stars() == 0 {
		return 0, 0, Galaxy{}, false
	}
	return userID, chatID, galaxy, true
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
	Galaxy   int    `json:"galaxy"`
}

// galaxyView is a galaxy as the page shows it, open or not.
type galaxyView struct {
	Galaxy
	Open   bool `json:"open"`
	Bought bool `json:"bought"`
	Price  int  `json:"stars"`
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
	errOpen        = failure{http.StatusConflict, "open"}
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
	case "galaxy_invoice":
		return h.galaxyInvoice(r.Context(), opened.User, chatID, req.Galaxy)
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
	bought, err := h.boughtGalaxies(ctx, chatID, userID)
	if err != nil {
		return nil, errUnavailable
	}
	out := state{Grams: grams, Place: place, Galaxies: make([]galaxyView, len(Galaxies)), Packs: Packs}
	for i, g := range Galaxies {
		open := grams >= g.Grams || bought[g.ID]
		if open {
			out.Open++
		}
		out.Galaxies[i] = galaxyView{Galaxy: g, Open: open, Bought: bought[g.ID], Price: g.Stars()}
	}
	return out, failure{}
}

// boughtGalaxies are the galaxies the member opened for Stars in the group.
func (h *handler) boughtGalaxies(ctx context.Context, chatID, userID int64) (map[int]bool, error) {
	ids, err := h.Store.FarosatGalaxies(ctx, chatID, userID)
	if err != nil {
		h.Logger.Error("farosat app: could not read bought galaxies", "user_id", userID, "chat_id", chatID, "error", err)
		return nil, err
	}
	bought := make(map[int]bool, len(ids))
	for _, id := range ids {
		bought[id] = true
	}
	return bought, nil
}

// galaxyInvoice makes the link to open a galaxy for Stars; one already open
// is not sold.
func (h *handler) galaxyInvoice(ctx context.Context, user miniapp.User, chatID int64, id int) (any, failure) {
	galaxy, ok := GalaxyByID(id)
	if !ok || galaxy.Stars() == 0 || h.Invoices == nil {
		return nil, errBadRequest
	}
	grams, _, err := h.Store.FarosatOf(ctx, chatID, user.ID)
	if err != nil {
		return nil, errUnavailable
	}
	bought, err := h.boughtGalaxies(ctx, chatID, user.ID)
	if err != nil {
		return nil, errUnavailable
	}
	if grams >= galaxy.Grams || bought[galaxy.ID] {
		return nil, errOpen
	}
	link, err := h.Invoices.CreateInvoiceLink(ctx, telegram.Invoice{
		Title:       "🌌 " + galaxy.Name,
		Description: fmt.Sprintf("Farosat ilovasida %s galaktikasi sizga ochiladi va doim ochiq qoladi.", galaxy.Name),
		Payload:     GalaxyPayload(user.ID, chatID, galaxy),
		Label:       galaxy.Name,
		Price:       galaxy.Stars(),
	})
	if err != nil {
		h.Logger.Error("farosat app: could not make a galaxy invoice", "user_id", user.ID, "galaxy", id, "error", err)
		return nil, errUnavailable
	}
	return map[string]any{"link": link}, failure{}
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
