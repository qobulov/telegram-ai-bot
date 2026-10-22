package webapi

import (
	"strings"
	"testing"
)

func moviesOptions(u *upstream) Options {
	opts := u.options()
	opts.TMDBToken = "tmdb-test"
	return opts
}

const tmdbDune = `{"results":[
	{"media_type":"person","id":1,"name":"Dune Person"},
	{"media_type":"movie","id":438631,"title":"Дюна","original_title":"Dune","release_date":"2021-09-15","vote_average":7.784,"vote_count":14000,"overview":"Пол Атрейдес"},
	{"media_type":"tv","id":90228,"name":"Дюна: Пророчество","original_name":"Dune: Prophecy","first_air_date":"2024-11-17","vote_average":7.2,"vote_count":671},
	{"media_type":"movie","id":2,"title":"Unrated","original_title":"Unrated","release_date":""}]}`

func TestMoviesSearchKeepsTitlesOnly(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/tmdb/3/search/multi": ok(tmdbDune)})

	out := result[moviesOutput](t, run(t, moviesOptions(u), "movies", `{"action":"search","query":" Dune ","year":2021}`))

	want := movieRow{ID: 438631, Kind: "movie", Title: "Дюна", Original: "Dune", Year: "2021", Rating: "7.8/10 (14000 votes)", Overview: "Пол Атрейдес", Link: "https://www.themoviedb.org/movie/438631"}
	if len(out.Titles) != 3 || out.Titles[0] != want || out.Titles[1].Kind != "tv" || out.Titles[1].Link != "https://www.themoviedb.org/tv/90228" || out.Titles[2].Rating != "" || out.Titles[2].Original != "" {
		t.Errorf("titles = %+v", out.Titles)
	}
	if !strings.Contains(out.Source, "not endorsed") {
		t.Errorf("source = %q, want TMDB's attribution", out.Source)
	}
	req := u.requests("/tmdb/3/search/multi")[0]
	if q := req.URL.Query(); q.Get("query") != "Dune" || q.Get("year") != "2021" || q.Get("language") != "ru-RU" {
		t.Errorf("query = %v", q)
	}
	if req.Header.Get("Authorization") != "Bearer tmdb-test" || strings.Contains(req.URL.String(), "tmdb-test") {
		t.Error("the token must go in the header only")
	}
}

func TestMoviesDetails(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/tmdb/3/movie/438631": ok(`{"id":438631,"title":"Dune","original_title":"Dune","release_date":"2021-09-15","vote_average":7.8,"vote_count":14000,
		"runtime":155,"tagline":"Beyond fear, destiny awaits.","genres":[{"name":"Science Fiction"},{"name":"Adventure"}],
		"credits":{"cast":[{"name":"Timothée Chalamet"},{"name":"Rebecca Ferguson"}],"crew":[{"name":"Denis Villeneuve","job":"Director"},{"name":"Hans Zimmer","job":"Original Music Composer"}]}}`)})

	out := result[movieDetails](t, run(t, moviesOptions(u), "movies", `{"action":"details","id":438631,"lang":"en"}`))

	if out.Title != "Dune" || out.Runtime != "155 min" || strings.Join(out.Genres, ",") != "Science Fiction,Adventure" || strings.Join(out.Director, ",") != "Denis Villeneuve" || len(out.Cast) != 2 || out.Tagline == "" {
		t.Errorf("details = %+v", out)
	}
	if q := u.requests("/tmdb/3/movie/438631")[0].URL.Query(); q.Get("append_to_response") != "credits" || q.Get("language") != "en-US" {
		t.Errorf("query = %v", q)
	}
}

func TestMoviesPopularByGenre(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/tmdb/3/genre/movie/list": ok(`{"genres":[{"id":35,"name":"Comedy"},{"id":878,"name":"Science Fiction"}]}`),
		"/tmdb/3/discover/movie":   ok(`{"results":[{"id":5,"title":"Funny","release_date":"2025-01-01","vote_average":7,"vote_count":900}]}`),
		"/tmdb/3/trending/tv/week": ok(`{"results":[{"id":6,"name":"Show","first_air_date":"2026-01-01"}]}`),
	})

	out := result[moviesOutput](t, run(t, moviesOptions(u), "movies", `{"action":"popular","genre":"science fiction"}`))
	if len(out.Titles) != 1 || out.Titles[0].Title != "Funny" {
		t.Errorf("titles = %+v", out.Titles)
	}
	if q := u.requests("/tmdb/3/discover/movie")[0].URL.Query(); q.Get("with_genres") != "878" || q.Get("vote_count.gte") != "300" {
		t.Errorf("query = %v", q)
	}
	if out := result[moviesOutput](t, run(t, moviesOptions(u), "movies", `{"action":"popular","kind":"tv"}`)); len(out.Titles) != 1 || out.Titles[0].Kind != "tv" {
		t.Errorf("trending = %+v", out.Titles)
	}
	wantError(t, run(t, moviesOptions(u), "movies", `{"action":"popular","genre":"cartoons"}`), "invalid_arguments", "comedy")
}

func TestMoviesRefusesBadArguments(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/tmdb/3/search/multi": ok(`{"results":[{"media_type":"person","id":1,"name":"Someone"}]}`)})
	for args, word := range map[string]string{
		`{"action":"search","query":" "}`:             "query",
		`{"action":"details"}`:                        "id",
		`{"action":"popular","kind":"anime"}`:         "kind",
		`{"action":"search","query":"x","lang":"uz"}`: "lang",
		`{"action":"rate"}`:                           "action",
		`{"action":"search","query":"Someone"}`:       "no film",
	} {
		wantError(t, run(t, moviesOptions(u), "movies", args), "invalid_arguments", word)
	}
	if strings.Contains(toolNames(Tools(u.options())), "movies") {
		t.Error("movies offered without a token")
	}
}
