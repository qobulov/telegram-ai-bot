package handler

import (
	"net/http"

	"telegram-ai-bot/serverless"
)

// Game deals and scores plays of the game in public/game. Vercel serves it
// at /api/game; each request carries a token the bot signed when Play was
// pressed.
func Game(w http.ResponseWriter, r *http.Request) {
	serverless.Game(w, r)
}
