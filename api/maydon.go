package handler

import (
	"net/http"

	"telegram-ai-bot/serverless"
)

// Maydon answers the Maydon team battle in public/game/maydon. Vercel
// serves it at /api/maydon; each request carries a session the bot signed
// when Play was pressed.
func Maydon(w http.ResponseWriter, r *http.Request) {
	serverless.Maydon(w, r)
}
