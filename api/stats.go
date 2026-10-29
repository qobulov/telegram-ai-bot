package handler

import (
	"net/http"

	"telegram-ai-bot/serverless"
)

// Stats answers the group dashboard, the Mini App in public/stats. Vercel
// serves it at /api/stats; each request is authenticated by the init data
// Telegram signs with the bot's token.
func Stats(w http.ResponseWriter, r *http.Request) {
	serverless.Stats(w, r)
}
