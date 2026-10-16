package webapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type footballResult struct {
	Competition string `json:"competition"`
	Matchday    int    `json:"matchday"`
	Rows        []struct {
		Pos    int    `json:"pos"`
		Team   string `json:"team"`
		Played int    `json:"played"`
		Points int    `json:"points"`
		GD     int    `json:"gd"`
	} `json:"rows"`
	Matches []struct {
		Kickoff string `json:"kickoff_tashkent"`
		Home    string `json:"home"`
		Away    string `json:"away"`
		Score   string `json:"score"`
		Status  string `json:"status"`
	} `json:"matches"`
	Scorers []struct {
		Player  string `json:"player"`
		Team    string `json:"team"`
		Goals   int    `json:"goals"`
		Assists *int   `json:"assists"`
	} `json:"scorers"`
	Note   string `json:"note"`
	Source string `json:"source"`
}

const (
	plPath        = "/football/v4/competitions/PL"
	plTablePath   = plPath + "/standings"
	plMatchesPath = plPath + "/matches"
	clPath        = "/football/v4/competitions/CL"
	ecPath        = "/football/v4/competitions/EC"
)

func footballRoutes(t *testing.T) map[string]stub {
	return map[string]stub{
		plPath:                        ok(fixture(t, "football_competition_pl.json")),
		plTablePath:                   ok(fixture(t, "football_standings_pl.json")),
		plMatchesPath + "?matchday=7": ok(fixture(t, "football_matches_pl_7.json")),
		plMatchesPath + "?matchday=8": ok(fixture(t, "football_matches_pl_8.json")),
		plPath + "/scorers":           ok(fixture(t, "football_scorers_pl.json")),
		clPath:                        ok(fixture(t, "football_competition_cl.json")),
		clPath + "/matches?status=SCHEDULED,TIMED": ok(fixture(t, "football_matches_cl_scheduled.json")),
		// Hand-made from the v4 documentation of extra time and shootouts
		// (no key to capture a live one): Euro 2024's quarter-finals.
		ecPath:                                   ok(fixture(t, "football_competition_ec.json")),
		ecPath + "/matches?matchday=5":           ok(fixture(t, "football_matches_ec_5.json")),
		"/football/v4/competitions/PD/standings": {status: http.StatusForbidden, body: fixture(t, "football_403.json")},
	}
}

func footballOptions(u *upstream) Options {
	opts := u.options()
	opts.FootballToken = "fd-secret"
	return opts
}

func TestFootball(t *testing.T) {
	inProgress := strings.Replace(fixture(t, "football_matches_pl_7.json"), `"status": "FINISHED"`, `"status": "IN_PLAY"`, 1)
	tests := []struct {
		name     string
		override map[string]stub
		args     string
		check    func(t *testing.T, r footballResult, u *upstream)
	}{
		{
			name: "the league table",
			args: `{"action":"table"}`,
			check: func(t *testing.T, r footballResult, u *upstream) {
				first := r.Rows[0]
				if r.Competition != "Premier League" || len(r.Rows) != 10 || first.Pos != 1 || first.Team != "Arsenal" || first.Played != 7 || first.Points != 19 || first.GD != 20 {
					t.Errorf("result = %+v", r)
				}
				request := u.requests(plTablePath)[0]
				if request.Header.Get("X-Auth-Token") != "fd-secret" || strings.Contains(request.URL.String(), "fd-secret") {
					t.Errorf("the token must travel in the X-Auth-Token header only: %s", request.URL)
				}
				if !strings.Contains(r.Source, "football-data.org") {
					t.Errorf("source = %q", r.Source)
				}
			},
		},
		{
			name: "a team below the top ten is added to the table",
			args: `{"action":"table","competition":"APL","team":"Sunderland"}`,
			check: func(t *testing.T, r footballResult, _ *upstream) {
				if len(r.Rows) != 11 || r.Rows[10].Pos != 17 || r.Rows[10].Team != "Sunderland" {
					t.Errorf("rows = %+v", r.Rows)
				}
			},
		},
		{
			name: "a finished matchday moves on to the next",
			args: `{"action":"matches"}`,
			check: func(t *testing.T, r footballResult, u *upstream) {
				first := r.Matches[0]
				if r.Matchday != 8 || len(r.Matches) != 10 || first.Kickoff != "Sat 17 Oct 16:30" || first.Home != "Chelsea" || first.Away != "Man City" || first.Score != "" || first.Status != "scheduled" {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name: "a team's last result and next match",
			args: `{"action":"matches","team":"man city"}`,
			check: func(t *testing.T, r footballResult, _ *upstream) {
				if len(r.Matches) != 2 || r.Matches[0].Score != "3-0" || r.Matches[0].Status != "finished" || r.Matches[0].Kickoff != "Sat 03 Oct 19:00" || r.Matches[1].Away != "Man City" {
					t.Errorf("matches = %+v", r.Matches)
				}
			},
		},
		{
			name:     "a matchday under way stays",
			override: map[string]stub{plMatchesPath + "?matchday=7": ok(inProgress)},
			args:     `{"action":"matches","competition":"pl"}`,
			check: func(t *testing.T, r footballResult, u *upstream) {
				if r.Matchday != 7 || r.Matches[0].Status != "playing" || r.Matches[0].Score != "2-1" || len(u.requests(plMatchesPath)) != 1 {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name: "a cup between matchdays lists scheduled and timed matches",
			args: `{"action":"matches","competition":"Chempionlar ligasi"}`,
			check: func(t *testing.T, r footballResult, u *upstream) {
				if r.Competition != "UEFA Champions League" || len(r.Matches) != 14 || r.Matches[0].Home != "Real Madrid" || r.Matches[0].Kickoff != "Wed 21 Oct 21:45" || r.Note != "" {
					t.Errorf("result = %+v", r)
				}
				if got := u.requests(clPath + "/matches")[0].URL.Query().Get("status"); got != "SCHEDULED,TIMED" {
					t.Errorf("status = %q", got)
				}
			},
		},
		{
			name: "extra time and penalty shootouts",
			args: `{"action":"matches","competition":"Euro"}`,
			check: func(t *testing.T, r footballResult, _ *upstream) {
				want := []string{"2-1 (aet)", "0-0 (pen 3-5)", "1-1 (pen 5-3)", "2-1"}
				if len(r.Matches) != len(want) {
					t.Fatalf("matches = %+v", r.Matches)
				}
				for i, score := range want {
					if r.Matches[i].Score != score || r.Matches[i].Status != "finished" {
						t.Errorf("match %d = %+v, want score %q", i, r.Matches[i], score)
					}
				}
			},
		},
		{
			name: "Milan is not Inter",
			args: `{"action":"matches","competition":"CL","team":"Милан"}`,
			check: func(t *testing.T, r footballResult, _ *upstream) {
				if len(r.Matches) != 1 || r.Matches[0].Home != "Milan" {
					t.Errorf("matches = %+v", r.Matches)
				}
			},
		},
		{
			name: "part of a name is enough when nothing matches better",
			args: `{"action":"matches","competition":"CL","team":"Leverk"}`,
			check: func(t *testing.T, r footballResult, _ *upstream) {
				if len(r.Matches) != 1 || r.Matches[0].Away != "Leverkusen" {
					t.Errorf("matches = %+v", r.Matches)
				}
			},
		},
		{
			name: "a team that is not in the table",
			args: `{"action":"table","team":"Pakhtakor"}`,
			check: func(t *testing.T, r footballResult, _ *upstream) {
				if len(r.Rows) != 10 || !strings.Contains(r.Note, "Pakhtakor is not in this table") {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name: "team nicknames",
			args: `{"action":"matches","competition":"CL","team":"Barsa"}`,
			check: func(t *testing.T, r footballResult, _ *upstream) {
				if len(r.Matches) != 1 || r.Matches[0].Away != "Barça" {
					t.Errorf("matches = %+v", r.Matches)
				}
			},
		},
		{
			name: "a team without a match",
			args: `{"action":"matches","competition":"CL","team":"Pakhtakor"}`,
			check: func(t *testing.T, r footballResult, _ *upstream) {
				if len(r.Matches) != 0 || !strings.Contains(r.Note, "Pakhtakor") {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name: "top scorers",
			args: `{"action":"scorers"}`,
			check: func(t *testing.T, r footballResult, u *upstream) {
				if len(r.Scorers) != 5 || r.Scorers[0].Player != "Erling Haaland" || r.Scorers[0].Team != "Man City" || r.Scorers[0].Goals != 9 || r.Scorers[2].Assists != nil {
					t.Errorf("scorers = %+v", r.Scorers)
				}
				if got := u.requests(plPath + "/scorers")[0].URL.Query().Get("limit"); got != "5" {
					t.Errorf("limit = %q", got)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := footballRoutes(t)
			for path, answer := range test.override {
				routes[path] = answer
			}
			u := newUpstream(t, routes)
			env := run(t, footballOptions(u), "football", test.args)
			wantFits(t, env, registryLimit)
			test.check(t, result[footballResult](t, env), u)
		})
	}
}

func TestFootballFailures(t *testing.T) {
	tests := []struct {
		name     string
		override map[string]stub
		args     string
		code     string
		words    []string
	}{
		{name: "rate limited", override: map[string]stub{plTablePath: {status: http.StatusTooManyRequests, body: fixture(t, "football_429.json")}}, args: `{"action":"table"}`, code: "execution_failed", words: []string{"football-data.org is busy"}},
		{name: "slow", override: map[string]stub{plTablePath: slow}, args: `{"action":"table"}`, code: "timeout"},
		{name: "not on the free plan", args: `{"action":"table","competition":"PD"}`, code: "execution_failed", words: []string{"free plan"}},
		{name: "no table", override: map[string]stub{plTablePath: ok(`{"standings":[]}`)}, args: `{"action":"table"}`, code: "execution_failed", words: []string{"no table"}},
		{name: "unknown competition", args: `{"action":"table","competition":"Uzbekistan Super League"}`, code: "invalid_arguments", words: []string{"PL"}},
		{name: "unknown action", args: `{"action":"transfers"}`, code: "invalid_arguments"},
		{name: "no action", args: `{}`, code: "invalid_arguments"},
		{name: "a team name that is a paragraph", args: `{"action":"matches","team":"` + strings.Repeat("a", 61) + `"}`, code: "invalid_arguments"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := footballRoutes(t)
			for path, answer := range test.override {
				routes[path] = answer
			}
			wantError(t, run(t, impatient(footballOptions(newUpstream(t, routes))), "football", test.args), test.code, test.words...)
		})
	}
}

func TestFootballIsCached(t *testing.T) {
	u := newUpstream(t, footballRoutes(t))
	registry := newRegistry(t, footballOptions(u))

	for _, args := range []string{`{"action":"matches"}`, `{"action":"matches","team":"Arsenal"}`, `{"action":"table"}`, `{"action":"table","team":"Leeds"}`} {
		result[footballResult](t, execute(t, registry, "football", args))
	}
	if n := u.total(); n != 4 {
		t.Errorf("football-data.org was asked %d times, want 4 (competition, two matchdays, table)", n)
	}
}

func TestFootballNeedsAToken(t *testing.T) {
	opts := newUpstream(t, nil).options()
	if names := toolNames(Tools(opts)); strings.Contains(names, "football") {
		t.Errorf("tools without a token = %s", names)
	}
	opts.FootballToken = "fd-secret"
	if names := toolNames(Tools(opts)); !strings.Contains(names, "football") {
		t.Errorf("tools with a token = %s", names)
	}
}

// matchday is an answer with n timed matches between made-up teams.
func matchday(n int) string {
	var matches []string
	for i := range n {
		matches = append(matches, fmt.Sprintf(`{"utcDate":"2026-10-21T19:00:00Z","status":"TIMED","homeTeam":{"name":"Home %d FC","shortName":"Home %d"},"awayTeam":{"name":"Away %d FC","shortName":"Away %d"},"score":{"duration":"REGULAR","fullTime":{"home":null,"away":null}}}`, i, i, i, i))
	}
	return `{"competition":{"name":"UEFA Champions League"},"matches":[` + strings.Join(matches, ",") + `]}`
}

func TestFootballShowsAWholeMatchdayOrSaysWhatIsLeftOut(t *testing.T) {
	tests := []struct {
		matches, shown int
		note           string
	}{
		{matches: 18, shown: 18},
		{matches: 24, shown: 24},
		{matches: 30, shown: 24, note: "showing 24 of 30 matches"},
	}
	for _, test := range tests {
		routes := footballRoutes(t)
		routes[clPath+"/matches?status=SCHEDULED,TIMED"] = ok(matchday(test.matches))
		env := run(t, footballOptions(newUpstream(t, routes)), "football", `{"action":"matches","competition":"CL"}`)

		wantFits(t, env, registryLimit)
		r := result[footballResult](t, env)
		if len(r.Matches) != test.shown || (test.note == "") != (r.Note == "") || !strings.Contains(r.Note, test.note) {
			t.Errorf("%d matches: %d shown, note %q", test.matches, len(r.Matches), r.Note)
		}
	}
}

func TestTeamFilterPrefersTheClosestMatch(t *testing.T) {
	teams := []footballTeam{
		{Name: "Niger", TLA: "NIG"}, {Name: "Nigeria", TLA: "NGA"},
		{Name: "Guinea", TLA: "GUI"}, {Name: "Equatorial Guinea", TLA: "EQG"},
		{Name: "South Sudan", TLA: "SSD"}, {Name: "Sudan", TLA: "SDN"},
		{Name: "AC Milan", ShortName: "Milan", TLA: "MIL"}, {Name: "FC Internazionale Milano", ShortName: "Inter", TLA: "INT"},
		{Name: "FC Barcelona", ShortName: "Barça", TLA: "FCB"}, {Name: "FC Bayern München", ShortName: "Bayern", TLA: "FCB"},
	}
	tests := []struct {
		team string
		want []string
	}{
		{team: "Niger", want: []string{"Niger"}},
		{team: "nigeria", want: []string{"Nigeria"}},
		{team: "Guinea", want: []string{"Guinea"}},
		{team: "Sudan", want: []string{"Sudan"}},
		{team: "south sudan", want: []string{"South Sudan"}},
		{team: "milan", want: []string{"AC Milan"}},
		{team: "barsa", want: []string{"FC Barcelona"}},
		{team: "Bayern Munchen", want: []string{"FC Bayern München"}},
		{team: "internaz", want: []string{"FC Internazionale Milano"}},
		{team: "Pakhtakor"},
	}
	for _, test := range tests {
		filter := newTeamFilter(test.team, teams)
		var got []string
		for _, team := range teams {
			if filter.names(team) {
				got = append(got, team.Name)
			}
		}
		if strings.Join(got, ",") != strings.Join(test.want, ",") || filter.found() != (len(test.want) > 0) {
			t.Errorf("%q names %v, want %v", test.team, got, test.want)
		}
	}
}
