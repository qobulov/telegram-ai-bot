// Package kassa serves Kassa, a cash book Mini App: the page in
// public/kassa is static and asks the handler here for the user's books and
// their money in (kirim) and out (chiqim). Whose books they are comes only
// from the init data Telegram signs with the bot's token, or from the
// session the bot signed for a press of the "kassa" game's Play.
package kassa

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/storage"
)

// Store is the cash books' storage.
type Store interface {
	KassaBooks(ctx context.Context, userID int64) ([]storage.KassaBook, error)
	KassaBook(ctx context.Context, userID, bookID int64) (storage.KassaBook, error)
	CreateKassaBook(ctx context.Context, userID int64, name string) (int64, error)
	RenameKassaBook(ctx context.Context, userID, bookID int64, name string) error
	DeleteKassaBook(ctx context.Context, userID, bookID int64) error
	AddKassaEntry(ctx context.Context, userID, bookID int64, e storage.KassaEntry) (int64, error)
	KassaEntries(ctx context.Context, userID, bookID int64, kind string, limit int) ([]storage.KassaEntry, error)
	DeleteKassaEntry(ctx context.Context, userID, id int64) error
}

// Sessions reads the session a game link opened the page with: a press of
// Play on the "kassa" game, whose page Telegram opens without init data.
type Sessions interface {
	Session(token string) (game.Player, string, error)
}

// Options configure the handler; Now defaults to time.Now, and Sessions nil
// takes init data alone.
type Options struct {
	BotToken string
	Store    Store
	Sessions Sessions
	Logger   *slog.Logger
	Now      func() time.Time
}

const (
	// initDataAge is how long the page's init data stays good.
	initDataAge  = 24 * time.Hour
	maxBodyBytes = 16 << 10
	// listLimit is how many entries of each kind the page shows.
	listLimit = 200
	// maxAmount keeps a mistyped amount from overflowing the sums.
	maxAmount = 1_000_000_000_000
	maxNote   = 300
	maxName   = 40
	// firstBook names the book a user without one is given.
	firstBook = "Asosiy kassa"
)

// tashkent is the day an entry is checked against; the users are there.
var tashkent = time.FixedZone("Asia/Tashkent", 5*60*60)

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
	Session  string `json:"session"`
	Action   string `json:"action"`
	Book     int64  `json:"book"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Amount   int64  `json:"amount"`
	Note     string `json:"note"`
	Day      string `json:"day"`
	ID       int64  `json:"id"`
}

type entry struct {
	ID     int64  `json:"id"`
	Amount int64  `json:"amount"`
	Note   string `json:"note"`
	Day    string `json:"day"`
}

type book struct {
	ID     int64               `json:"id"`
	Name   string              `json:"name"`
	Totals storage.KassaTotals `json:"totals"`
}

// bookState is one book open: its sums and entries.
type bookState struct {
	Book  book    `json:"book"`
	In    []entry `json:"in"`
	Out   []entry `json:"out"`
	Today string  `json:"today"`
}

// booksState is the list of books, with the sums of them all.
type booksState struct {
	Books  []book              `json:"books"`
	Totals storage.KassaTotals `json:"totals"`
	Max    int                 `json:"max"`
	Today  string              `json:"today"`
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
	errAmount      = failure{http.StatusBadRequest, "amount"}
	errDay         = failure{http.StatusBadRequest, "day"}
	errNote        = failure{http.StatusBadRequest, "note"}
	errName        = failure{http.StatusBadRequest, "name"}
	errBooksFull   = failure{http.StatusConflict, "books_full"}
	errNotFound    = failure{http.StatusNotFound, "not_found"}
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
	userID, fail := h.user(req)
	if fail != (failure{}) {
		return nil, fail
	}
	ctx := r.Context()
	switch req.Action {
	case "books":
		return h.books(ctx, userID)
	case "create_book":
		return h.createBook(ctx, userID, req.Name)
	case "rename_book":
		return h.renameBook(ctx, userID, req.Book, req.Name)
	case "delete_book":
		return h.deleteBook(ctx, userID, req.Book)
	case "state":
		return h.state(ctx, userID, req.Book)
	case "add":
		return h.add(ctx, userID, req)
	case "delete":
		return h.remove(ctx, userID, req.Book, req.ID)
	}
	return nil, errBadRequest
}

// user is whose books are asked for: from the init data of the Mini App, or
// from the session of a game link when there is none.
func (h *handler) user(req request) (int64, failure) {
	if req.InitData == "" && req.Session != "" && h.Sessions != nil {
		player, name, err := h.Sessions.Session(req.Session)
		switch {
		case errors.Is(err, game.ErrToken):
			return 0, errStale
		case err != nil || name != game.KassaShortName || player.UserID <= 0:
			return 0, errForged
		}
		return player.UserID, failure{}
	}
	opened, err := miniapp.Validate(req.InitData, h.BotToken, h.Now(), initDataAge)
	switch {
	case errors.Is(err, miniapp.ErrStale):
		return 0, errStale
	case err != nil:
		return 0, errForged
	}
	return opened.User.ID, failure{}
}

// books lists the user's books; a user without one is given the first, so
// that the page never opens empty.
func (h *handler) books(ctx context.Context, userID int64) (any, failure) {
	stored, err := h.Store.KassaBooks(ctx, userID)
	if err == nil && len(stored) == 0 {
		if _, err = h.Store.CreateKassaBook(ctx, userID, firstBook); err == nil {
			stored, err = h.Store.KassaBooks(ctx, userID)
		}
	}
	if err != nil {
		h.Logger.Error("kassa: books", "error", err)
		return nil, errUnavailable
	}
	out := booksState{Books: make([]book, 0, len(stored)), Max: storage.MaxKassaBooks, Today: h.today().Format(time.DateOnly)}
	for _, b := range stored {
		out.Books = append(out.Books, book{ID: b.ID, Name: b.Name, Totals: b.Totals})
		out.Totals.In += b.Totals.In
		out.Totals.Out += b.Totals.Out
	}
	out.Totals.Balance = out.Totals.In - out.Totals.Out
	return out, failure{}
}

func (h *handler) createBook(ctx context.Context, userID int64, name string) (any, failure) {
	name, fail := nameOf(name)
	if fail != (failure{}) {
		return nil, fail
	}
	_, err := h.Store.CreateKassaBook(ctx, userID, name)
	switch {
	case errors.Is(err, storage.ErrKassaBooksFull):
		return nil, errBooksFull
	case err != nil:
		h.Logger.Error("kassa: create book", "error", err)
		return nil, errUnavailable
	}
	return h.books(ctx, userID)
}

func (h *handler) renameBook(ctx context.Context, userID, bookID int64, name string) (any, failure) {
	name, fail := nameOf(name)
	if fail != (failure{}) {
		return nil, fail
	}
	if fail := h.bookFailure(h.Store.RenameKassaBook(ctx, userID, bookID, name), "rename book"); fail != (failure{}) {
		return nil, fail
	}
	return h.state(ctx, userID, bookID)
}

func (h *handler) deleteBook(ctx context.Context, userID, bookID int64) (any, failure) {
	if fail := h.bookFailure(h.Store.DeleteKassaBook(ctx, userID, bookID), "delete book"); fail != (failure{}) {
		return nil, fail
	}
	return h.books(ctx, userID)
}

// bookFailure is what a book's change that failed with err answers.
func (h *handler) bookFailure(err error, what string) failure {
	switch {
	case err == nil:
		return failure{}
	case errors.Is(err, storage.ErrNoKassaBook):
		return errNotFound
	}
	h.Logger.Error("kassa: "+what, "error", err)
	return errUnavailable
}

// nameOf is a book's name as the page sent it, trimmed: not empty, and
// short enough to fit a row.
func nameOf(name string) (string, failure) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > maxName {
		return "", errName
	}
	return name, failure{}
}

func (h *handler) add(ctx context.Context, userID int64, req request) (any, failure) {
	e, fail := h.entryOf(req)
	if fail != (failure{}) {
		return nil, fail
	}
	_, err := h.Store.AddKassaEntry(ctx, userID, req.Book, e)
	if fail := h.bookFailure(err, "add entry"); fail != (failure{}) {
		return nil, fail
	}
	return h.state(ctx, userID, req.Book)
}

func (h *handler) remove(ctx context.Context, userID, bookID, id int64) (any, failure) {
	err := h.Store.DeleteKassaEntry(ctx, userID, id)
	switch {
	case errors.Is(err, storage.ErrNoKassaEntry):
		return nil, errNotFound
	case err != nil:
		h.Logger.Error("kassa: delete entry", "error", err)
		return nil, errUnavailable
	}
	return h.state(ctx, userID, bookID)
}

// entryOf checks what the page sent: a known kind, a positive amount, a
// short note and a day no later than tomorrow in Tashkent.
func (h *handler) entryOf(req request) (storage.KassaEntry, failure) {
	if req.Kind != storage.KassaIn && req.Kind != storage.KassaOut {
		return storage.KassaEntry{}, errBadRequest
	}
	if req.Amount <= 0 || req.Amount > maxAmount {
		return storage.KassaEntry{}, errAmount
	}
	note := strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(note) > maxNote {
		return storage.KassaEntry{}, errNote
	}
	day, err := time.Parse(time.DateOnly, req.Day)
	tomorrow := h.today().AddDate(0, 0, 1)
	if err != nil || day.Year() < 2000 || day.After(tomorrow) {
		return storage.KassaEntry{}, errDay
	}
	return storage.KassaEntry{Kind: req.Kind, Amount: req.Amount, Note: note, Day: day}, failure{}
}

func (h *handler) today() time.Time {
	y, m, d := h.Now().In(tashkent).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func (h *handler) state(ctx context.Context, userID, bookID int64) (any, failure) {
	b, err := h.Store.KassaBook(ctx, userID, bookID)
	if fail := h.bookFailure(err, "book"); fail != (failure{}) {
		return nil, fail
	}
	in, err := h.entries(ctx, userID, bookID, storage.KassaIn)
	if err != nil {
		return nil, errUnavailable
	}
	out, err := h.entries(ctx, userID, bookID, storage.KassaOut)
	if err != nil {
		return nil, errUnavailable
	}
	return bookState{Book: book{ID: b.ID, Name: b.Name, Totals: b.Totals}, In: in, Out: out, Today: h.today().Format(time.DateOnly)}, failure{}
}

func (h *handler) entries(ctx context.Context, userID, bookID int64, kind string) ([]entry, error) {
	stored, err := h.Store.KassaEntries(ctx, userID, bookID, kind, listLimit)
	if err != nil {
		h.Logger.Error("kassa: entries", "kind", kind, "error", err)
		return nil, err
	}
	list := make([]entry, 0, len(stored))
	for _, e := range stored {
		list = append(list, entry{ID: e.ID, Amount: e.Amount, Note: e.Note, Day: e.Day.Format(time.DateOnly)})
	}
	return list, nil
}
