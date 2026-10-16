package webapi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/tools"
)

const (
	// footballTTL keeps the free plan's 10 calls a minute out of reach.
	footballTTL = 10 * time.Minute
	tableRows   = 10
	// maxMatches fits a whole World Cup group matchday (24 matches) and a
	// Champions League one (18) in about 2.5 KB.
	maxMatches    = 24
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
	Note        string     `json:"note,omitempty"`
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
	// Kickoff is in Tashkent time; the field name says so, because the
	// source is British and Champions League times are known in CET.
	Kickoff string `json:"kickoff_tashkent"`
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
	code, found := competitionNames[normalizeName(cmp.Or(args.Competition, "PL"))]
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
func (t footballTeam) label() string { return cmp.Or(t.ShortName, t.Name) }

// teamMatch is how closely a fan's words name a team.
type teamMatch int

const (
	noMatch teamMatch = iota
	// partMatch is part of a word: "milan" in "internazionale milano".
	partMatch
	// wordMatch is whole words of a name: "milan" in "ac milan".
	wordMatch
	// fullMatch is the whole short name, TLA or name.
	fullMatch
)

// match says how closely wanted, already folded, names the team.
func (t footballTeam) match(wanted string) teamMatch {
	names := []string{foldTeam(t.ShortName), foldTeam(t.TLA), foldTeam(t.Name)}
	if slices.Contains(names, wanted) {
		return fullMatch
	}
	for _, name := range names {
		if strings.Contains(" "+name+" ", " "+wanted+" ") {
			return wordMatch
		}
	}
	for _, name := range names {
		if strings.Contains(name, wanted) {
			return partMatch
		}
	}
	return noMatch
}

// teamFilter keeps the teams a fan named. Only the closest kind of match
// among the teams in view counts, so "Milan" keeps AC Milan and not
// Internazionale Milano, and "Guinea" keeps Guinea and not Equatorial
// Guinea; part of a word counts only when nothing matches better.
type teamFilter struct {
	wanted string
	best   teamMatch
}

func newTeamFilter(team string, teams []footballTeam) teamFilter {
	if team == "" {
		return teamFilter{}
	}
	wanted := foldTeam(team)
	if nickname, found := teamNicknames[wanted]; found {
		wanted = nickname
	}
	f := teamFilter{wanted: wanted}
	for _, t := range teams {
		f.best = max(f.best, t.match(wanted))
	}
	return f
}

// names reports whether a team was named and t is it.
func (f teamFilter) names(t footballTeam) bool {
	return f.wanted != "" && f.best != noMatch && t.match(f.wanted) == f.best
}

// keeps is names, or true for every team when none was named.
func (f teamFilter) keeps(t footballTeam) bool { return f.wanted == "" || f.names(t) }

// found reports whether a named team was among the teams in view.
func (f teamFilter) found() bool { return f.wanted == "" || f.best != noMatch }

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
	var teams []footballTeam
	for _, standing := range answer.Standings {
		for _, row := range standing.Table {
			teams = append(teams, row.Team)
		}
	}
	filter := newTeamFilter(team, teams)
	chosen := -1
	for i, standing := range answer.Standings {
		if standing.Type != "TOTAL" {
			continue
		}
		if chosen < 0 {
			chosen = i
		}
		for _, row := range standing.Table {
			if filter.names(row.Team) {
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
		if i < tableRows || filter.names(row.Team) {
			out.Rows = append(out.Rows, tableRow{Pos: row.Position, Team: row.Team.label(), Played: row.PlayedGames, Points: row.Points, GD: row.GoalDifference})
		}
	}
	if !filter.found() {
		out.Note = fmt.Sprintf("%s is not in this table; check the team's name or the competition", team)
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
	UTCDate  time.Time     `json:"utcDate"`
	Status   string        `json:"status"`
	HomeTeam footballTeam  `json:"homeTeam"`
	AwayTeam footballTeam  `json:"awayTeam"`
	Score    footballScore `json:"score"`
}

type footballScore struct {
	// Duration is REGULAR, EXTRA_TIME or PENALTY_SHOOTOUT.
	Duration  string `json:"duration"`
	FullTime  goals  `json:"fullTime"`
	Penalties goals  `json:"penalties"`
}

type goals struct {
	Home *int `json:"home"`
	Away *int `json:"away"`
}

// text writes the score the way fans say it. football-data.org's fullTime
// counts shootout goals too (a 1-1 draw won 6-5 on penalties is 7-6), so
// those are taken out and shown apart.
func (s footballScore) text(finished bool) string {
	if s.FullTime.Home == nil || s.FullTime.Away == nil {
		return ""
	}
	home, away := *s.FullTime.Home, *s.FullTime.Away
	switch {
	case s.Duration == "PENALTY_SHOOTOUT" && s.Penalties.Home != nil && s.Penalties.Away != nil:
		penHome, penAway := *s.Penalties.Home, *s.Penalties.Away
		return fmt.Sprintf("%d-%d (pen %d-%d)", home-penHome, away-penAway, penHome, penAway)
	case s.Duration == "EXTRA_TIME" && finished:
		return fmt.Sprintf("%d-%d (aet)", home, away)
	case s.Duration == "EXTRA_TIME":
		return fmt.Sprintf("%d-%d (extra time)", home, away)
	default:
		return fmt.Sprintf("%d-%d", home, away)
	}
}

func (m footballMatch) finished() bool {
	return m.Status == "FINISHED" || m.Status == "AWARDED" || m.Status == "CANCELLED"
}

// matches gives the current matchday, or the next one once every match
// of the current one is over; with a team, its last result comes too.
// Competitions without a current matchday list upcoming matches: TIMED
// ones (date and time fixed) as well as SCHEDULED, because the status
// filter is a plain list.
func (a *apis) matches(ctx context.Context, code, team string) (matchesOutput, error) {
	competition, err := askFootball[competitionAnswer](ctx, a, code)
	if err != nil {
		return matchesOutput{}, err
	}
	out := matchesOutput{Competition: competition.Name, Matches: []matchRow{}, Source: footballSource}
	current := competition.CurrentSeason.CurrentMatchday
	if current == nil {
		upcoming, err := askFootball[matchesAnswer](ctx, a, code+"/matches?status=SCHEDULED,TIMED")
		if err != nil {
			return matchesOutput{}, err
		}
		return a.matchList(out, team, upcoming.Matches), nil
	}
	shown, err := askFootball[matchesAnswer](ctx, a, code+"/matches?matchday="+strconv.Itoa(*current))
	if err != nil {
		return matchesOutput{}, err
	}
	out.Matchday = *current
	var previous []footballMatch
	if allFinished(shown.Matches) {
		next, err := askFootball[matchesAnswer](ctx, a, code+"/matches?matchday="+strconv.Itoa(*current+1))
		if err == nil && len(next.Matches) > 0 {
			if team != "" {
				previous = shown.Matches
			}
			shown, out.Matchday = next, *current+1
		}
	}
	return a.matchList(out, team, previous, shown.Matches), nil
}

func allFinished(matches []footballMatch) bool {
	for _, m := range matches {
		if !m.finished() {
			return false
		}
	}
	return len(matches) > 0
}

// matchList shows the lists' matches of the named team, or all of them,
// up to maxMatches, and says when some had to be left out.
func (a *apis) matchList(out matchesOutput, team string, lists ...[]footballMatch) matchesOutput {
	matches := slices.Concat(lists...)
	var teams []footballTeam
	for _, m := range matches {
		teams = append(teams, m.HomeTeam, m.AwayTeam)
	}
	filter := newTeamFilter(team, teams)
	total := 0
	for _, m := range matches {
		if !filter.keeps(m.HomeTeam) && !filter.keeps(m.AwayTeam) {
			continue
		}
		total++
		if len(out.Matches) == maxMatches {
			continue
		}
		out.Matches = append(out.Matches, matchRow{
			Kickoff: m.UTCDate.In(a.location).Format("Mon 02 Jan 15:04"),
			Home:    m.HomeTeam.label(),
			Away:    m.AwayTeam.label(),
			Score:   m.Score.text(m.finished()),
			Status:  matchStatus(m.Status),
		})
	}
	switch {
	case total == 0 && team != "":
		out.Note = fmt.Sprintf("%s has no match in this list; check the team's name or the competition", team)
	case total > len(out.Matches):
		out.Note = fmt.Sprintf("showing %d of %d matches; name a team for the rest", len(out.Matches), total)
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
