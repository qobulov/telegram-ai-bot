package game

import (
	"net/http"
)

// kindAvatar tokens name a player whose photo a page may show: the server
// signs one for each player it lists, so the pages can show no one else's.
const kindAvatar = "avatar"

// maxAvatarBytes bounds a photo; a small one is some kilobytes.
const maxAvatarBytes = 256 << 10

// avatarToken is a player and the last day, in days since 1970, their
// photo's address works on. The address is the same all day, so that
// browsers and Vercel's cache keep the photo.
type avatarToken struct {
	UserID int64 `json:"u"`
	Until  int64 `json:"d"`
}

// AvatarPath is the address of a player's photo for the pages, good until
// the end of tomorrow.
func (g *Game) AvatarPath(userID int64) string {
	return "/api/game?avatar=" + g.sign(kindAvatar, avatarToken{UserID: userID, Until: g.now().Unix()/86400 + 1})
}

// avatarUser is the player an avatar token names, while it works.
func (g *Game) avatarUser(token string) (int64, bool) {
	var a avatarToken
	if !g.verify(kindAvatar, token, &a) || a.UserID == 0 || g.now().Unix()/86400 > a.Until {
		return 0, false
	}
	return a.UserID, true
}

// avatar serves the photo an avatar token names, kept for a day by
// browsers and Vercel's cache. A player without one, or who hides it from
// the bot, is a 404, which the pages show as their initial; Telegram's
// address, which holds the bot's token, never reaches the page.
func (h *handler) avatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.game.avatarUser(r.URL.Query().Get("avatar"))
	if !ok {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "", http.StatusUnauthorized)
		return
	}
	data, found, err := h.telegram.Avatar(r.Context(), userID, maxAvatarBytes)
	if err != nil {
		h.logger.Warn("could not fetch an avatar", "user_id", userID, "error", err)
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "", http.StatusBadGateway)
		return
	}
	if !found {
		w.Header().Set("Cache-Control", "public, max-age=3600, s-maxage=3600")
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(data))
	w.Header().Set("Cache-Control", "public, max-age=86400, s-maxage=86400")
	_, _ = w.Write(data)
}
