package webapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/tools"
)

func runTool(t *testing.T, u *upstream, name, args string) (envelope, *tools.Sink) {
	t.Helper()
	sink := &tools.Sink{}
	ctx := tools.WithTurn(context.Background(), tools.Caller{UserID: 1, ChatID: -100, Human: true}, sink)
	var env envelope
	output := newRegistry(t, u.options()).Execute(ctx, 1, name, args)
	if err := json.Unmarshal([]byte(output), &env); err != nil {
		t.Fatalf("output %q", output)
	}
	env.raw = output
	return env, sink
}

// Central Asia is asked by its box and magnitude 3, the world by magnitude
// 5; times are Tashkent's.
func TestEarthquakes(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/usgs/fdsnws/event/1/query": ok(`{"features":[{"properties":{"mag":4.26,"place":"35 km NE of Andijon, Uzbekistan","time":1792735200000},"geometry":{"coordinates":[72.6,40.9,12.4]}}]}`),
	})

	env, _ := runTool(t, u, "earthquakes", `{}`)

	if !env.OK || !strings.Contains(env.raw, `"magnitude":4.3`) || !strings.Contains(env.raw, "Andijon") || !strings.Contains(env.raw, `"depth_km":12`) {
		t.Errorf("result = %s", env.raw)
	}
	q := u.requests("/usgs/fdsnws/event/1/query")[0].URL.Query()
	if q.Get("minmagnitude") != "3" || q.Get("minlatitude") != "35" || q.Get("maxlongitude") != "90" {
		t.Errorf("query = %v", q)
	}
	runTool(t, u, "earthquakes", `{"area":"world","days":2}`)
	if q := u.requests("/usgs/fdsnws/event/1/query")[1].URL.Query(); q.Get("minmagnitude") != "5" || q.Get("minlatitude") != "" {
		t.Errorf("world query = %v", q)
	}
	if env, _ := runTool(t, u, "earthquakes", `{"days":90}`); env.OK {
		t.Errorf("90 days = %s, want refused", env.raw)
	}
}

// A QR code is fetched as a picture for the turn, holding the text given.
func TestQRCode(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/qr/v1/create-qr-code/": ok("\x89PNG\r\n\x1a\nQR")})

	env, sink := runTool(t, u, "qr_code", `{"text":"https://t.me/tiktok_trends_music"}`)

	if drawings := sink.Take(); !env.OK || len(drawings) != 1 || drawings[0].MimeType != "image/png" {
		t.Fatalf("env %s, drawings %d", env.raw, len(drawings))
	}
	if q := u.requests("/qr/v1/create-qr-code/")[0].URL.Query(); q.Get("data") != "https://t.me/tiktok_trends_music" {
		t.Errorf("query = %v", q)
	}
}

func TestFreeGames(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/ftg/api/games": ok(`[{"title":"Warframe","short_description":"A cooperative free-to-play third person online action game.","game_url":"https://www.freetogame.com/open/warframe","genre":"Shooter","platform":"PC (Windows)"}]`),
	})

	env, _ := runTool(t, u, "free_games", `{"genre":"shooter"}`)

	if !env.OK || !strings.Contains(env.raw, "Warframe") || !strings.Contains(env.raw, "freetogame.com/open/warframe") {
		t.Errorf("result = %s", env.raw)
	}
	if q := u.requests("/ftg/api/games")[0].URL.Query(); q.Get("category") != "shooter" || q.Get("platform") != "all" {
		t.Errorf("query = %v", q)
	}
}

func TestCatPicture(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/cat/img.jpg": ok("\xff\xd8\xffJPEG")})
	u.routes["/cat/v1/images/search"] = ok(`[{"url":"` + u.server.URL + `/cat/img.jpg"}]`)

	env, sink := runTool(t, u, "cat_picture", `{}`)

	if drawings := sink.Take(); !env.OK || len(drawings) != 1 || drawings[0].MimeType != "image/jpeg" {
		t.Errorf("env %s, drawings %+v", env.raw, drawings)
	}
}

// A word gets one definition per part of speech; an unknown one is an
// argument error the model can explain.
func TestEnglishWord(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/dict/api/v2/entries/en/run":  ok(`[{"word":"run","phonetic":"/rʌn/","meanings":[{"partOfSpeech":"verb","definitions":[{"definition":"To move swiftly.","example":"I run every day."},{"definition":"Second."}]},{"partOfSpeech":"noun","definitions":[{"definition":"An act of running."}]}]}]`),
		"/dict/api/v2/entries/en/qwzx": status(404),
	})

	env, _ := runTool(t, u, "english_word", `{"word":"Run"}`)
	if !env.OK || !strings.Contains(env.raw, `"phonetic":"/rʌn/"`) || !strings.Contains(env.raw, "To move swiftly.") || strings.Contains(env.raw, "Second.") || !strings.Contains(env.raw, `"part_of_speech":"noun"`) {
		t.Errorf("result = %s", env.raw)
	}
	env, _ = runTool(t, u, "english_word", `{"word":"qwzx"}`)
	wantError(t, env, "invalid_arguments", "no English word")
}

// A search gives the title in English, the score out of 100, the main
// studio and a description without HTML; nothing found is an argument
// error, and the season is the one the date falls in.
func TestAnime(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/anilist": ok(`{"data":{"Page":{"media":[{"title":{"romaji":"NARUTO","english":"Naruto","native":"NARUTO -ナルト-"},"averageScore":80,"episodes":220,"seasonYear":2002,"status":"FINISHED","genres":["Action"],"siteUrl":"https://anilist.co/anime/20","description":"Naruto <br>is a ninja.","studios":{"nodes":[{"name":"Studio Pierrot"}]}}]}}}`),
	})

	env, _ := runTool(t, u, "anime", `{"action":"search","query":"naruto"}`)

	for _, want := range []string{`"title":"Naruto"`, `"score":"80/100"`, `"studio":"Studio Pierrot"`, `"about":"Naruto is a ninja."`, `"status":"finished"`} {
		if !strings.Contains(env.raw, want) {
			t.Errorf("result does not contain %s: %s", want, env.raw)
		}
	}
	if env, _ := runTool(t, u, "anime", `{"action":"search"}`); env.OK {
		t.Errorf("a search without a query = %s", env.raw)
	}
	for month, want := range map[time.Month]string{time.December: "WINTER", time.January: "WINTER", time.April: "SPRING", time.July: "SUMMER", time.October: "FALL"} {
		if got, _ := animeSeason(time.Date(2026, month, 5, 0, 0, 0, 0, time.UTC)); got != want {
			t.Errorf("%s: season %s, want %s", month, got, want)
		}
	}
}
