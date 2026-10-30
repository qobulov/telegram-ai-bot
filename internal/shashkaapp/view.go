package shashkaapp

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"

	"telegram-ai-bot/internal/shashka"
	"telegram-ai-bot/internal/storage"
)

// The kinds of game, and how one ended, as the page is told.
const (
	kindBot    = "bot"
	kindFriend = "friend"
	kindRandom = "random"

	reasonNoMoves  = "no_moves"
	reasonResign   = "resign"
	reasonTimeout  = "timeout"
	reasonDrawRule = "draw_rule"
)

func randomPick(n int) int { return rand.IntN(n) }

// view is the game as the page shows it to the player: the board, whose
// move it is with the player's legal moves when it is theirs, the last move,
// the clock, and how it ended with what it did to their rating.
func (h *handler) view(ctx context.Context, p player, g storage.ShashkaGame) (map[string]any, error) {
	pos, err := shashka.Decode(g.Position)
	if err != nil {
		return nil, err
	}
	white, err := h.sideView(ctx, p, g, g.WhiteID)
	if err != nil {
		return nil, err
	}
	var black any
	if g.Status != storage.ShashkaWaiting {
		if black, err = h.sideView(ctx, p, g, g.BlackID); err != nil {
			return nil, err
		}
	}
	mine := sideOf(g, p.UserID)
	moves := [][]int{}
	if g.Status == storage.ShashkaPlaying && int(pos.Turn) == mine {
		for _, m := range pos.Moves() {
			moves = append(moves, m.Path)
		}
	}
	view := map[string]any{
		"id":          g.ID,
		"kind":        g.Kind,
		"level":       g.Level,
		"status":      g.Status,
		"you":         colour(mine),
		"white":       white,
		"black":       black,
		"board":       pos.Board(),
		"turn":        colour(int(pos.Turn)),
		"moves":       moves,
		"last":        orEmpty(g.LastPath),
		"captured":    orEmpty(g.Captured),
		"version":     g.Version,
		"deadline_ms": h.left(g),
		"rated":       g.Rated,
		"invite":      nil,
		"result":      nil,
	}
	if g.Status == storage.ShashkaWaiting && g.InviteCode != "" {
		view["invite"] = map[string]any{"code": g.InviteCode, "link": InviteLink(h.opts.BotUsername, g.InviteCode)}
	}
	if g.Status == storage.ShashkaOver {
		change := g.WhiteChange
		if mine < 0 {
			change = g.BlackChange
		}
		var winner any
		if g.Winner != 0 {
			winner = colour(g.Winner)
		}
		view["result"] = map[string]any{"winner": winner, "reason": g.Reason, "rating_change": change}
	}
	return view, nil
}

// sideView names who plays a side: the bot at its level, or a player with
// their rating.
func (h *handler) sideView(ctx context.Context, p player, g storage.ShashkaGame, userID int64) (map[string]any, error) {
	if userID == 0 {
		return map[string]any{"name": botName, "rating": nil, "bot": true}, nil
	}
	who := p.ShashkaPlayer
	if userID != p.UserID {
		var err error
		if who, err = h.opts.Store.ShashkaPlayer(ctx, userID); err != nil {
			return nil, err
		}
	}
	return map[string]any{"name": who.Name, "rating": who.Rating, "bot": false}, nil
}

// left is how long the side to move has, in milliseconds; 0 without a
// clock.
func (h *handler) left(g storage.ShashkaGame) int64 {
	if g.Status != storage.ShashkaPlaying || g.Deadline.IsZero() {
		return 0
	}
	return max(g.Deadline.Sub(h.opts.Now()).Milliseconds(), 0)
}

func playerView(p storage.ShashkaPlayer) map[string]any {
	return map[string]any{"id": p.UserID, "name": p.Name, "rating": p.Rating, "wins": p.Wins, "losses": p.Losses, "draws": p.Draws}
}

func colour(side int) string {
	if side < 0 {
		return "black"
	}
	return "white"
}

func orEmpty(squares []int) []int {
	if squares == nil {
		return []int{}
	}
	return squares
}

// InvitePrefix starts the /start parameter of an invite's link.
const InvitePrefix = "sh_"

// InviteLink opens the bot's private chat with the invite's code, where
// the bot answers with a button into the game.
func InviteLink(botUsername, code string) string {
	return "https://t.me/" + botUsername + "?start=" + InvitePrefix + code
}

// localText is a text in Uzbek, Russian and English.
type localText struct{ uz, ru, en string }

// in is the text in language, filled with args.
func (t localText) in(language string, args ...any) string {
	text := t.uz
	switch language {
	case "ru":
		text = t.ru
	case "en":
		text = t.en
	}
	if len(args) > 0 {
		return fmt.Sprintf(text, args...)
	}
	return text
}

// languageOf is "ru" or "en" for a Telegram app in those, "uz" otherwise.
func languageOf(code string) string {
	switch {
	case strings.HasPrefix(code, "ru"):
		return "ru"
	case strings.HasPrefix(code, "en"):
		return "en"
	}
	return "uz"
}

var (
	textJoined   = localText{"♟ %s taklifingizni qabul qildi, o'yin boshlandi! Siz oqlar bilan yurasiz.", "♟ %s принял(а) ваше приглашение, игра началась! Вы ходите белыми.", "♟ %s took your invite, the game is on! You play white."}
	textOpenGame = localText{"♟ O'yinga kirish", "♟ Открыть игру", "♟ Open the game"}
)

// botName is the bot's side; the page shows its level next to it.
const botName = "Maria"
