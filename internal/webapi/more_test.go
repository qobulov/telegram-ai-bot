package webapi

import (
	"strings"
	"testing"
)

func TestBooks(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/openlibrary/search.json": ok(`{"docs":[{"title":"O'tkan kunlar","author_name":["Abdulla Qodiriy"],"first_publish_year":1926,"key":"/works/OL1W","ratings_average":4.6}]}`)})

	env := run(t, u.options(), "more_info", `{"action":"book","query":"O'tkan  kunlar"}`)

	if !env.OK || !strings.Contains(env.raw, `"authors":["Abdulla Qodiriy"]`) || !strings.Contains(env.raw, `"rating":"4.6/5"`) || !strings.Contains(env.raw, `/openlibrary/works/OL1W`) {
		t.Errorf("books = %s", env.raw)
	}
	if q := u.requests("/openlibrary/search.json")[0].URL.Query(); q.Get("q") != "O'tkan kunlar" {
		t.Errorf("query = %v", q)
	}
	wantError(t, run(t, u.options(), "more_info", `{"action":"book"}`), "invalid_arguments", "query")
}

// Recipes with pork or alcohol are left out, by category or ingredient.
func TestRecipesLeaveOutPorkAndAlcohol(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/mealdb/api/json/v1/1/search.php": ok(`{"meals":[
		{"strMeal":"Pork Plov","strCategory":"Pork","strArea":"Russian","strInstructions":"x","strIngredient1":"Rice","strMeasure1":"1 cup"},
		{"strMeal":"Wine Plov","strCategory":"Lamb","strInstructions":"x","strIngredient1":"Rice","strMeasure1":"1 cup","strIngredient2":"Red Wine","strMeasure2":"1 glass"},
		{"strMeal":"Lamb Plov","strCategory":"Lamb","strArea":"Uzbek","strInstructions":"Fry the lamb.","strYoutube":"https://youtu.be/x","strIngredient1":"Lamb","strMeasure1":"500g","strIngredient2":"Rice","strMeasure2":"2 cups","strIngredient3":"","strMeasure3":" "}]}`)})

	out := result[struct {
		Recipes []recipe `json:"recipes"`
	}](t, run(t, u.options(), "more_info", `{"action":"recipe","query":"plov"}`))

	if len(out.Recipes) != 1 || out.Recipes[0].Name != "Lamb Plov" || strings.Join(out.Recipes[0].Ingredients, "|") != "500g Lamb|2 cups Rice" {
		t.Errorf("recipes = %+v", out.Recipes)
	}
}

func TestMetalPricesPerOunceAndGram(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/gold/price/XAU": ok(`{"price":3110.34768,"updatedAt":"2026-10-23T17:30:14Z"}`)})
	env := run(t, u.options(), "more_info", `{"action":"gold"}`)
	if !env.OK || !strings.Contains(env.raw, `"usd_per_ounce":"3110.35"`) || !strings.Contains(env.raw, `"usd_per_gram":"100.00"`) {
		t.Errorf("gold = %s", env.raw)
	}
}

func TestChess(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/lichess/api/puzzle/daily":       ok(`{"puzzle":{"id":"QjzCH","rating":1769,"fen":"8/8/6k1/4PRp1/7p/4P1r1/5K2/8 w - - 1 1","solution":["e5e6","g6f5"],"themes":["endgame"]}}`),
		"/lichess/api/user/DrNykterstein": ok(`{"username":"DrNykterstein","title":"GM","url":"https://lichess.org/@/DrNykterstein","perfs":{"blitz":{"games":100,"rating":3194},"rapid":{"games":0,"rating":1500}},"count":{"all":10455,"win":7212,"loss":2409,"draw":834}}`),
		"/lichess/api/user/nobody_here":   status(404),
	})

	if env := run(t, u.options(), "more_info", `{"action":"chess_puzzle"}`); !env.OK || !strings.Contains(env.raw, "https://lichess.org/training/QjzCH") {
		t.Errorf("puzzle = %s", env.raw)
	}
	env := run(t, u.options(), "more_info", `{"action":"chess_player","query":"@DrNykterstein"}`)
	if !env.OK || !strings.Contains(env.raw, `"ratings":{"blitz":3194}`) || !strings.Contains(env.raw, `"title":"GM"`) {
		t.Errorf("player = %s", env.raw)
	}
	wantError(t, run(t, u.options(), "more_info", `{"action":"chess_player","query":"nobody_here"}`), "invalid_arguments", "no player")
	wantError(t, run(t, u.options(), "more_info", `{"action":"chess_player","query":"a b/c"}`), "invalid_arguments", "username")
}

func TestFormulaOne(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/jolpica/ergast/f1/current/driverStandings.json": ok(`{"MRData":{"StandingsTable":{"StandingsLists":[{"season":"2026","round":"17","DriverStandings":[{"position":"1","points":"340","wins":"8","Driver":{"givenName":"Andrea Kimi","familyName":"Antonelli"},"Constructors":[{"name":"Mercedes"}]}]}]}}}`),
		"/jolpica/ergast/f1/current/last/results.json":    ok(`{"MRData":{"RaceTable":{"Races":[{"season":"2026","round":"17","raceName":"Singapore Grand Prix","date":"2026-10-11","Results":[{"position":"1","points":"25","status":"Finished","Driver":{"givenName":"Max","familyName":"Verstappen"},"Constructor":{"name":"Red Bull"},"Time":{"time":"1:42:00.785"}},{"position":"2","points":"0","status":"Retired","Driver":{"givenName":"A","familyName":"B"},"Constructor":{"name":"C"}}]}]}}}`),
	})

	if env := run(t, u.options(), "more_info", `{"action":"f1_standings"}`); !env.OK || !strings.Contains(env.raw, "1. Andrea Kimi Antonelli, Mercedes: 340 points, 8 wins") {
		t.Errorf("standings = %s", env.raw)
	}
	env := run(t, u.options(), "more_info", `{"action":"f1_last_race"}`)
	if !env.OK || !strings.Contains(env.raw, "1. Max Verstappen (Red Bull) 1:42:00.785, 25 points") || !strings.Contains(env.raw, "2. A B (C) Retired") {
		t.Errorf("last race = %s", env.raw)
	}
}
