package webapi

import (
	"strings"
	"testing"
)

const (
	mplPlayerStats = `{"leaderboards":[{"title":"Kills Ranking","entries":[{"rank":1,"name":"KarlTzy","team":{"name":"TLPH","full_name":"Team Liquid PH"},"hero":null,"value":4.36}]}]}`
	mplHeroStats   = `{"leaderboards":[{"title":"Top 5 Hero Ban","entries":[{"rank":1,"name":"Freya","team":null,"hero":{"name":"Freya"},"value":148}]}]}`
	mplTeamStats   = `{"teams":[{"team":{"name":"ONIC","full_name":"ONIC PH"},"kills":394,"deaths":353,"lord":52,"tortoise":83,"tower":253}]}`
	mplMVP         = `{"standings":[{"rank":1,"name":"KEVIN","team":{"name":"TLID","full_name":"TEAM LIQUID ID"},"points":151}]}`
	mplPlayed      = `{"weeks":[{"days":[{"matches":[
		{"match_id":"rora-onic-20260821","start_at":"2026-08-21T09:00:00+00:00","team1":{"name":"RORA","full_name":"Aurora Gaming"},"team2":{"name":"ONIC"},"score1":0,"score2":2,"best_of":3,"status":"finished","winner":"team2"}]}]}]}`
	mplDetails = `{"match_id":"rora-onic-20260821","games":[{"number":1,"duration_estimate":"14:07","teams":[
		{"team":{"name":"RORA","full_name":"Aurora Gaming"},"winner":false,"kills":3,"players":[{"name":"Koyl","hero":{"name":"Hirara"},"kills":0,"deaths":2,"assists":1}]},
		{"team":{"name":"ONIC"},"winner":true,"kills":12,"players":[{"name":"Kelra","hero":{"name":"Claude"},"kills":6,"deaths":0,"assists":3}]}]}]}`
)

func TestMPLStats(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/arena/api/esports/ph/statistics/players": ok(mplPlayerStats),
		"/arena/api/esports/ph/statistics/heroes": ok(mplHeroStats), "/arena/api/esports/ph/statistics/teams": ok(mplTeamStats)})

	env := run(t, u.options(), "mpl", `{"action":"stats","league":"ph"}`)

	for _, want := range []string{`"Kills Ranking":["1. KarlTzy (Team Liquid PH): 4.36"]`, `"Top 5 Hero Ban":["1. Freya: 148"]`, `"ONIC PH: kills 394, deaths 353, lords 52, turtles 83, towers 253"`} {
		if !env.OK || !strings.Contains(env.raw, want) {
			t.Errorf("stats lack %s: %s", want, env.raw)
		}
	}
}

func TestMPLMVP(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/arena/api/esports/id/statistics/mvp": ok(mplMVP)})

	if env := run(t, u.options(), "mpl", `{"action":"mvp","league":"id"}`); !env.OK || !strings.Contains(env.raw, "1. KEVIN (TEAM LIQUID ID): 151 points") {
		t.Errorf("mvp = %s", env.raw)
	}
	wantError(t, run(t, u.options(), "mpl", `{"action":"mvp","league":"ph"}`), "invalid_arguments", "only MPL Indonesia and Cambodia")
}

func TestMPLMatchDetails(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/arena/api/esports/ph/schedule": ok(mplPlayed), "/arena/api/esports/ph/playoffs": ok(`{"days":[]}`),
		"/arena/api/esports/ph/matches/rora-onic-20260821": ok(mplDetails)})

	env := run(t, u.options(), "mpl", `{"action":"match","league":"ph","team":"onic","opponent":"aurora"}`)

	for _, want := range []string{`"score":"0:2"`, `"Koyl Hirara 0/2/1"`, `"Kelra Claude 6/0/3"`, `"won":true`, `"length":"14:07"`} {
		if !env.OK || !strings.Contains(env.raw, want) {
			t.Errorf("match lacks %s: %s", want, env.raw)
		}
	}
	wantError(t, run(t, u.options(), "mpl", `{"action":"match","league":"ph","team":"onic","opponent":"tnc"}`), "invalid_arguments", "no played")
}
