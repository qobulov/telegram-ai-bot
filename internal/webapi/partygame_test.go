package webapi

import (
	"strings"
	"testing"
)

// Only PG questions are asked for, and a dare comes with the rule to keep it
// harmless and in the chat.
func TestPartyGame(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/tod/v1/truth": ok(`{"id":"a","type":"TRUTH","rating":"PG","question":"Are you good at smiling in photos?"}`),
		"/tod/v1/dare":  ok(`{"id":"b","type":"DARE","rating":"PG","question":"Post the oldest photo on your phone"}`),
		"/tod/v1/wyr":   ok(`{"id":"c","type":"WYR","rating":"PG","question":"Would you rather swim faster or run faster?"}`),
	})

	env := run(t, u.options(), "party_game", `{"kind":"truth"}`)
	if !env.OK || !strings.Contains(env.raw, `"question":"Are you good at smiling in photos?"`) {
		t.Errorf("truth = %s", env.raw)
	}
	if q := u.requests("/tod/v1/truth")[0].URL.Query(); q.Get("rating") != "pg" {
		t.Errorf("rating = %q, want pg", q.Get("rating"))
	}
	if env := run(t, u.options(), "party_game", `{"kind":"dare"}`); !strings.Contains(env.raw, "never one that has them share a private photo") {
		t.Errorf("dare = %s", env.raw)
	}
	if env := run(t, u.options(), "party_game", `{"kind":"would_you_rather"}`); !strings.Contains(env.raw, "create_poll") {
		t.Errorf("would you rather = %s", env.raw)
	}
	wantError(t, run(t, u.options(), "party_game", `{"kind":"kiss"}`), "invalid_arguments", "kind")
}
