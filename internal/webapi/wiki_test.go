package webapi

import (
	"net/http"
	"strings"
	"testing"
	"unicode"
)

type wikiResult struct {
	Title   string `json:"title"`
	Extract string `json:"extract"`
	URL     string `json:"url"`
	Lang    string `json:"lang"`
	Date    string `json:"date"`
	Events  []struct {
		Year int    `json:"year"`
		Text string `json:"text"`
	} `json:"events"`
	Source string `json:"source"`
}

const (
	uzSearch = "/wiki/uz/w/api.php"
	ruSearch = "/wiki/ru/w/api.php"
	enSearch = "/wiki/en/w/api.php"
)

func wikiRoutes(t *testing.T) map[string]stub {
	none := ok(fixture(t, "wiki_uz_none.json"))
	return map[string]stub{
		uzSearch:                                  none,
		ruSearch:                                  none,
		enSearch:                                  none,
		uzSearch + "?gsrsearch=Amir Temur":        ok(fixture(t, "wiki_uz_temur.json")),
		uzSearch + "?gsrsearch=Alisher Navoiy":    ok(fixture(t, "wiki_uz_navoiy.json")),
		ruSearch + "?gsrsearch=Навои":             ok(fixture(t, "wiki_ru_navoi.json")),
		enSearch + "?gsrsearch=Cristiano Ronaldo": ok(fixture(t, "wiki_en_ronaldo.json")),
		"/wiki/ru/api/rest_v1/feed/onthisday/selected/10/16": ok(fixture(t, "wiki_onthisday_ru.json")),
		"/wiki/en/api/rest_v1/feed/onthisday/selected/10/16": ok(fixture(t, "wiki_onthisday_en.json")),
		"/wiki/uz/api/rest_v1/feed/onthisday/selected/10/16": {status: http.StatusNotFound, body: fixture(t, "wiki_onthisday_uz.json")},
	}
}

func TestWikiLookup(t *testing.T) {
	tests := []struct {
		name  string
		args  string
		check func(t *testing.T, r wikiResult, u *upstream)
	}{
		{
			name: "an Uzbek article",
			args: `{"query":"Amir Temur"}`,
			check: func(t *testing.T, r wikiResult, u *upstream) {
				if r.Title != "Amir Temur" || r.Lang != "uz" || r.URL != "https://uz.wikipedia.org/wiki/Amir_Temur" || !strings.HasPrefix(r.Extract, "Amir Temur (toʻliq ismi") {
					t.Errorf("result = %+v", r)
				}
				if len([]rune(r.Extract)) > 600 || r.Source != "Wikipedia (CC BY-SA)" {
					t.Errorf("extract is %d characters, source %q", len([]rune(r.Extract)), r.Source)
				}
				request := u.requests(uzSearch)[0]
				query := request.URL.Query()
				if query.Get("exintro") != "1" || query.Get("explaintext") != "1" || query.Get("gsrlimit") != "1" || request.Header.Get("User-Agent") == "" {
					t.Errorf("request = %s", request.URL)
				}
			},
		},
		{
			name: "Uzbek Cyrillic is searched in Latin, without the hatnote",
			args: `{"query":"Алишер Навоий"}`,
			check: func(t *testing.T, r wikiResult, _ *upstream) {
				if r.Title != "Alisher Navoiy" || !strings.HasPrefix(r.Extract, "Alisher Navoiy (chigʻatoycha") {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name: "Russian when Uzbek has nothing, without stress marks",
			args: `{"query":"Навои"}`,
			check: func(t *testing.T, r wikiResult, u *upstream) {
				if r.Lang != "ru" || r.Title != "Алишер Навои" || !strings.HasPrefix(r.Extract, "Алишер Навои (узб.") || !strings.HasPrefix(r.URL, "https://ru.wikipedia.org/wiki/") {
					t.Errorf("result = %+v", r)
				}
				for _, c := range r.Extract {
					if unicode.Is(unicode.Mn, c) || unicode.Is(unicode.Cf, c) {
						t.Errorf("extract keeps %U", c)
					}
				}
				if got := u.requests(uzSearch)[0].URL.Query().Get("gsrsearch"); got != "Navoi" {
					t.Errorf("Uzbek search for %q, want the Latin spelling", got)
				}
			},
		},
		{
			name: "English asked first",
			args: `{"query":"Cristiano Ronaldo","lang":"en"}`,
			check: func(t *testing.T, r wikiResult, u *upstream) {
				if r.Lang != "en" || r.Title != "Cristiano Ronaldo" || len(u.requests(uzSearch)) != 0 {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name: "this day in history",
			args: `{"mode":"on_this_day","lang":"ru"}`,
			check: func(t *testing.T, r wikiResult, _ *upstream) {
				if r.Date != "2026-10-16" || r.Lang != "ru" || len(r.Events) != 6 || r.Events[0].Year != 2002 {
					t.Fatalf("result = %+v", r)
				}
				for _, event := range r.Events {
					if event.Text == "" || strings.Contains(event.Text, "на илл.") || strings.Contains(event.Text, "  ") {
						t.Errorf("event = %+v", event)
					}
				}
			},
		},
		{
			name: "this day in history has no Uzbek feed, so Russian",
			args: `{"mode":"on_this_day"}`,
			check: func(t *testing.T, r wikiResult, _ *upstream) {
				if r.Lang != "ru" || len(r.Events) != 6 {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name: "this day in English, spread over the years",
			args: `{"mode":"on_this_day","lang":"en"}`,
			check: func(t *testing.T, r wikiResult, _ *upstream) {
				if len(r.Events) != 6 || r.Events[0].Year != 2017 || r.Events[5].Year > 1950 {
					t.Errorf("events = %+v", r.Events)
				}
				for _, event := range r.Events {
					if strings.Contains(event.Text, "pictured") {
						t.Errorf("event = %+v", event)
					}
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			u := newUpstream(t, wikiRoutes(t))
			env := run(t, u.options(), "wiki_lookup", test.args)
			wantFits(t, env, registryLimit)
			test.check(t, result[wikiResult](t, env), u)
		})
	}
}

func TestWikiLookupFailures(t *testing.T) {
	tests := []struct {
		name     string
		override map[string]stub
		args     string
		code     string
		words    []string
	}{
		{name: "nothing anywhere", args: `{"query":"qwzxqy"}`, code: "invalid_arguments", words: []string{"qwzxqy"}},
		{name: "busy", override: map[string]stub{uzSearch + "?gsrsearch=Amir Temur": status(http.StatusTooManyRequests)}, args: `{"query":"Amir Temur"}`, code: "execution_failed", words: []string{"Wikipedia is busy"}},
		{name: "slow", override: map[string]stub{uzSearch + "?gsrsearch=Amir Temur": slow}, args: `{"query":"Amir Temur"}`, code: "timeout"},
		{name: "this day missing", override: map[string]stub{"/wiki/ru/api/rest_v1/feed/onthisday/selected/10/16": status(http.StatusNotFound)}, args: `{"mode":"on_this_day"}`, code: "execution_failed", words: []string{"404"}},
		{name: "this day empty", override: map[string]stub{"/wiki/ru/api/rest_v1/feed/onthisday/selected/10/16": ok(`{"selected":[]}`)}, args: `{"mode":"on_this_day"}`, code: "execution_failed"},
		{name: "no query", args: `{}`, code: "invalid_arguments"},
		{name: "a query that is a paragraph", args: `{"query":"` + strings.Repeat("a", 201) + `"}`, code: "invalid_arguments"},
		{name: "unknown language", args: `{"query":"x","lang":"de"}`, code: "invalid_arguments"},
		{name: "unknown mode", args: `{"query":"x","mode":"random"}`, code: "invalid_arguments"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := wikiRoutes(t)
			for path, answer := range test.override {
				routes[path] = answer
			}
			wantError(t, run(t, impatient(newUpstream(t, routes).options()), "wiki_lookup", test.args), test.code, test.words...)
		})
	}
}

func TestWikiLookupIsCached(t *testing.T) {
	u := newUpstream(t, wikiRoutes(t))
	registry := newRegistry(t, u.options())

	for range 2 {
		result[wikiResult](t, execute(t, registry, "wiki_lookup", `{"query":"Навои"}`))
	}
	if uz, ru := len(u.requests(uzSearch)), len(u.requests(ruSearch)); uz != 1 || ru != 1 {
		t.Errorf("asked uz %d and ru %d times, want once each", uz, ru)
	}
}

func TestDropPictured(t *testing.T) {
	for in, want := range map[string]string{
		"Háry János (poster pictured), an opera": "Háry János , an opera",
		"«капитан» (на илл.) обманом":            "«капитан»  обманом",
		"Disney (pictured) founded (1923) a":     "Disney  founded (1923) a",
		"an open (bracket":                       "an open (bracket",
	} {
		if got := dropPictured(in); got != want {
			t.Errorf("dropPictured(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLatinize(t *testing.T) {
	for cyrillic, latin := range map[string]string{
		"Алишер Навоий":   "Alisher Navoiy",
		"Ўзбекистон":      "Oʻzbekiston",
		"Ғафур Ғулом":     "Gʻafur Gʻulom",
		"Қарши":           "Qarshi",
		"Ҳамза":           "Hamza",
		"Шаҳрисабз":       "Shahrisabz",
		"Ер ва поезд":     "Yer va poyezd",
		"Чирчиқ":          "Chirchiq",
		"Юлдуз ёки Ялта":  "Yulduz yoki Yalta",
		"Амир Temur 1336": "Amir Temur 1336",
	} {
		if got := latinize(cyrillic); got != latin {
			t.Errorf("latinize(%q) = %q, want %q", cyrillic, got, latin)
		}
	}
}
