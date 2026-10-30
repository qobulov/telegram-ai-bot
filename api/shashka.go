package handler

import (
	"net/http"

	"telegram-ai-bot/serverless"
)

// Shashka answers the draughts Mini App in public/shashka. Vercel serves it
// at /api/shashka; each request is authenticated by the init data Telegram
// signs with the bot's token.
func Shashka(w http.ResponseWriter, r *http.Request) {
	serverless.Shashka(w, r)
}
