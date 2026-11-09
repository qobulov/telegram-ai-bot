package handler

import (
	"net/http"

	"telegram-ai-bot/serverless"
)

// Reminders is called every minute by an outside scheduler (cron-job.org;
// Vercel Cron runs only daily on this plan) to send the reminders that are
// due. Vercel serves it at /api/reminders; each call must carry
// "Authorization: Bearer <CRON_SECRET>".
func Reminders(w http.ResponseWriter, r *http.Request) {
	serverless.Reminders(w, r)
}
