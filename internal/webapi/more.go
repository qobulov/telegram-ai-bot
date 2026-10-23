package webapi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/tools"
)

// The more_info tool gathers keyless reference APIs under one schema: Open
// Library, TheMealDB, Gold API, Lichess and Jolpica's Formula 1 data.

const (
	moreTTL       = time.Hour
	metalTTL      = 10 * time.Minute
	maxMoreQuery  = 80
	maxBooks      = 5
	maxRecipes    = 3
	troyOunceGram = 31.1034768
)

type moreArgs struct {
	Action string `json:"action"`
	Query  string `json:"query"`
}

func (a *apis) moreTool() tools.Tool {
	return tools.Tool{
		Name: "more_info",
		Description: "book: books by title or author; recipe: recipes by the dish's English name, as \"plov\" (no pork or alcohol); gold, silver: world price in USD per ounce and gram; " +
			"chess_puzzle: Lichess's puzzle of the day; chess_player: a Lichess user's ratings; f1_standings: this Formula 1 season's drivers; f1_last_race: the last race's result.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["book", "recipe", "gold", "silver", "chess_puzzle", "chess_player", "f1_standings", "f1_last_race"]},
				"query": {"type": "string", "description": "For book, recipe and chess_player: what to look for."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		Timeout: patientTimeout,
		Run:     a.more,
	}
}

func (a *apis) more(ctx context.Context, raw json.RawMessage) (any, error) {
	var args moreArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	query := strings.Join(strings.Fields(args.Query), " ")
	switch args.Action {
	case "book", "recipe", "chess_player":
		if query == "" || utf8.RuneCountInString(query) > maxMoreQuery {
			return nil, argumentError("query must be 1 to %d characters", maxMoreQuery)
		}
	}
	switch args.Action {
	case "book":
		return a.books(ctx, query)
	case "recipe":
		return a.recipes(ctx, query)
	case "gold":
		return a.metal(ctx, "XAU", "gold")
	case "silver":
		return a.metal(ctx, "XAG", "silver")
	case "chess_puzzle":
		return a.chessPuzzle(ctx)
	case "chess_player":
		return a.chessPlayer(ctx, query)
	case "f1_standings":
		return a.f1Standings(ctx)
	case "f1_last_race":
		return a.f1LastRace(ctx)
	}
	return nil, argumentError("action must be book, recipe, gold, silver, chess_puzzle, chess_player, f1_standings or f1_last_race")
}

// cachedJSON reads a JSON answer, keeping it for ttl.
func (a *apis) cachedJSON(ctx context.Context, service, link string, ttl time.Duration, into any) error {
	raw, err := remember(a.cache, "more:"+link, ttl, func() ([]byte, error) {
		return a.patient.get(ctx, call{service: service, url: link, limit: largeBodyLimit})
	})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("%s sent an answer that could not be read", service)
	}
	return nil
}

// --- Books (Open Library) ---

type book struct {
	Title   string   `json:"title"`
	Authors []string `json:"authors,omitempty"`
	Year    int      `json:"first_published,omitempty"`
	Pages   int      `json:"pages,omitempty"`
	Rating  string   `json:"rating,omitempty"`
	Link    string   `json:"link"`
}

func (a *apis) books(ctx context.Context, query string) (any, error) {
	link := a.urls.OpenLibrary + "/search.json?limit=5&fields=title,author_name,first_publish_year,number_of_pages_median,key,ratings_average&q=" + url.QueryEscape(query)
	var answer struct {
		Docs []struct {
			Title   string   `json:"title"`
			Authors []string `json:"author_name"`
			Year    int      `json:"first_publish_year"`
			Pages   int      `json:"number_of_pages_median"`
			Key     string   `json:"key"`
			Rating  float64  `json:"ratings_average"`
		} `json:"docs"`
	}
	if err := a.cachedJSON(ctx, "Open Library", link, moreTTL, &answer); err != nil {
		return nil, err
	}
	var out []book
	for _, d := range answer.Docs[:min(len(answer.Docs), maxBooks)] {
		b := book{Title: cleanText(d.Title, 150), Year: d.Year, Pages: d.Pages, Link: a.urls.OpenLibrary + d.Key}
		for _, author := range d.Authors[:min(len(d.Authors), 3)] {
			b.Authors = append(b.Authors, cleanText(author, 80))
		}
		if d.Rating > 0 {
			b.Rating = fmt.Sprintf("%.1f/5", d.Rating)
		}
		out = append(out, b)
	}
	if len(out) == 0 {
		return nil, argumentError("Open Library found no book; try the title in English or the author's name")
	}
	return map[string]any{"books": out, "source": "Open Library"}, nil
}

// --- Recipes (TheMealDB) ---

type recipe struct {
	Name         string   `json:"name"`
	Cuisine      string   `json:"cuisine,omitempty"`
	Ingredients  []string `json:"ingredients"`
	Instructions string   `json:"instructions"`
	Video        string   `json:"video,omitempty"`
}

// notHalal finds an ingredient a Muslim audience would not cook with.
var notHalal = regexp.MustCompile(`(?i)\b(pork|bacon|ham|prosciutto|pancetta|chorizo|salami|lard|gelatin|wine|beer|brandy|rum|vodka|whisky|whiskey|sherry|cognac|liqueur|sake|mirin|cider)\b`)

func (a *apis) recipes(ctx context.Context, query string) (any, error) {
	link := a.urls.MealDB + "/api/json/v1/1/search.php?s=" + url.QueryEscape(query)
	var answer struct {
		Meals []map[string]any `json:"meals"`
	}
	if err := a.cachedJSON(ctx, "TheMealDB", link, moreTTL, &answer); err != nil {
		return nil, err
	}
	var out []recipe
	for _, m := range answer.Meals {
		if len(out) == maxRecipes {
			break
		}
		field := func(name string) string {
			value, _ := m[name].(string)
			return strings.TrimSpace(value)
		}
		r := recipe{Name: cleanText(field("strMeal"), 100), Cuisine: field("strArea"), Instructions: cleanText(field("strInstructions"), 1200), Video: field("strYoutube")}
		halal := field("strCategory") != "Pork"
		for i := 1; i <= 20; i++ {
			ingredient := field(fmt.Sprintf("strIngredient%d", i))
			if ingredient == "" {
				continue
			}
			if notHalal.MatchString(ingredient) {
				halal = false
				break
			}
			r.Ingredients = append(r.Ingredients, strings.TrimSpace(field(fmt.Sprintf("strMeasure%d", i))+" "+ingredient))
		}
		if halal && !notHalal.MatchString(r.Name) {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil, argumentError("TheMealDB has no such recipe; try the dish's English name, or answer from what you know")
	}
	return map[string]any{"recipes": out, "source": "TheMealDB"}, nil
}

// --- Gold and silver (Gold API) ---

func (a *apis) metal(ctx context.Context, symbol, name string) (any, error) {
	var answer struct {
		Price     float64 `json:"price"`
		UpdatedAt string  `json:"updatedAt"`
	}
	if err := a.cachedJSON(ctx, "Gold API", a.urls.GoldAPI+"/price/"+symbol, metalTTL, &answer); err != nil {
		return nil, err
	}
	if answer.Price <= 0 {
		return nil, fmt.Errorf("Gold API sent no %s price", name)
	}
	return map[string]any{
		"metal":         name,
		"usd_per_ounce": fmt.Sprintf("%.2f", answer.Price),
		"usd_per_gram":  fmt.Sprintf("%.2f", answer.Price/troyOunceGram),
		"updated":       answer.UpdatedAt,
		"note":          "World spot price per troy ounce (31.1 g); shops in Uzbekistan sell above it. Convert to so'm with currency_rate if asked.",
		"source":        "gold-api.com",
	}, nil
}

// --- Chess (Lichess) ---

func (a *apis) chessPuzzle(ctx context.Context) (any, error) {
	var answer struct {
		Puzzle struct {
			ID       string   `json:"id"`
			Rating   int      `json:"rating"`
			FEN      string   `json:"fen"`
			Solution []string `json:"solution"`
			Themes   []string `json:"themes"`
		} `json:"puzzle"`
	}
	if err := a.cachedJSON(ctx, "Lichess", a.urls.Lichess+"/api/puzzle/daily", moreTTL, &answer); err != nil {
		return nil, err
	}
	p := answer.Puzzle
	if p.ID == "" {
		return nil, fmt.Errorf("Lichess sent no puzzle")
	}
	return map[string]any{
		"link": "https://lichess.org/training/" + p.ID, "rating": p.Rating, "position_fen": p.FEN,
		"themes": p.Themes, "solution": p.Solution,
		"note":   "Give the link to solve it on the board; keep the solution (moves in UCI) until they ask for it.",
		"source": "Lichess",
	}, nil
}

var lichessName = regexp.MustCompile(`^[A-Za-z0-9_-]{2,30}$`)

func (a *apis) chessPlayer(ctx context.Context, name string) (any, error) {
	name = strings.TrimPrefix(name, "@")
	if !lichessName.MatchString(name) {
		return nil, argumentError("query must be a Lichess username")
	}
	var answer struct {
		Username string `json:"username"`
		Title    string `json:"title"`
		URL      string `json:"url"`
		Disabled bool   `json:"disabled"`
		Perfs    map[string]struct {
			Games  int `json:"games"`
			Rating int `json:"rating"`
		} `json:"perfs"`
		Count struct {
			All  int `json:"all"`
			Win  int `json:"win"`
			Loss int `json:"loss"`
			Draw int `json:"draw"`
		} `json:"count"`
	}
	err := a.cachedJSON(ctx, "Lichess", a.urls.Lichess+"/api/user/"+url.PathEscape(name), moreTTL, &answer)
	if isNotFound(err) || (err == nil && answer.Username == "") {
		return nil, argumentError("Lichess has no player %q", name)
	}
	if err != nil {
		return nil, err
	}
	ratings := map[string]int{}
	for _, perf := range []string{"bullet", "blitz", "rapid", "classical", "puzzle"} {
		if p, ok := answer.Perfs[perf]; ok && (p.Games > 0 || perf == "puzzle") && p.Rating > 0 {
			ratings[perf] = p.Rating
		}
	}
	return map[string]any{
		"username": answer.Username, "title": answer.Title, "ratings": ratings,
		"games": answer.Count.All, "wins": answer.Count.Win, "losses": answer.Count.Loss, "draws": answer.Count.Draw,
		"link": answer.URL, "source": "Lichess",
	}, nil
}

// --- Formula 1 (Jolpica, the Ergast API's successor) ---

type f1Driver struct {
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
}

func (d f1Driver) name() string { return strings.TrimSpace(d.GivenName + " " + d.FamilyName) }

func (a *apis) f1Standings(ctx context.Context) (any, error) {
	var answer struct {
		MRData struct {
			StandingsTable struct {
				StandingsLists []struct {
					Season          string `json:"season"`
					Round           string `json:"round"`
					DriverStandings []struct {
						Position     string   `json:"position"`
						Points       string   `json:"points"`
						Wins         string   `json:"wins"`
						Driver       f1Driver `json:"Driver"`
						Constructors []struct {
							Name string `json:"name"`
						} `json:"Constructors"`
					} `json:"DriverStandings"`
				} `json:"StandingsLists"`
			} `json:"StandingsTable"`
		} `json:"MRData"`
	}
	if err := a.cachedJSON(ctx, "Jolpica F1", a.urls.Jolpica+"/ergast/f1/current/driverStandings.json", moreTTL, &answer); err != nil {
		return nil, err
	}
	lists := answer.MRData.StandingsTable.StandingsLists
	if len(lists) == 0 {
		return nil, fmt.Errorf("Jolpica F1 sent no standings")
	}
	var drivers []string
	for _, d := range lists[0].DriverStandings[:min(len(lists[0].DriverStandings), 10)] {
		team := ""
		if len(d.Constructors) > 0 {
			team = ", " + d.Constructors[0].Name
		}
		drivers = append(drivers, fmt.Sprintf("%s. %s%s: %s points, %s wins", d.Position, d.Driver.name(), team, d.Points, d.Wins))
	}
	return map[string]any{"season": lists[0].Season, "after_round": lists[0].Round, "top_10": drivers, "source": "Jolpica F1"}, nil
}

func (a *apis) f1LastRace(ctx context.Context) (any, error) {
	var answer struct {
		MRData struct {
			RaceTable struct {
				Races []struct {
					Season  string `json:"season"`
					Round   string `json:"round"`
					Name    string `json:"raceName"`
					Date    string `json:"date"`
					Results []struct {
						Position string   `json:"position"`
						Points   string   `json:"points"`
						Status   string   `json:"status"`
						Driver   f1Driver `json:"Driver"`
						Team     struct {
							Name string `json:"name"`
						} `json:"Constructor"`
						Time struct {
							Time string `json:"time"`
						} `json:"Time"`
					} `json:"Results"`
				} `json:"Races"`
			} `json:"RaceTable"`
		} `json:"MRData"`
	}
	if err := a.cachedJSON(ctx, "Jolpica F1", a.urls.Jolpica+"/ergast/f1/current/last/results.json", moreTTL, &answer); err != nil {
		return nil, err
	}
	races := answer.MRData.RaceTable.Races
	if len(races) == 0 {
		return nil, fmt.Errorf("Jolpica F1 sent no race")
	}
	race := races[0]
	var top []string
	for _, r := range race.Results[:min(len(race.Results), 10)] {
		result := cmp.Or(r.Time.Time, r.Status)
		top = append(top, fmt.Sprintf("%s. %s (%s) %s, %s points", r.Position, r.Driver.name(), r.Team.Name, result, r.Points))
	}
	return map[string]any{"race": race.Name, "date": race.Date, "season": race.Season, "round": race.Round, "top_10": top, "source": "Jolpica F1"}, nil
}

// isNotFound reports an API's 404.
func isNotFound(err error) bool {
	var status *statusError
	return errors.As(err, &status) && status.status == http.StatusNotFound
}
