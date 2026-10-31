package game

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// maxBodyBytes bounds a request: a play of Tarozi's answers are a few
// kilobytes, a long one of Kosmik ilon's turns some tens.
const maxBodyBytes = 256 << 10

// Telegram keeps the game's scores on each game message, and has the
// players' photos.
type Telegram interface {
	SetGameScore(ctx context.Context, game telegram.GameMessage, score int) (improved bool, err error)
	GetGameHighScores(ctx context.Context, game telegram.GameMessage) ([]telegram.GameHighScore, error)
	Avatar(ctx context.Context, userID, maxBytes int64) (data []byte, ok bool, err error)
}

// Records keeps the scored plays in the bot's own database too, each
// player's by game and chat, and makes each game's table across all chats
// from them.
type Records interface {
	RecordGameScore(ctx context.Context, score storage.GameScore) error
	GameTable(ctx context.Context, game string, userID int64, limit int) ([]storage.GameRank, error)
}

// tableSize is how many players a game's table from the records shows,
// before the player's own row.
const tableSize = 10

type handler struct {
	game     *Game
	telegram Telegram
	records  Records
	logger   *slog.Logger
}

// Handler serves the pages' requests: "start" deals a play for a session,
// of the game it is for, "finish" scores one, sets the score and records
// it, and "table" is the game's table: from records across all chats, or
// when records is nil or fails, the game message's from Telegram. A GET
// with an avatar token is that player's photo.
func (g *Game) Handler(tg Telegram, records Records, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &handler{game: g, telegram: tg, records: records, logger: logger}
}

type request struct {
	Action string `json:"action"`
	// Session is the token the page was opened with, for start and table;
	// Run the one start gave, for finish.
	Session string `json:"session"`
	Run     string `json:"run"`
	Answers string `json:"answers"`
	Times   []int  `json:"times"`
	// Turns and Ticks are a play of Kosmik ilon's, for finish; Sides and
	// Times one of O'tinchi's.
	Turns []int  `json:"turns"`
	Ticks int    `json:"ticks"`
	Sides string `json:"sides"`
}

type started struct {
	Run     string  `json:"run"`
	Seconds int     `json:"seconds"`
	Rounds  []Round `json:"rounds"`
}

type startedIlon struct {
	Run string `json:"run"`
	IlonPlay
}

type startedOtinchi struct {
	Run string `json:"run"`
	OtinchiPlay
}

type finished struct {
	Score    int   `json:"score"`
	Improved bool  `json:"improved"`
	Table    []row `json:"table"`
}

type row struct {
	Position int    `json:"position"`
	Name     string `json:"name"`
	Score    int    `json:"score"`
	Me       bool   `json:"me"`
	Avatar   string `json:"avatar"`
}

type failure struct {
	status int
	code   string
}

var (
	errMethod      = failure{http.StatusMethodNotAllowed, "method"}
	errBadRequest  = failure{http.StatusBadRequest, "bad_request"}
	errToken       = failure{http.StatusUnauthorized, "session"}
	errImplausible = failure{http.StatusUnprocessableEntity, "implausible"}
	errUnavailable = failure{http.StatusBadGateway, "unavailable"}
)

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Query().Has("avatar") {
		h.avatar(w, r)
		return
	}
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
	switch req.Action {
	case "start":
		return h.start(req)
	case "finish":
		return h.finish(r.Context(), req)
	case "table":
		return h.table(r.Context(), req)
	}
	return nil, errBadRequest
}

func (h *handler) start(req request) (any, failure) {
	s, err := h.game.session(req.Session)
	if err != nil {
		return nil, errToken
	}
	switch s.Game {
	case IlonShortName:
		run, play, err := h.game.StartIlon(req.Session)
		if err != nil {
			return nil, errToken
		}
		return startedIlon{Run: run, IlonPlay: play}, failure{}
	case OtinchiShortName:
		run, play, err := h.game.StartOtinchi(req.Session)
		if err != nil {
			return nil, errToken
		}
		return startedOtinchi{Run: run, OtinchiPlay: play}, failure{}
	}
	run, dealt, err := h.game.Start(req.Session)
	if err != nil {
		return nil, errToken
	}
	return started{Run: run, Seconds: Seconds, Rounds: dealt}, failure{}
}

// finish scores a play and sets the score on the game message, where
// Telegram keeps each player's best, then records it; the table follows.
func (h *handler) finish(ctx context.Context, req request) (any, failure) {
	var result Result
	var err error
	name := gameName(h.game.runGame(req.Run))
	switch name {
	case IlonShortName:
		result, err = h.game.FinishIlon(req.Run, req.Turns, req.Ticks)
	case OtinchiShortName:
		result, err = h.game.FinishOtinchi(req.Run, req.Sides, req.Times)
	default:
		result, err = h.game.Finish(req.Run, req.Answers, req.Times)
	}
	switch {
	case errors.Is(err, ErrToken):
		return nil, errToken
	case err != nil:
		h.logger.Warn("game play refused", "user_id", result.Player.UserID, "error", err)
		return nil, errImplausible
	}
	improved := false
	if result.Score > 0 {
		// A 0 would only put the player at the bottom of the table.
		improved, err = h.telegram.SetGameScore(ctx, message(result.Player), result.Score)
	}
	if err != nil {
		h.logger.Error("could not set a game score", "user_id", result.Player.UserID, "error", err)
		return nil, errUnavailable
	}
	h.record(ctx, name, result)
	table, err := h.rows(ctx, name, result.Player)
	if err != nil {
		// The score is set; only the table is missing.
		h.logger.Warn("could not read the game's scores", "user_id", result.Player.UserID, "error", err)
	}
	return finished{Score: result.Score, Improved: improved, Table: table}, failure{}
}

// gameName is the short name of a run's game: Tarozi's runs name none.
func gameName(name string) string {
	if name == "" {
		return ShortName
	}
	return name
}

// record keeps a scored play in the database; Telegram has the score
// already, so a failure is only logged.
func (h *handler) record(ctx context.Context, name string, result Result) {
	if h.records == nil {
		return
	}
	p := result.Player
	score := storage.GameScore{Game: name, ChatID: p.ChatID, UserID: p.UserID, Name: p.Name, Score: result.Score, At: h.game.now()}
	if err := h.records.RecordGameScore(ctx, score); err != nil {
		h.logger.Warn("could not record a game score", "user_id", p.UserID, "error", err)
	}
}

func (h *handler) table(ctx context.Context, req request) (any, failure) {
	player, name, err := h.game.Session(req.Session)
	if err != nil {
		return nil, errToken
	}
	table, err := h.rows(ctx, name, player)
	if err != nil {
		h.logger.Warn("could not read the game's scores", "user_id", player.UserID, "error", err)
		return nil, errUnavailable
	}
	return map[string][]row{"table": table}, failure{}
}

// rows is the game's table for the player: from the records, or the game
// message's from Telegram when there are none or they fail.
func (h *handler) rows(ctx context.Context, name string, player Player) ([]row, error) {
	if h.records != nil {
		ranks, err := h.records.GameTable(ctx, name, player.UserID, tableSize)
		if err == nil {
			table := make([]row, 0, len(ranks))
			for _, r := range ranks {
				table = append(table, row{Position: r.Position, Name: r.Name, Score: r.Best, Me: r.UserID == player.UserID, Avatar: h.game.AvatarPath(r.UserID)})
			}
			return table, nil
		}
		h.logger.Warn("could not read the game's table", "game", name, "error", err)
	}
	scores, err := h.telegram.GetGameHighScores(ctx, message(player))
	if err != nil {
		return []row{}, err
	}
	table := make([]row, 0, len(scores))
	for _, s := range scores {
		table = append(table, row{Position: s.Position, Name: s.User.FirstName, Score: s.Score, Me: s.User.ID == player.UserID, Avatar: h.game.AvatarPath(s.User.ID)})
	}
	return table, nil
}

func message(p Player) telegram.GameMessage {
	return telegram.GameMessage{UserID: p.UserID, ChatID: p.ChatID, MessageID: p.MessageID, InlineMessageID: p.InlineMessageID}
}
