package handler

import (
	"net/http"

	"telegram-ai-bot/serverless"
)

// Cleanup is called once a day by Vercel Cron (see vercel.json) to delete
// chat history older than RETENTION_DAYS. Vercel serves it at /api/cleanup
// and authenticates its calls with CRON_SECRET.
func Cleanup(w http.ResponseWriter, r *http.Request) {
	serverless.Cleanup(w, r)
}
