package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/tools"
)

const (
	// footballTTL keeps the free plan's 10 calls a minute out of reach.
	footballTTL   = 10 * time.Minute
	tableRows     = 10
	maxMatches    = 12
	topScorers    = 5
	maxTeamName   = 60
	footballCodes = "PL, PD, SA, BL1, FL1, CL, DED, PPL, ELC, EC, WC"

	footballSource = "football-data.org (latest, may be delayed)"
)

// competitionNames maps codes and the names people use, after
// normalizeName, to football-data.org's codes.
var competitionNames = map[string]string{
	"pl": "PL", "apl": "PL", "epl": "PL", "premierleague": "PL", "premyerliga": "PL", "angliya": "PL", "апл": "PL", "англия": "PL", "премьерлига": "PL",
	"pd": "PD", "laliga": "PD", "ispaniya": "PD", "лалига": "PD", "испания": "PD",
	"sa": "SA", "seriea": "SA", "seriyaa": "SA", "italiya": "SA", "сериаа": "SA", "серияа": "SA", "италия": "SA",
	"bl1": "BL1", "bundesliga": "BL1", "germaniya": "BL1", "бундеслига": "BL1", "германия": "BL1",
	"fl1": "FL1", "ligue1": "FL1", "liga1": "FL1", "fransiya": "FL1", "лига1": "FL1", "франция": "FL1",
	"cl": "CL", "ucl": "CL", "championsleague": "CL", "chempionlarligasi": "CL", "ligachempionov": "CL", "лигачемпионов": "CL", "чемпионларлигаси": "CL",
	"ded": "DED", "eredivisie": "DED", "gollandiya": "DED", "niderlandiya": "DED", "эредивизи": "DED",
	"ppl": "PPL", "primeiraliga": "PPL", "portugaliya": "PPL", "португалия": "PPL",
	"elc": "ELC", "championship": "ELC", "чемпионшип": "ELC",
	"ec": "EC", "euro": "EC", "yevro": "EC", "evro": "EC", "евро": "EC", "yevropachempionati": "EC", "чемпионатевропы": "EC",
	"wc": "WC", "worldcup": "WC", "jahonchempionati": "WC", "mundial": "WC", "чм": "WC", "чемпионатмира": "WC",
}

// teamNicknames maps what fans type, after foldTeam, to words of the
// team's official name.
var teamNicknames = map[string]string{
	"barsa": "barcelona", "barca": "barcelona", "барса": "barcelona", "барселона": "barcelona",
	"real": "real madrid", "реал": "real madrid",
	"man city": "manchester city", "ман сити": "manchester city", "манчестер сити": "manchester city",
	"man united": "manchester united", "man utd": "manchester united", "manu": "manchester united", "манчестер юнайтед": "manchester united",
	"bavariya": "bayern", "бавария": "bayern",
	"yuventus": "juventus", "ювентус": "juventus",
	"liverpul": "liverpool", "ливерпуль": "liverpool",
	"chelsi": "chelsea", "челси": "chelsea",
	"арсенал": "arsenal",
	"psj":     "paris saint-germain", "pszh": "paris saint-germain", "псж": "paris saint-germain",
	"интер": "inter", "милан": "milan",
	"atletiko": "atletico", "атлетико": "atletico",
}

type footballArgs struct {
	Action      string `json:"action"`
	Competition string `json:"competition"`
	Team        string `json:"team"`
}

type tableOutput struct {
	Competition string     `json:"competition"`
	Group       string     `json:"group,omitempty"`
	Matchday    int        `json:"matchday,omitempty"`
	Rows        []tableRow `json:"rows"`
	Source      string     `json:"source"`
}

type tableRow struct {
	Pos    int    `json:"pos"`
	Team   string `json:"team"`
	Played int    `json:"played"`
	Points int    `json:"points"`
	GD     int    `json:"gd"`
}

type matchesOutput struct {
	Competition string     `json:"competition"`
	Matchday    int        `json:"matchday,omitempty"`
	Matches     []matchRow `json:"matches"`
	Note        string     `json:"note,omitempty"`
	Source      string     `json:"source"`
}

type matchRow struct {
	Kickoff string `json:"kickoff"`
	Home    string `json:"home"`
	Away    string `json:"away"`
	Score   string `json:"score,omitempty"`
	Status  string `json:"status"`
}

type scorersOutput struct {
	Competition string      `json:"competition"`
	Scorers     []scorerRow `json:"scorers"`
	Source      string      `json:"source"`
}

type scorerRow struct {
	Player    string `json:"player"`
	Team      string `json:"team"`
	Goals     int    `json:"goals"`
	Assists   *int   `json:"assists,omitempty"`
	Penalties *int   `json:"penalties,omitempty"`
}

func (a *apis) footballTool() tools.Tool {
	return tools.Tool{
		Name:        "football",
		Description: "Football from football-data.org: league table, the current matchday's matches, or top scorers, for the top European leagues, Champions League, Euro and World Cup. Scores can lag a few minutes: call them latest, not live.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["table", "matches", "scorers"]},
				"competition": {"type": "string", "description": "PL (England), PD (Spain), SA (Italy), BL1 (Germany), FL1 (France), CL (Champions League), DED, PPL, ELC, EC (Euro) or WC (World Cup); names such as APL or La Liga work too. Default PL."},
				"team": {"type": "string", "description": "Only this team's matches, or its row in the table, e.g. Real Madrid, Barsa, Man City."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		Run: a.football,
	}
}

func (a *apis) football(ctx context.Context, raw json.RawMessage) (any, error) {
	var args footballArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	code, found := competitionNames[normalizeName(or(args.Competition, "PL"))]
	if !found {
		return nil, argumentError("unknown competition %q; use one of %s", args.Competition, footballCodes)
	}
	team := strings.TrimSpace(args.Team)
	if len([]rune(team)) > maxTeamName {
		return nil, argumentError("team must be at most %d characters", maxTeamName)
	}
	switch args.Action {
	case "table":
		return a.table(ctx, code, team)
	case "matches":
		return a.matches(ctx, code, team)
	case "scorers":
		return a.scorers(ctx, code)
	default:
		return nil, argumentError("action must be table, matches or scorers")
	}
}

// askFootball sends the key in a header and caches answers, because the
// free plan allows 10 calls a minute for every instance together.
func askFootball[T any](ctx context.Context, a *apis, path string) (T, error) {
	return remember(a.cache, "football:"+path, footballTTL, func() (T, error) {
		var answer T
		header := http.Header{}
		header.Set("X-Auth-Token", a.footballToken)
		err := a.client.getJSON(ctx, call{service: "football-data.org", url: a.urls.FootballData + "/v4/competitions/" + path, header: header, limit: largeBodyLimit}, &answer)
		var status *statusError
		if errors.As(err, &status) && status.status == http.StatusForbidden {
			err = errors.New("football-data.org's free plan does not cover this competition")
		}
		return answer, err
	})
}

type footballTeam struct {
	Name      string `json:"name"`
	ShortName string `json:"shortName"`
	TLA       string `json:"tla"`
}

// label is the name to show: the short one when there is one.
func (t footballTeam) label() string { return or(t.ShortName, t.Name) }

// is reports whether the user's words name this team.
func (t footballTeam) is(team string) bool {
	if team == "" {
		return true
	}
	wanted := foldTeam(team)
	if nickname, found := teamNicknames[wanted]; found {
		wanted = nickname
	}
	return strings.Contains(foldTeam(t.Name+" | "+t.ShortName+" | "+t.TLA), wanted)
}

// foldTeam lowers a name and drops the accents that names such as
// Atlético carry and fans do not type.
func foldTeam(name string) string {
	return accents.Replace(strings.Join(strings.Fields(strings.ToLower(name)), " "))
}

var accents = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "ç", "c", "é", "e", "è", "e", "ê", "e", "í", "i", "ñ", "n", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ú", "u", "ü", "u", "ß", "ss")

type standingsAnswer struct {
	Competition struct {
		Name string `json:"name"`
	} `json:"competition"`
	Season struct {
		CurrentMatchday int `json:"currentMatchday"`
	} `json:"season"`
	Standings []struct {
		Type  string `json:"type"`
		Group string `json:"group"`
		Table []struct {
			Position       int          `json:"position"`
			Team           footballTeam `json:"team"`
			PlayedGames    int          `json:"playedGames"`
			Points         int          `json:"points"`
			GoalDifference int          `json:"goalDifference"`
		} `json:"table"`
	} `json:"standings"`
}

// table gives the top ten of the overall table and, when a team is named
// and sits lower, its row too. In a group stage it picks the team's group.
func (a *apis) table(ctx context.Context, code, team string) (tableOutput, error) {
	answer, err := askFootball[standingsAnswer](ctx, a, code+"/standings")
	if err != nil {
		return tableOutput{}, err
	}
	out := tableOutput{Competition: answer.Competition.Name, Matchday: answer.Season.CurrentMatchday, Source: footballSource}
	chosen := -1
	for i, standing := range answer.Standings {
		if standing.Type != "TOTAL" {
			continue
		}
		if chosen < 0 {
			chosen = i
		}
		for _, row := range standing.Table {
			if team != "" && row.Team.is(team) {
				chosen = i
			}
		}
	}
	if chosen < 0 {
		return tableOutput{}, errors.New("football-data.org has no table for this competition right now")
	}
	standing := answer.Standings[chosen]
	out.Group = strings.Replace(standing.Group, "GROUP_", "Group ", 1)
	for i, row := range standing.Table {
		if i < tableRows || (team != "" && row.Team.is(team)) {
			out.Rows = append(out.Rows, tableRow{Pos: row.Position, Team: row.Team.label(), Played: row.PlayedGames, Points: row.Points, GD: row.GoalDifference})
		}
	}
	return out, nil
}

type competitionAnswer struct {
	Name          string `json:"name"`
	CurrentSeason struct {
		CurrentMatchday *int `json:"currentMatchday"`
	} `json:"currentSeason"`
}

type matchesAnswer struct {
	Competition struct {
		Name string `json:"name"`
	} `json:"competition"`
	Matches []footballMatch `json:"matches"`
}

type footballMatch struct {
	UTCDate  time.Time    `json:"utcDate"`
	Status   string       `json:"status"`
	HomeTeam footballTeam `json:"homeTeam"`
	AwayTeam footballTeam `json:"awayTeam"`
	Score    struct {
		FullTime struct {
			Home *int `json:"home"`
			Away *int `json:"away"`
		} `json:"fullTime"`
	} `json:"score"`
}

func (m footballMatch) finished() bool {
	return m.Status == "FINISHED" || m.Status == "AWARDED" || m.Status == "CANCELLED"
}

// matches gives the current matchday, or the next one once every match
// of the current one is over; with a team, its last result comes too.
// Competitions without a current matchday list scheduled matches.
func (a *apis) matches(ctx context.Context, code, team string) (matchesOutput, error) {
	competition, err := askFootball[competitionAnswer](ctx, a, code)
	if err != nil {
		return matchesOutput{}, err
	}
	out := matchesOutput{Competition: competition.Name, Matches: []matchRow{}, Source: footballSource}
	current := competition.CurrentSeason.CurrentMatchday
	if current == nil {
		scheduled, err := askFootball[matchesAnswer](ctx, a, code+"/matches?status=SCHEDULED")
		if err != nil {
			return matchesOutput{}, err
		}
		a.addMatches(&out, scheduled.Matches, team)
		return withMatchNote(out, team), nil
	}
	shown, err := askFootball[matchesAnswer](ctx, a, code+"/matches?matchday="+strconv.Itoa(*current))
	if err != nil {
		return matchesOutput{}, err
	}
	out.Matchday = *current
	if allFinished(shown.Matches) {
		next, err := askFootball[matchesAnswer](ctx, a, code+"/matches?matchday="+strconv.Itoa(*current+1))
		if err == nil && len(next.Matches) > 0 {
			if team != "" {
				a.addMatches(&out, shown.Matches, team)
			}
			shown, out.Matchday = next, *current+1
		}
	}
	a.addMatches(&out, shown.Matches, team)
	return withMatchNote(out, team), nil
}

func allFinished(matches []footballMatch) bool {
	for _, m := range matches {
		if !m.finished() {
			return false
		}
	}
	return len(matches) > 0
}

func (a *apis) addMatches(out *matchesOutput, matches []footballMatch, team string) {
	for _, m := range matches {
		if len(out.Matches) == maxMatches {
			return
		}
		if team != "" && !m.HomeTeam.is(team) && !m.AwayTeam.is(team) {
			continue
		}
		row := matchRow{
			Kickoff: m.UTCDate.In(a.location).Format("Mon 02 Jan 15:04"),
			Home:    m.HomeTeam.label(),
			Away:    m.AwayTeam.label(),
			Status:  matchStatus(m.Status),
		}
		if home, away := m.Score.FullTime.Home, m.Score.FullTime.Away; home != nil && away != nil {
			row.Score = fmt.Sprintf("%d-%d", *home, *away)
		}
		out.Matches = append(out.Matches, row)
	}
}

func withMatchNote(out matchesOutput, team string) matchesOutput {
	if len(out.Matches) == 0 && team != "" {
		out.Note = fmt.Sprintf("%s has no match in this list; check the team's name or the competition", team)
	}
	return out
}

func matchStatus(status string) string {
	switch status {
	case "SCHEDULED", "TIMED":
		return "scheduled"
	case "IN_PLAY", "LIVE":
		return "playing"
	case "PAUSED":
		return "half-time"
	default:
		return strings.ToLower(status)
	}
}

type scorersAnswer struct {
	Competition struct {
		Name string `json:"name"`
	} `json:"competition"`
	Scorers []struct {
		Player struct {
			Name string `json:"name"`
		} `json:"player"`
		Team      footballTeam `json:"team"`
		Goals     int          `json:"goals"`
		Assists   *int         `json:"assists"`
		Penalties *int         `json:"penalties"`
	} `json:"scorers"`
}

func (a *apis) scorers(ctx context.Context, code string) (scorersOutput, error) {
	query := url.Values{"limit": {strconv.Itoa(topScorers)}}
	answer, err := askFootball[scorersAnswer](ctx, a, code+"/scorers?"+query.Encode())
	if err != nil {
		return scorersOutput{}, err
	}
	out := scorersOutput{Competition: answer.Competition.Name, Scorers: []scorerRow{}, Source: footballSource}
	for _, s := range answer.Scorers[:min(len(answer.Scorers), topScorers)] {
		out.Scorers = append(out.Scorers, scorerRow{Player: s.Player.Name, Team: s.Team.label(), Goals: s.Goals, Assists: s.Assists, Penalties: s.Penalties})
	}
	return out, nil
}
