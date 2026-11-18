package agent

import "testing"

func TestCurrentCommands(t *testing.T) {
	for answer, want := range map[string]string{
		"Kartalaring /cards da, top /cardtop da": "Kartalaring /card da, top /card da",
		"/mycards yoz":                           "/card yoz",
		"Bozor: /market. Duel: /duel!":           "Bozor: /card. Duel: /card!",
		"/cards@gemini_max1bot ni bos":           "/card@gemini_max1bot ni bos",
		"/cardsx is no command":                  "/cardsx is no command",
		"see https://x/cards and x.com/market":   "see https://x/cards and x.com/market",
		"Карталар «/cards» орқали, таклифлар /myinvites": "Карталар «/card» орқали, таклифлар /invites",
		"Haftalik: /invites_week, oylik: /invites_month": "Haftalik: /invites, oylik: /invites",
		"/invites o'zi bor":        "/invites o'zi bor",
		"/profile — huquqlaring":   "/rights — huquqlaring",
		"Ирисда .дк деб ёзинг":     "Ирисда /access деб ёзинг",
		"/card allaqachon to'g'ri": "/card allaqachon to'g'ri",
		"(/duel)":                  "(/card)",
		"a/cards":                  "a/cards",
	} {
		if got := currentCommands(answer); got != want {
			t.Errorf("currentCommands(%q) = %q, want %q", answer, got, want)
		}
	}
}
