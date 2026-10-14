// Package handler is the Vercel functions. Vercel serves this one at
// /api/webhook; it receives Telegram updates by webhook.
//
// The long-polling command in cmd/bot is the other way to run the same bot;
// a bot token can use only one of the two at a time.
package handler

import (
	"net/http"
	_ "time/tzdata" // timezone data for get_current_time; the host has none

	"telegram-ai-bot/serverless"
)

// Handler handles one Telegram update.
func Handler(w http.ResponseWriter, r *http.Request) {
	serverless.Webhook(w, r)
}
