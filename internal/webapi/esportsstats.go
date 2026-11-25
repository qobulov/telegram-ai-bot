package webapi

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// The pro leagues' season statistics, MVP race and match details, from
// Rone Arena, as the owners asked on 2026-11-25.

type mplLeaderboard struct {
	Title   string `json:"title"`
	Entries []struct {
		Rank int      `json:"rank"`
		Name string   `json:"name"`
		Team *mplTeam `json:"team"`
		Hero *struct {
			Name string `json:"name"`
		} `json:"hero"`
		Value float64 `json:"value"`
	} `json:"entries"`
}

// lines are a leaderboard's entries: "1. KarlTzy (Team Liquid PH): 4.36".
func (b mplLeaderboard) lines() []string {
	var lines []string
	for _, e := range b.Entries {
		name := cleanText(e.Name, maxHeroName)
		if e.Hero != nil && e.Hero.Name != "" && !strings.EqualFold(e.Hero.Name, e.Name) {
			name += ", " + cleanText(e.Hero.Name, maxHeroName)
		}
		if e.Team != nil {
			name += " (" + e.Team.label() + ")"
		}
		lines = append(lines, strconv.Itoa(e.Rank)+". "+name+": "+strconv.FormatFloat(e.Value, 'f', -1, 64))
	}
	return lines
}

func (a *apis) mplStats(ctx context.Context, code, league string) (any, error) {
	type boards struct {
		Leaderboards []mplLeaderboard `json:"leaderboards"`
	}
	out := map[string]any{"league": league, "source": esportsSource, "note": esportsNote}
	for _, what := range []string{"players", "heroes"} {
		answer, err := esportsGet[boards](ctx, a, code, "statistics/"+what)
		if err != nil {
			continue
		}
		for _, b := range answer.Leaderboards {
			if lines := b.lines(); len(lines) > 0 {
				out[cleanText(b.Title, maxHeroName*2)] = lines
			}
		}
	}
	teams, err := esportsGet[struct {
		Teams []struct {
			Team    mplTeam `json:"team"`
			Kills   int     `json:"kills"`
			Deaths  int     `json:"deaths"`
			Lords   int     `json:"lord"`
			Turtles int     `json:"tortoise"`
			Towers  int     `json:"tower"`
		} `json:"teams"`
	}](ctx, a, code, "statistics/teams")
	if err == nil && len(teams.Teams) > 0 {
		var rows []string
		for _, t := range teams.Teams {
			rows = append(rows, t.Team.label()+": kills "+strconv.Itoa(t.Kills)+", deaths "+strconv.Itoa(t.Deaths)+
				", lords "+strconv.Itoa(t.Lords)+", turtles "+strconv.Itoa(t.Turtles)+", towers "+strconv.Itoa(t.Towers))
		}
		out["teams"] = rows
	}
	if len(out) == 3 {
		return nil, argumentError("%s has no season statistics yet", league)
	}
	return out, nil
}

func (a *apis) mplMVP(ctx context.Context, code, league string) (any, error) {
	answer, err := esportsGet[struct {
		Standings []struct {
			Rank   int     `json:"rank"`
			Name   string  `json:"name"`
			Team   mplTeam `json:"team"`
			Points int     `json:"points"`
		} `json:"standings"`
	}](ctx, a, code, "statistics/mvp")
	if err != nil && (code == "id" || code == "kh") {
		return nil, err
	}
	if err != nil || len(answer.Standings) == 0 {
		return nil, argumentError("%s gives no MVP standings; only MPL Indonesia and Cambodia do", league)
	}
	var rows []string
	for _, s := range answer.Standings[:min(len(answer.Standings), 10)] {
		rows = append(rows, strconv.Itoa(s.Rank)+". "+cleanText(s.Name, maxHeroName)+" ("+s.Team.label()+"): "+strconv.Itoa(s.Points)+" points")
	}
	return map[string]any{"league": league, "mvp": rows, "source": esportsSource}, nil
}

type mplGamePlayer struct {
	Name string `json:"name"`
	Hero struct {
		Name string `json:"name"`
	} `json:"hero"`
	Kills   int `json:"kills"`
	Deaths  int `json:"deaths"`
	Assists int `json:"assists"`
}

type mplGame struct {
	Number   int    `json:"number"`
	Duration string `json:"duration_estimate"`
	Teams    []struct {
		Team    mplTeam         `json:"team"`
		Winner  *bool           `json:"winner"`
		Kills   int             `json:"kills"`
		Players []mplGamePlayer `json:"players"`
	} `json:"teams"`
}

// mplMatch is a team's last played match, against opponent when named,
// game by game.
func (a *apis) mplMatch(ctx context.Context, code, league, team, opponent string) (any, error) {
	if team == "" {
		return nil, argumentError("name the team, and the opponent for their match against each other")
	}
	all, err := a.seasonMatches(ctx, code, league, team)
	if err != nil {
		return nil, err
	}
	all = slices.DeleteFunc(all, func(m mplMatch) bool {
		return m.Status != "finished" || m.ID == "" || (opponent != "" && !m.Team1.is(opponent) && !m.Team2.is(opponent))
	})
	if len(all) == 0 {
		return nil, argumentError("no played %s match of %q fits; check the names", league, team)
	}
	last := all[len(all)-1]
	views := a.matchViews([]mplMatch{last})
	out := map[string]any{"league": league, "match": views[0], "source": esportsSource, "note": esportsNote}
	details, err := esportsGet[struct {
		Games []mplGame `json:"games"`
	}](ctx, a, code, "matches/"+url.PathEscape(last.ID))
	if err != nil {
		out["games"] = "the games' details could not be read just now: say so, and to ask again later"
		return out, nil
	}
	if len(details.Games) == 0 {
		out["games"] = "the games' details are not on the source"
		return out, nil
	}
	var games []map[string]any
	for _, g := range details.Games {
		game := map[string]any{"game": g.Number}
		if g.Duration != "" {
			game["length"] = g.Duration
		}
		for i, t := range g.Teams {
			var players []string
			for _, p := range t.Players {
				players = append(players, cleanText(p.Name, maxHeroName)+" "+cleanText(p.Hero.Name, maxHeroName)+" "+
					strconv.Itoa(p.Kills)+"/"+strconv.Itoa(p.Deaths)+"/"+strconv.Itoa(p.Assists))
			}
			side := map[string]any{"team": t.Team.label(), "kills": t.Kills, "players": players}
			if t.Winner != nil {
				side["won"] = *t.Winner
			}
			game["team"+strconv.Itoa(i+1)] = side
		}
		games = append(games, game)
	}
	out["games"] = games
	out["note"] = esportsNote + " Players are given as name, hero, kills/deaths/assists."
	return out, nil
}
