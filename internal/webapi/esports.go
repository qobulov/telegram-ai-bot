package webapi

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/tools"
)

// mpl answers about the Mobile Legends pro leagues, MPL Indonesia, the
// Philippines, Malaysia and Cambodia, from Rone Arena, which reads their
// official sites, as the owners asked on 2026-11-25: asked who won MPL PH
// or when a team plays, the bot searched the web.

const (
	esportsTTL = 30 * time.Minute
	// shownMatches are the latest results and the next matches shown.
	shownMatches  = 6
	esportsSource = "Rone Arena API, from the league's official site"
	esportsNote   = "The league's current season. Times are Tashkent's. Team names are as the league writes them."
)

var mplLeagues = map[string]string{"id": "MPL Indonesia", "ph": "MPL Philippines", "my": "MPL Malaysia", "kh": "MPL Cambodia"}

type mplArgs struct {
	Action   string `json:"action"`
	League   string `json:"league"`
	Team     string `json:"team"`
	Opponent string `json:"opponent"`
}

func (a *apis) mplTool() tools.Tool {
	return tools.Tool{
		Name: "mpl",
		Description: "Mobile Legends pro leagues this season: MPL id, ph, my, kh. standings. matches: results and next, of a team if named. playoffs: with the champion. " +
			"match: a team's last played match game by game, players' heroes and KDA. stats: top players, heroes picked, banned, won, and teams. mvp. Not M-series.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["standings", "matches", "playoffs", "match", "stats", "mvp"]},
				"league": {"type": "string", "enum": ["id", "ph", "my", "kh"]},
				"team": {"type": "string", "description": "For matches and match: a team's short or full name, as \"ONIC\" or \"Aurora\"."},
				"opponent": {"type": "string", "description": "For match: the other team, for their last match against each other."}
			},
			"required": ["action", "league"],
			"additionalProperties": false
		}`),
		// A match's details follow its schedule and playoffs, each up to
		// patientTimeout when Rone Arena is busy.
		Timeout:        3 * patientTimeout,
		MaxOutputBytes: mlbbOutputBytes,
		Run:            a.mpl,
	}
}

type mplTeam struct {
	Name     string  `json:"name"`
	FullName *string `json:"full_name"`
}

func (t mplTeam) label() string {
	if t.FullName != nil && *t.FullName != "" {
		return cleanText(*t.FullName, maxHeroName)
	}
	return cleanText(t.Name, maxHeroName)
}

func (t mplTeam) is(name string) bool {
	want := lettersOf(name)
	return want != "" && (lettersOf(t.Name) == want || (t.FullName != nil && strings.Contains(lettersOf(*t.FullName), want)))
}

type mplWL struct {
	Win  int `json:"win"`
	Lose int `json:"lose"`
}

func (r mplWL) String() string {
	return strconv.Itoa(r.Win) + "-" + strconv.Itoa(r.Lose)
}

type mplMatch struct {
	ID      string  `json:"match_id"`
	StartAt string  `json:"start_at"`
	Team1   mplTeam `json:"team1"`
	Team2   mplTeam `json:"team2"`
	Score1  *int    `json:"score1"`
	Score2  *int    `json:"score2"`
	Round   *string `json:"round"`
	BestOf  int     `json:"best_of"`
	Status  string  `json:"status"`
	Winner  string  `json:"winner"`
}

type mplDay struct {
	Matches []mplMatch `json:"matches"`
}

func (a *apis) mpl(ctx context.Context, raw json.RawMessage) (any, error) {
	var args mplArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	league, ok := mplLeagues[args.League]
	if !ok {
		return nil, argumentError("league must be id, ph, my or kh")
	}
	switch args.Action {
	case "standings":
		return a.mplStandings(ctx, args.League, league)
	case "matches":
		return a.mplMatches(ctx, args.League, league, strings.TrimSpace(args.Team))
	case "playoffs":
		return a.mplPlayoffs(ctx, args.League, league)
	case "match":
		return a.mplMatch(ctx, args.League, league, strings.TrimSpace(args.Team), strings.TrimSpace(args.Opponent))
	case "stats":
		return a.mplStats(ctx, args.League, league)
	case "mvp":
		return a.mplMVP(ctx, args.League, league)
	}
	return nil, argumentError("action must be standings, matches, playoffs, match, stats or mvp")
}

func esportsGet[T any](ctx context.Context, a *apis, league, what string) (T, error) {
	link := a.urls.RoneArena + "/api/esports/" + league + "/" + what
	return remember(a.cache, "esports:"+league+":"+what, esportsTTL, func() (T, error) {
		var out T
		err := a.patient.getJSON(ctx, call{service: arenaService, url: link, limit: largeBodyLimit}, &out)
		return out, err
	})
}

type mplStanding struct {
	Rank       int     `json:"rank"`
	Team       mplTeam `json:"team"`
	MatchPoint int     `json:"match_point"`
	Matches    mplWL   `json:"match_wl"`
	NetGames   int     `json:"net_game_win"`
	Games      mplWL   `json:"game_wl"`
}

func (a *apis) mplStandings(ctx context.Context, code, league string) (any, error) {
	answer, err := esportsGet[struct {
		Standings []mplStanding `json:"standings"`
	}](ctx, a, code, "standings")
	if err != nil {
		return nil, err
	}
	if len(answer.Standings) == 0 {
		return nil, argumentError("%s has no standings yet", league)
	}
	table := make([]map[string]any, 0, len(answer.Standings))
	for _, s := range answer.Standings {
		table = append(table, map[string]any{"rank": s.Rank, "team": s.Team.label(), "points": s.MatchPoint,
			"matches": s.Matches.String(), "games": s.Games.String(), "net_games": s.NetGames})
	}
	return map[string]any{"league": league, "standings": table, "source": esportsSource, "note": esportsNote}, nil
}

// seasonMatches are the season's matches, the playoffs too, oldest first,
// of team when it is not empty.
func (a *apis) seasonMatches(ctx context.Context, code, league, team string) ([]mplMatch, error) {
	answer, err := esportsGet[struct {
		Weeks []struct {
			Days []mplDay `json:"days"`
		} `json:"weeks"`
	}](ctx, a, code, "schedule")
	if err != nil {
		return nil, err
	}
	var all []mplMatch
	for _, w := range answer.Weeks {
		for _, d := range w.Days {
			all = append(all, d.Matches...)
		}
	}
	if playoffs, err := a.playoffMatches(ctx, code); err == nil {
		all = append(all, playoffs...)
	}
	if team != "" {
		all = slices.DeleteFunc(all, func(m mplMatch) bool { return !m.Team1.is(team) && !m.Team2.is(team) })
		if len(all) == 0 {
			return nil, argumentError("no %s team is called %q; give its short or full name", league, team)
		}
	}
	slices.SortStableFunc(all, func(x, y mplMatch) int { return cmp.Compare(x.StartAt, y.StartAt) })
	return all, nil
}

func (a *apis) mplMatches(ctx context.Context, code, league, team string) (any, error) {
	all, err := a.seasonMatches(ctx, code, league, team)
	if err != nil {
		return nil, err
	}
	var played, next []mplMatch
	unrecorded := 0
	for _, m := range all {
		switch {
		case m.Status == "finished":
			played = append(played, m)
		case a.past(m):
			unrecorded++
		default:
			next = append(next, m)
		}
	}
	played = slices.Clone(played[max(0, len(played)-shownMatches):])
	slices.Reverse(played)
	out := map[string]any{"league": league, "latest_results": a.matchViews(played), "source": esportsSource, "note": esportsNote}
	if len(next) > 0 {
		out["next_matches"] = a.matchViews(next[:min(len(next), shownMatches)])
	} else {
		out["next_matches"] = "none scheduled: the season's matches are over or not yet announced"
	}
	if team != "" {
		out["team"] = team
	}
	if unrecorded > 0 {
		out["not_recorded"] = strconv.Itoa(unrecorded) + " matches whose time is past have no result in the source yet: say you do not know them"
	}
	return out, nil
}

// past is a match whose time has gone by without a result.
func (a *apis) past(m mplMatch) bool {
	at, err := time.Parse(time.RFC3339, m.StartAt)
	return err == nil && m.Status != "finished" && at.Add(6*time.Hour).Before(a.now())
}

func (a *apis) playoffMatches(ctx context.Context, code string) ([]mplMatch, error) {
	answer, err := esportsGet[struct {
		Days []mplDay `json:"days"`
	}](ctx, a, code, "playoffs")
	if err != nil {
		return nil, err
	}
	var all []mplMatch
	for _, d := range answer.Days {
		all = append(all, d.Matches...)
	}
	return all, nil
}

func (a *apis) mplPlayoffs(ctx context.Context, code, league string) (any, error) {
	matches, err := a.playoffMatches(ctx, code)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return map[string]any{"league": league, "playoffs": "not started or not yet announced", "source": esportsSource}, nil
	}
	matches = slices.Clone(matches)
	slices.SortStableFunc(matches, func(x, y mplMatch) int { return cmp.Compare(x.StartAt, y.StartAt) })
	out := map[string]any{"league": league, "playoff_matches": a.matchViews(matches), "source": esportsSource, "note": esportsNote}
	last := matches[len(matches)-1]
	allPlayed := !slices.ContainsFunc(matches, func(m mplMatch) bool { return m.Status != "finished" })
	if champion := winnerOf(last); allPlayed && champion != "" {
		out["champion"] = champion
	}
	return out, nil
}

func winnerOf(m mplMatch) string {
	switch m.Winner {
	case "team1":
		return m.Team1.label()
	case "team2":
		return m.Team2.label()
	}
	return ""
}

func (a *apis) matchViews(matches []mplMatch) []map[string]any {
	views := make([]map[string]any, 0, len(matches))
	for _, m := range matches {
		v := map[string]any{"match": m.Team1.label() + " vs " + m.Team2.label(), "best_of": m.BestOf, "status": m.Status}
		if a.past(m) {
			v["status"] = "played, result not recorded in the source yet"
		}
		if at, err := time.Parse(time.RFC3339, m.StartAt); err == nil {
			v["when"] = at.In(a.location).Format("2006-01-02 15:04")
		}
		if m.Score1 != nil && m.Score2 != nil && m.Status == "finished" {
			v["score"] = strconv.Itoa(*m.Score1) + ":" + strconv.Itoa(*m.Score2)
			v["winner"] = winnerOf(m)
		}
		if m.Round != nil && *m.Round != "" {
			v["round"] = cleanText(*m.Round, 2*maxHeroName)
		}
		views = append(views, v)
	}
	return views
}
