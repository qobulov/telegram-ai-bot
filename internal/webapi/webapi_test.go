package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"telegram-ai-bot/internal/tools"
)

// registryLimit is the registry's default bound on a tool result.
const registryLimit = 4000

var tashkent = defaultLocation()

// testNow is when the fixtures were captured.
var testNow = time.Date(2026, 10, 16, 12, 0, 0, 0, tashkent)

// stub is one canned answer of the fake upstream.
type stub struct {
	status int // zero means 200
	body   string
	// slow holds the answer until the client gives up.
	slow bool
	// slowCalls makes only the first few requests to the path slow.
	slowCalls int
}

func ok(body string) stub  { return stub{body: body} }
func status(code int) stub { return stub{status: code, body: `{"message":"no"}`} }

var slow = stub{slow: true}

// upstream is every API at once: it answers by URL path and records the
// requests it saw.
type upstream struct {
	server *httptest.Server
	mu     sync.Mutex
	routes map[string]stub
	seen   []*http.Request
}

func newUpstream(t *testing.T, routes map[string]stub) *upstream {
	t.Helper()
	u := &upstream{routes: routes}
	u.server = httptest.NewServer(http.HandlerFunc(u.serve))
	t.Cleanup(u.server.Close)
	return u
}

func (u *upstream) serve(w http.ResponseWriter, r *http.Request) {
	// The copy keeps the body, which the handler's own request loses.
	body, _ := io.ReadAll(r.Body)
	seen := r.Clone(context.Background())
	seen.Body = io.NopCloser(bytes.NewReader(body))
	u.mu.Lock()
	u.seen = append(u.seen, seen)
	answer, found := u.route(r)
	u.mu.Unlock()
	if !found {
		http.NotFound(w, r)
		return
	}
	if answer.slow || len(u.requests(r.URL.Path)) <= answer.slowCalls {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		return
	}
	if answer.status != 0 {
		w.WriteHeader(answer.status)
	}
	_, _ = w.Write([]byte(answer.body))
}

// route finds the answer for r: a route written "path?name=value" wins over
// the bare path when the query has that parameter.
func (u *upstream) route(r *http.Request) (stub, bool) {
	for name, values := range r.URL.Query() {
		for _, value := range values {
			if answer, found := u.routes[r.URL.Path+"?"+name+"="+value]; found {
				return answer, true
			}
		}
	}
	answer, found := u.routes[r.URL.Path]
	return answer, found
}

// requests returns the requests made to path, in order.
func (u *upstream) requests(path string) []*http.Request {
	u.mu.Lock()
	defer u.mu.Unlock()
	var matching []*http.Request
	for _, r := range u.seen {
		if r.URL.Path == path {
			matching = append(matching, r)
		}
	}
	return matching
}

func (u *upstream) total() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.seen)
}

// options points every API at the fake upstream.
func (u *upstream) options() Options {
	base := u.server.URL
	return Options{
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
		UserAgent:  "MariaBot-test/1.0 (+https://t.me/test)",
		Location:   tashkent,
		Now:        func() time.Time { return testNow },
		BaseURLs: BaseURLs{
			CBU:               base + "/cbu",
			CurrencyAPI:       base + "/currency-api/usd.json",
			CurrencyAPIMirror: base + "/mirror/usd.json",
			OpenMeteo:         base + "/open-meteo",
			AirQuality:        base + "/air-quality",
			Geocoding:         base + "/geocoding",
			METNorway:         base + "/met",
			Aladhan:           base + "/aladhan",
			FootballData:      base + "/football",
			CoinGecko:         base + "/coingecko",
			Deezer:            base + "/deezer",
			RoneArena:         base + "/arena",
			Isan:              base + "/isan",
			USGS:              base + "/usgs",
			QRServer:          base + "/qr",
			FreeToGame:        base + "/ftg",
			CatAPI:            base + "/cat",
			Dictionary:        base + "/dict",
			AniList:           base + "/anilist",
			QuranCom:          base + "/qurancom",
			AlQuran:           base + "/alquran",
			Jina:              base + "/jina",
			OpenTrivia:        base + "/trivia",
			UselessFacts:      base + "/facts",
			AdviceSlip:        base + "/advice",
			ZenQuotes:         base + "/zen",
			DogCEO:            base + "/dog",
			OpenLibrary:       base + "/openlibrary",
			MealDB:            base + "/mealdb",
			GoldAPI:           base + "/gold",
			Lichess:           base + "/lichess",
			Jolpica:           base + "/jolpica",
			MathJS:            base + "/mathjs",
			Crossref:          base + "/crossref",
			ArXiv:             base + "/arxiv",
			Datamuse:          base + "/datamuse",
			Wolfram:           base + "/wolfram",
			Shikimori:         base + "/shikimori",
			TraceMoe:          base + "/tracemoe",
			CheapShark:        base + "/cheapshark",
			LaunchLibrary:     base + "/ll2",
			OpenDota:          base + "/opendota",
			Tavily:            base + "/tavily",
			TMDB:              base + "/tmdb",
			YouTube:           base + "/youtube",
			Wikipedia:         base + "/wiki/%s",
			News: map[string]map[string]string{
				"uz":       {"kun.uz": base + "/rss/kun", "gazeta.uz": base + "/rss/gazeta-oz", "uza.uz": base + "/rss/uza-oz", "daryo.uz": base + "/rss/daryo"},
				"ru":       {"gazeta.uz": base + "/rss/gazeta-ru", "uza.uz": base + "/rss/uza-ru", "daryo.uz": base + "/rss/daryo-ru"},
				"world-en": {"bbc.co.uk": base + "/rss/bbc-world"},
			},
		},
	}
}

// impatient makes the client give up quickly, for tests of slow servers.
func impatient(opts Options) Options {
	opts.HTTPClient = &http.Client{Timeout: 150 * time.Millisecond}
	return opts
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(data)
}

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	raw string
}

func newRegistry(t *testing.T, opts Options) *tools.Registry {
	t.Helper()
	registry, err := tools.NewRegistry(func(int64) bool { return false }, nil, Tools(opts)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

// execute runs a tool the way the agent does, through the registry, so its
// argument checks, timeout and size limit all apply.
func execute(t *testing.T, registry *tools.Registry, name, args string) envelope {
	t.Helper()
	output := registry.Execute(context.Background(), 1, name, args)
	var env envelope
	if err := json.Unmarshal([]byte(output), &env); err != nil {
		t.Fatalf("tool output is not JSON: %q", output)
	}
	env.raw = output
	return env
}

// run is execute with a fresh registry, and so a fresh cache.
func run(t *testing.T, opts Options, name, args string) envelope {
	t.Helper()
	return execute(t, newRegistry(t, opts), name, args)
}

// result decodes a successful result, failing the test on an error.
func result[T any](t *testing.T, env envelope) T {
	t.Helper()
	var value T
	if !env.OK {
		t.Fatalf("tool failed: %s", env.raw)
	}
	if err := json.Unmarshal(env.Result, &value); err != nil {
		t.Fatalf("decode result %s: %v", env.Result, err)
	}
	return value
}

// wantError checks a failed result's code and that its message mentions
// every one of words.
func wantError(t *testing.T, env envelope, code string, words ...string) {
	t.Helper()
	if env.OK || env.Error.Code != code {
		t.Fatalf("output = %s, want error code %q", env.raw, code)
	}
	for _, word := range words {
		if !strings.Contains(env.Error.Message, word) {
			t.Errorf("error %q does not mention %q", env.Error.Message, word)
		}
	}
}

// wantFits checks a result against the size the registry allows.
func wantFits(t *testing.T, env envelope, limit int) {
	t.Helper()
	if !env.OK {
		t.Fatalf("tool failed: %s", env.raw)
	}
	if len(env.raw) > limit {
		t.Errorf("result is %d bytes, over the %d limit", len(env.raw), limit)
	}
}

func TestToolsHaveStrictSchemas(t *testing.T) {
	opts := newUpstream(t, nil).options()
	opts.FootballToken = "token"
	for _, tool := range Tools(opts) {
		var schema struct {
			Type                 string `json:"type"`
			AdditionalProperties *bool  `json:"additionalProperties"`
		}
		if err := json.Unmarshal(tool.Schema, &schema); err != nil {
			t.Errorf("%s: schema is not JSON: %v", tool.Name, err)
			continue
		}
		if schema.Type != "object" || schema.AdditionalProperties == nil || *schema.AdditionalProperties {
			t.Errorf("%s: schema must be an object without additional properties", tool.Name)
		}
		if tool.Description == "" || len(tool.Description) > 400 {
			t.Errorf("%s: description is %d bytes; it is sent on every turn", tool.Name, len(tool.Description))
		}
	}
	if _, err := tools.NewRegistry(func(int64) bool { return false }, nil, Tools(opts)...); err != nil {
		t.Errorf("NewRegistry: %v", err)
	}
}

func TestToolsRejectUnknownArguments(t *testing.T) {
	opts := newUpstream(t, nil).options()
	opts.FootballToken = "token"
	registry := newRegistry(t, opts)
	for _, tool := range Tools(opts) {
		wantError(t, execute(t, registry, tool.Name, `{"surprise":1}`), "invalid_arguments")
	}
}

func toolNames(list []tools.Tool) string {
	var names []string
	for _, tool := range list {
		names = append(names, tool.Name)
	}
	return strings.Join(names, ",")
}

func TestToolsListsTheLiveDataTools(t *testing.T) {
	opts := newUpstream(t, nil).options()
	if got, want := toolNames(Tools(opts)), "currency_rate,get_weather,prayer_times,news_headlines,wiki_lookup,crypto_price,music_search,mlbb,earthquakes,qr_code,free_games,cat_picture,english_word,anime,quran,read_page,fun,more_info,study,games"; got != want {
		t.Errorf("tools = %s, want %s", got, want)
	}
}

func TestDefaultUserAgentIsNotRefused(t *testing.T) {
	// gazeta.uz answers any agent containing "telegram" with an empty feed.
	if agent := newAPIs(Options{}).client.userAgent; agent == "" || strings.Contains(strings.ToLower(agent), "telegram") {
		t.Errorf("default User-Agent = %q", agent)
	}
}

func TestOptionsDefaultToProductionURLs(t *testing.T) {
	urls := BaseURLs{}.withDefaults()
	all := []string{urls.CBU, urls.CurrencyAPI, urls.CurrencyAPIMirror, urls.OpenMeteo, urls.AirQuality, urls.Geocoding,
		urls.METNorway, urls.Aladhan, urls.FootballData, urls.CoinGecko, strings.ReplaceAll(urls.Wikipedia, "%s", "uz")}
	for _, feeds := range urls.News {
		for _, feed := range feeds {
			all = append(all, feed)
		}
	}
	if len(urls.News["uz"]) != 4 || len(urls.News["ru"]) != 3 {
		t.Errorf("feeds = %v", urls.News)
	}
	for _, value := range all {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			t.Errorf("default %q is not an https URL", value)
		}
	}
}
