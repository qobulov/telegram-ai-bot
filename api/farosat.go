package handler

import (
	"net/http"

	"telegram-ai-bot/serverless"
)

// Farosat answers the Farosat Mini App in public/farosat: a member's
// galaxies and the shop where farosat is bought with Stars. Vercel serves it
// at /api/farosat; each request is authenticated by the init data Telegram
// signs with the bot's token.
func Farosat(w http.ResponseWriter, r *http.Request) {
	serverless.Farosat(w, r)
}
