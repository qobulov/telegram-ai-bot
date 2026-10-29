package game

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"telegram-ai-bot/internal/telegram"
)

// maxBodyBytes bounds a request: a play's answers are a few kilobytes.
const maxBodyBytes = 32 << 10

// Scores keeps the game's scores, which Telegram holds on each game
// message.
type Scores interface {
	SetGameScore(ctx context.Context, game telegram.GameMessage, score int) (improved bool, err error)
	GetGameHighScores(ctx context.Context, game telegram.GameMessage) ([]telegram.GameHighScore, error)
}

type handler struct {
	game   *Game
	scores Scores
	logger *slog.Logger
}

// Handler serves the page's requests: "start" deals a play for a session,
// "finish" scores one and sets the score, and "table" is the game
// message's table of scores.
func (g *Game) Handler(scores Scores, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &handler{game: g, scores: scores, logger: logger}
}

type request struct {
	Action string `json:"action"`
	// Session is the token the page was opened with, for start and table;
	// Run the one start gave, for finish.
	Session string `json:"session"`
	Run     string `json:"run"`
	Answers string `json:"answers"`
	Times   []int  `json:"times"`
}

type started struct {
	Run     string  `json:"run"`
	Seconds int     `json:"seconds"`
	Rounds  []Round `json:"rounds"`
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
	run, dealt, err := h.game.Start(req.Session)
	if err != nil {
		return nil, errToken
	}
	return started{Run: run, Seconds: Seconds, Rounds: dealt}, failure{}
}

// finish scores a play and sets the score on the game message, where
// Telegram keeps each player's best; the table follows.
func (h *handler) finish(ctx context.Context, req request) (any, failure) {
	result, err := h.game.Finish(req.Run, req.Answers, req.Times)
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
		improved, err = h.scores.SetGameScore(ctx, message(result.Player), result.Score)
	}
	if err != nil {
		h.logger.Error("could not set a game score", "user_id", result.Player.UserID, "error", err)
		return nil, errUnavailable
	}
	table, err := h.rows(ctx, result.Player)
	if err != nil {
		// The score is set; only the table is missing.
		h.logger.Warn("could not read the game's scores", "user_id", result.Player.UserID, "error", err)
	}
	return finished{Score: result.Score, Improved: improved, Table: table}, failure{}
}

func (h *handler) table(ctx context.Context, req request) (any, failure) {
	player, err := h.game.Player(req.Session)
	if err != nil {
		return nil, errToken
	}
	table, err := h.rows(ctx, player)
	if err != nil {
		h.logger.Warn("could not read the game's scores", "user_id", player.UserID, "error", err)
		return nil, errUnavailable
	}
	return map[string][]row{"table": table}, failure{}
}

func (h *handler) rows(ctx context.Context, player Player) ([]row, error) {
	scores, err := h.scores.GetGameHighScores(ctx, message(player))
	if err != nil {
		return []row{}, err
	}
	table := make([]row, 0, len(scores))
	for _, s := range scores {
		table = append(table, row{Position: s.Position, Name: s.User.FirstName, Score: s.Score, Me: s.User.ID == player.UserID})
	}
	return table, nil
}

func message(p Player) telegram.GameMessage {
	return telegram.GameMessage{UserID: p.UserID, ChatID: p.ChatID, MessageID: p.MessageID, InlineMessageID: p.InlineMessageID}
}
