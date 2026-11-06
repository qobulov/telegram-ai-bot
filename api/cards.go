package handler

import (
	"net/http"

	"telegram-ai-bot/serverless"
)

// Cards answers Kartalar, the anime card collection Mini App in
// public/kartalar. Vercel serves it at /api/cards; each request is
// authenticated by the init data Telegram signs with the bot's token.
func Cards(w http.ResponseWriter, r *http.Request) {
	serverless.Cards(w, r)
}
