package handler

import (
	"net/http"

	"telegram-ai-bot/serverless"
)

// Raid answers Dev jangi, the boss raid game in public/game/boss. Vercel
// serves it at /api/raid; each request carries a session the bot signed
// when Play was pressed.
func Raid(w http.ResponseWriter, r *http.Request) {
	serverless.Raid(w, r)
}
