package webapi

import (
	"strings"
	"testing"
)

// The test clock is 2026-10-16 12:00 in Tashkent.
const (
	mplStandings = `{"standings":[{"rank":1,"team":{"name":"TLPH","full_name":null},"match_point":12,"match_wl":{"win":12,"lose":2},"net_game_win":19,"game_wl":{"win":25,"lose":6}}]}`
	mplSchedule  = `{"weeks":[{"week":1,"days":[{"matches":[
		{"start_at":"2026-08-21T09:00:00+00:00","team1":{"name":"RORA","full_name":"Aurora Gaming"},"team2":{"name":"ONIC","full_name":null},"score1":0,"score2":2,"best_of":3,"status":"finished","winner":"team2"},
		{"start_at":"2026-10-10T09:00:00+00:00","team1":{"name":"TNC"},"team2":{"name":"OMG"},"score1":null,"score2":null,"best_of":3,"status":"scheduled","winner":""},
		{"start_at":"2026-10-20T09:00:00+00:00","team1":{"name":"ONIC"},"team2":{"name":"TLPH"},"score1":null,"score2":null,"best_of":3,"status":"scheduled","winner":""}]}]}]}`
	mplPlayoffsDone = `{"days":[{"matches":[
		{"start_at":"2026-10-21T07:00:00+00:00","team1":{"name":"ONIC"},"team2":{"name":"TNC"},"score1":3,"score2":2,"best_of":5,"status":"finished","winner":"team1"},
		{"start_at":"2026-10-25T07:00:00+00:00","team1":{"name":"TLPH"},"team2":{"name":"RORA","full_name":"Aurora Gaming"},"score1":3,"score2":4,"round":"Grand Finals","best_of":7,"status":"finished","winner":"team2"}]}]}`
	mplPlayoffsOn = `{"days":[{"matches":[
		{"start_at":"2026-10-21T07:00:00+00:00","team1":{"name":"ONIC"},"team2":{"name":"TNC"},"score1":3,"score2":2,"best_of":5,"status":"finished","winner":"team1"},
		{"start_at":"2026-10-25T07:00:00+00:00","team1":{"name":"ONIC"},"team2":{"name":"RORA"},"best_of":7,"status":"scheduled","winner":""}]}]}`
)

func TestMPLStandings(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/arena/api/esports/ph/standings": ok(mplStandings)})

	env := run(t, u.options(), "mpl", `{"action":"standings","league":"ph"}`)

	if !env.OK || !strings.Contains(env.raw, `"matches":"12-2"`) || !strings.Contains(env.raw, `"team":"TLPH"`) || !strings.Contains(env.raw, `"league":"MPL Philippines"`) {
		t.Errorf("standings = %s", env.raw)
	}
	wantError(t, run(t, u.options(), "mpl", `{"action":"standings","league":"us"}`), "invalid_arguments", "league must be")
}

// A team's matches: its results newest first, its next match, and a match
// past its time without a result left out of the next ones.
func TestMPLMatchesOfATeam(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/arena/api/esports/ph/schedule": ok(mplSchedule), "/arena/api/esports/ph/playoffs": ok(mplPlayoffsOn)})

	env := run(t, u.options(), "mpl", `{"action":"matches","league":"ph","team":"onic"}`)

	for _, want := range []string{`"match":"Aurora Gaming vs ONIC"`, `"score":"0:2"`, `"winner":"ONIC"`, `"when":"2026-08-21 14:00"`, `"next_matches":[{"best_of":3,"match":"ONIC vs TLPH"`} {
		if !env.OK || !strings.Contains(env.raw, want) {
			t.Errorf("matches lack %s: %s", want, env.raw)
		}
	}
	env = run(t, u.options(), "mpl", `{"action":"matches","league":"ph"}`)
	if !strings.Contains(env.raw, `"not_recorded":"1 matches`) || strings.Contains(env.raw[strings.Index(env.raw, "next_matches"):], "TNC vs OMG") {
		t.Errorf("a past match without a result = %s", env.raw)
	}
	wantError(t, run(t, u.options(), "mpl", `{"action":"matches","league":"ph","team":"Saber"}`), "invalid_arguments", "no MPL Philippines team")
}

func TestMPLPlayoffsChampion(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/arena/api/esports/ph/playoffs": ok(mplPlayoffsDone), "/arena/api/esports/my/playoffs": ok(mplPlayoffsOn)})

	env := run(t, u.options(), "mpl", `{"action":"playoffs","league":"ph"}`)
	if !env.OK || !strings.Contains(env.raw, `"champion":"Aurora Gaming"`) || !strings.Contains(env.raw, `"round":"Grand Finals"`) {
		t.Errorf("finished playoffs = %s", env.raw)
	}
	env = run(t, u.options(), "mpl", `{"action":"playoffs","league":"my"}`)
	if !env.OK || strings.Contains(env.raw, "champion") {
		t.Errorf("unfinished playoffs named a champion: %s", env.raw)
	}
}
