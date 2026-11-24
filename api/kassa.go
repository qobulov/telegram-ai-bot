package handler

import (
	"net/http"

	"telegram-ai-bot/serverless"
)

// Kassa answers Kassa, the cash book Mini App in public/kassa. Vercel serves
// it at /api/kassa; each request is authenticated by the init data Telegram
// signs with the bot's token.
func Kassa(w http.ResponseWriter, r *http.Request) {
	serverless.Kassa(w, r)
}
