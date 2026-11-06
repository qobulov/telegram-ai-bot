package handler

import (
	"net/http"

	"telegram-ai-bot/serverless"
)

// Mafia answers the Mafia game in public/game/mafiya. Vercel serves it at
// /api/mafia; each request carries a session the bot signed when Play was
// pressed.
func Mafia(w http.ResponseWriter, r *http.Request) {
	serverless.Mafia(w, r)
}
