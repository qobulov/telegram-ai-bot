package mafia

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"telegram-ai-bot/internal/telegram"
)

// muteFor is how long a fallen player is kept from writing in the group
// at most: the game's end lets them write again, and should a game be
// left unfinished, Telegram lifts it by itself.
const muteFor = time.Hour

// silence keeps the fallen from writing in the group till the game ends,
// leaving out admins, whom Telegram does not restrict, and members someone
// restricted already, whose restriction is not the game's to lift; it
// marks in the game whom it muted, so that the end lets only them write
// again, and tells the group once a game when the bot may not do it.
func (h *handler) silence(ctx context.Context, chatID int64, round int, fallen []Player) {
	var ids []int64
	var names []string
	unruly := false
	for _, p := range fallen {
		ok, err := h.mute(ctx, chatID, p.ID)
		if err != nil {
			h.Logger.Warn("could not mute a fallen mafia player", "chat_id", chatID, "user_id", p.ID, "error", err)
			unruly = unruly || noRights(err)
		}
		if ok {
			ids = append(ids, p.ID)
			names = append(names, p.Name)
		}
	}
	warn, over := h.markMuted(ctx, chatID, round, ids, unruly)
	if over {
		// The game ended between the mute and its note: its end did not
		// know them.
		h.unmute(ctx, chatID, ids)
		names = nil
	}
	var lines []string
	if len(names) > 0 {
		lines = append(lines, "🔇 "+strings.Join(names, ", ")+" o'yin tugaguncha guruhda yoza olmaydi. So'nggi so'z — o'yin sahifasida.")
	}
	if warn {
		lines = append(lines, "⚠️ O'lganlarni o'yin tugaguncha jim qilish uchun botga guruhda «Foydalanuvchilarni cheklash» huquqini bering.")
	}
	if len(lines) > 0 {
		h.say(ctx, chatID, strings.Join(lines, "\n"))
	}
}

// mute keeps a member from writing in the group for muteFor, unless they
// are an admin or already restricted; ok reports that it did.
func (h *handler) mute(ctx context.Context, chatID, userID int64) (bool, error) {
	member, err := h.Telegram.GetChatMember(ctx, chatID, userID)
	if err != nil {
		return false, err
	}
	if member.Status != "member" {
		return false, nil
	}
	restriction := telegram.Restriction{ChatID: chatID, UserID: userID, Permissions: telegram.NoPermissions(), Until: h.Now().Add(muteFor)}
	if err := h.Telegram.RestrictChatMember(ctx, restriction); err != nil {
		return false, err
	}
	return true, nil
}

// markMuted notes in the game of round whom the bot muted, and that it may
// not; warn is true the first time it may not in that game, and over when
// the game is no longer going on.
func (h *handler) markMuted(ctx context.Context, chatID int64, round int, ids []int64, unruly bool) (warn, over bool) {
	if len(ids) == 0 && !unruly {
		return false, false
	}
	err := h.Store.ChangeMafiaGame(ctx, chatID, func(state []byte) ([]byte, error) {
		var g Game
		if err := json.Unmarshal(state, &g); err != nil {
			return nil, err
		}
		if g.Round != round || g.Phase == Over || g.Phase == Lobby || g.Phase == "" {
			over = true
			return state, nil
		}
		for _, id := range ids {
			if p := g.player(id); p != nil {
				p.Muted = true
			}
		}
		warn = unruly && !g.Unruly
		g.Unruly = g.Unruly || unruly
		return json.Marshal(g)
	})
	if err != nil {
		h.Logger.Error("could not note the muted mafia players", "chat_id", chatID, "error", err)
	}
	return warn, over
}

// unmute lets the players the game muted write in the group again.
func (h *handler) unmute(ctx context.Context, chatID int64, ids []int64) {
	for _, id := range ids {
		err := h.Telegram.RestrictChatMember(ctx, telegram.Restriction{ChatID: chatID, UserID: id, Permissions: telegram.AllPermissions()})
		if err != nil {
			h.Logger.Warn("could not unmute a mafia player", "chat_id", chatID, "user_id", id, "error", err)
		}
	}
}

// noRights reports Telegram's refusal for a bot that may not restrict
// members: not an admin, one without the right, or a group too small for
// restrictions.
func noRights(err error) bool {
	var apiErr *telegram.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	text := strings.ToLower(apiErr.Description)
	return strings.Contains(text, "rights") || strings.Contains(text, "supergroup")
}
