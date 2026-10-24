package webapi

import (
	"slices"
	"strings"
	"testing"
)

func TestTriviaGivesShuffledOptionsWithTheAnswer(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/trivia/api.php":                ok(`{"response_code":0,"results":[{"category":"Geography","difficulty":"easy","question":"What is the capital of &quot;Uzbekistan&quot;?","correct_answer":"Tashkent","incorrect_answers":["Samarkand","Bukhara","Khiva"]}]}`),
		"/trivia/api.php?category=99999": ok(`{}`),
	})

	out := result[struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
		Answer   string   `json:"answer"`
	}](t, run(t, u.options(), "fun", `{"action":"trivia","category":"geography","difficulty":"easy"}`))

	if out.Question != `What is the capital of "Uzbekistan"?` || out.Answer != "Tashkent" || len(out.Options) != 4 || !slices.Contains(out.Options, "Tashkent") {
		t.Errorf("trivia = %+v", out)
	}
	if q := u.requests("/trivia/api.php")[0].URL.Query(); q.Get("category") != "22" || q.Get("difficulty") != "easy" || q.Get("type") != "multiple" {
		t.Errorf("query = %v", q)
	}
	wantError(t, run(t, u.options(), "fun", `{"action":"trivia","category":"cooking"}`), "invalid_arguments")
}

func TestTriviaRateLimitIsBusy(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/trivia/api.php": ok(`{"response_code":5,"results":[]}`)})
	if env := run(t, u.options(), "fun", `{"action":"trivia"}`); env.OK || !strings.Contains(env.Error.Message, "busy") {
		t.Errorf("env = %s", env.raw)
	}
}

func TestFunTexts(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/facts/api/v2/facts/random": ok(`{"text":"Honey never spoils."}`),
		"/advice/advice":             ok(`{"slip": { "id": 58, "advice": "Don't give a speech. Put on a show."}}`),
		"/zen/api/random":            ok(`[{"q":"Well begun is half done.","a":"Aristotle","h":"<b>x</b>"}]`),
	})
	for action, want := range map[string]string{
		"fact":   `"fact":"Honey never spoils."`,
		"advice": `"advice":"Don't give a speech. Put on a show."`,
		"quote":  `"author":"Aristotle"`,
	} {
		if env := run(t, u.options(), "fun", `{"action":"`+action+`"}`); !env.OK || !strings.Contains(env.raw, want) {
			t.Errorf("%s = %s, want %s", action, env.raw, want)
		}
	}
}

func TestDogPicture(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/dog/img.jpg": ok("\xff\xd8\xffJPEG")})
	u.routes["/dog/api/breeds/image/random"] = ok(`{"message":"` + u.server.URL + `/dog/img.jpg","status":"success"}`)

	env, sink := runTool(t, u, "fun", `{"action":"dog"}`)

	if drawings := sink.Take(); !env.OK || len(drawings) != 1 || drawings[0].MimeType != "image/jpeg" {
		t.Errorf("env %s, drawings %+v", env.raw, drawings)
	}
}

// A fact about sex, drink or drugs is passed over for another.
func TestUnfitFactsAreSkipped(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/facts/api/v2/facts/random": ok(`{"text":"A band was named after a sexual device."}`)})
	if env := run(t, u.options(), "fun", `{"action":"fact"}`); env.OK || len(u.requests("/facts/api/v2/facts/random")) != factTries {
		t.Errorf("env = %s after %d tries", env.raw, len(u.requests("/facts/api/v2/facts/random")))
	}
	for _, fine := range []string{"Honey never spoils.", "Essex is a county.", "Wineries? No: vineyards."} {
		if unfitFact.MatchString(fine) {
			t.Errorf("%q was taken for unfit", fine)
		}
	}
}

func TestJokeIsCleanAndTwoPartJokesKeepTheirPunchline(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/jokeapi/joke/Misc,Programming,Pun,Spooky": ok(`{"error":false,"type":"twopart","setup":"Why do programmers prefer dark mode?","delivery":"Because light attracts bugs."}`)})

	env := run(t, u.options(), "fun", `{"action":"joke"}`)

	if !env.OK || !strings.Contains(env.raw, `"punchline":"Because light attracts bugs."`) {
		t.Errorf("joke = %s", env.raw)
	}
	if q := u.requests("/jokeapi/joke/Misc,Programming,Pun,Spooky")[0].URL.RawQuery; !strings.Contains(q, "safe-mode") || !strings.Contains(q, "religious") || !strings.Contains(q, "political") {
		t.Errorf("query = %s, want safe mode and the offending kinds left out", q)
	}
}
