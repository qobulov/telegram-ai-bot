// Package webapi gives the model live data from public web APIs: exchange
// rates, weather, prayer times, news headlines, Wikipedia, football, crypto
// prices, songs and Mobile Legends heroes. Every result names its source, and text written by outside
// sites comes back under fields the tool descriptions call untrusted.
package webapi

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"telegram-ai-bot/internal/tools"
)

// primaryTimeout is how long a tool with a fallback waits for its first
// source, so the second still has time inside the tool's deadline.
const primaryTimeout = 3 * time.Second

// defaultUserAgent must not contain "telegram": see Options.UserAgent.
const defaultUserAgent = "MariaBot/1.0"

// Options configures the tools. Zero values mean production defaults.
type Options struct {
	// HTTPClient makes every request. Nil means a client with short
	// timeouts. Either way redirects may only stay on the same host.
	HTTPClient *http.Client
	// UserAgent identifies the bot to every API. Wikipedia and MET Norway
	// throttle anonymous agents, so give a contact, for example
	// "MariaBot/1.0 (+https://t.me/<bot>)". Keep the word "telegram" out
	// of it (a t.me link is fine): gazeta.uz answers any agent containing
	// it with an empty feed.
	UserAgent string
	// Location decides what "today" is. Nil means Asia/Tashkent.
	Location *time.Location
	Now      func() time.Time
	// FootballToken is a football-data.org key. Without it there is no
	// football tool.
	FootballToken string
	// CoinGeckoKey is an optional CoinGecko demo key. Keyless calls share
	// a quota with every other client on the same IP.
	CoinGeckoKey string
	// TavilyKey is a Tavily key. Without it there is no web search.
	TavilyKey string
	// TMDBToken is a TMDB read access token. Without it there is no
	// movies tool.
	TMDBToken string
	// YouTubeKey is a YouTube Data API key. Without it there is no
	// youtube_search tool.
	YouTubeKey string
	// WolframAppID is a Wolfram|Alpha AppID. Without it there is no
	// wolfram tool.
	WolframAppID string
	BaseURLs     BaseURLs
}

// BaseURLs says where each API lives. Empty fields mean the production
// address; tests point them at a local server.
type BaseURLs struct {
	// CBU is the Central Bank's JSON archive, without the currency code.
	CBU string
	// CurrencyAPI and CurrencyAPIMirror are full URLs of usd.json.
	CurrencyAPI       string
	CurrencyAPIMirror string
	OpenMeteo         string
	AirQuality        string
	Geocoding         string
	METNorway         string
	Aladhan           string
	FootballData      string
	CoinGecko         string
	Deezer            string
	RoneArena         string
	Tavily            string
	TMDB              string
	YouTube           string
	Isan              string
	USGS              string
	QRServer          string
	FreeToGame        string
	CatAPI            string
	Dictionary        string
	AniList           string
	QuranCom          string
	AlQuran           string
	Jina              string
	OpenTrivia        string
	UselessFacts      string
	AdviceSlip        string
	ZenQuotes         string
	DogCEO            string
	OpenLibrary       string
	MealDB            string
	GoldAPI           string
	Lichess           string
	Jolpica           string
	MathJS            string
	Crossref          string
	ArXiv             string
	Datamuse          string
	Wolfram           string
	Shikimori         string
	TraceMoe          string
	SteamStore        string
	LaunchLibrary     string
	OpenDota          string
	// Wikipedia is a format string with %s for the language code.
	Wikipedia string
	// News maps a language (uz, ru), or world- and a language for the
	// BBC, to its feeds by source name. The name
	// is the site's domain, such as kun.uz: a headline's link must be on
	// it.
	News map[string]map[string]string
}

func (b BaseURLs) withDefaults() BaseURLs {
	b.CBU = cmp.Or(b.CBU, "https://cbu.uz/uz/arkhiv-kursov-valyut/json")
	b.CurrencyAPI = cmp.Or(b.CurrencyAPI, "https://latest.currency-api.pages.dev/v1/currencies/usd.json")
	b.CurrencyAPIMirror = cmp.Or(b.CurrencyAPIMirror, "https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/usd.json")
	b.OpenMeteo = cmp.Or(b.OpenMeteo, "https://api.open-meteo.com")
	b.AirQuality = cmp.Or(b.AirQuality, "https://air-quality-api.open-meteo.com")
	b.Geocoding = cmp.Or(b.Geocoding, "https://geocoding-api.open-meteo.com")
	b.METNorway = cmp.Or(b.METNorway, "https://api.met.no")
	b.Aladhan = cmp.Or(b.Aladhan, "https://api.aladhan.com")
	b.FootballData = cmp.Or(b.FootballData, "https://api.football-data.org")
	b.CoinGecko = cmp.Or(b.CoinGecko, "https://api.coingecko.com")
	b.Deezer = cmp.Or(b.Deezer, "https://api.deezer.com")
	b.RoneArena = cmp.Or(b.RoneArena, "https://arena.rone.dev")
	b.Tavily = cmp.Or(b.Tavily, "https://api.tavily.com")
	b.TMDB = cmp.Or(b.TMDB, "https://api.themoviedb.org")
	b.YouTube = cmp.Or(b.YouTube, "https://www.googleapis.com/youtube/v3")
	b.Isan = cmp.Or(b.Isan, "https://api.isan.eu.org")
	b.USGS = cmp.Or(b.USGS, "https://earthquake.usgs.gov")
	b.QRServer = cmp.Or(b.QRServer, "https://api.qrserver.com")
	b.FreeToGame = cmp.Or(b.FreeToGame, "https://www.freetogame.com")
	b.CatAPI = cmp.Or(b.CatAPI, "https://api.thecatapi.com")
	b.Dictionary = cmp.Or(b.Dictionary, "https://api.dictionaryapi.dev")
	b.AniList = cmp.Or(b.AniList, "https://graphql.anilist.co")
	b.QuranCom = cmp.Or(b.QuranCom, "https://api.quran.com")
	b.AlQuran = cmp.Or(b.AlQuran, "https://api.alquran.cloud")
	b.Jina = cmp.Or(b.Jina, "https://r.jina.ai")
	b.OpenTrivia = cmp.Or(b.OpenTrivia, "https://opentdb.com")
	b.UselessFacts = cmp.Or(b.UselessFacts, "https://uselessfacts.jsph.pl")
	b.AdviceSlip = cmp.Or(b.AdviceSlip, "https://api.adviceslip.com")
	b.ZenQuotes = cmp.Or(b.ZenQuotes, "https://zenquotes.io")
	b.DogCEO = cmp.Or(b.DogCEO, "https://dog.ceo")
	b.OpenLibrary = cmp.Or(b.OpenLibrary, "https://openlibrary.org")
	b.MealDB = cmp.Or(b.MealDB, "https://www.themealdb.com")
	b.GoldAPI = cmp.Or(b.GoldAPI, "https://api.gold-api.com")
	b.Lichess = cmp.Or(b.Lichess, "https://lichess.org")
	b.Jolpica = cmp.Or(b.Jolpica, "https://api.jolpi.ca")
	b.MathJS = cmp.Or(b.MathJS, "https://api.mathjs.org")
	b.Crossref = cmp.Or(b.Crossref, "https://api.crossref.org")
	b.ArXiv = cmp.Or(b.ArXiv, "https://export.arxiv.org")
	b.Datamuse = cmp.Or(b.Datamuse, "https://api.datamuse.com")
	b.Wolfram = cmp.Or(b.Wolfram, "https://api.wolframalpha.com")
	b.Shikimori = cmp.Or(b.Shikimori, "https://shikimori.io")
	b.TraceMoe = cmp.Or(b.TraceMoe, "https://api.trace.moe")
	b.SteamStore = cmp.Or(b.SteamStore, "https://store.steampowered.com")
	b.LaunchLibrary = cmp.Or(b.LaunchLibrary, "https://ll.thespacedevs.com")
	b.OpenDota = cmp.Or(b.OpenDota, "https://api.opendota.com")
	b.Wikipedia = cmp.Or(b.Wikipedia, "https://%s.wikipedia.org")
	if b.News == nil {
		b.News = map[string]map[string]string{
			"uz": {
				"kun.uz":    "https://kun.uz/news/rss",
				"gazeta.uz": "https://www.gazeta.uz/oz/rss/",
				"uza.uz":    "https://uza.uz/oz/rss",
				"daryo.uz":  "https://daryo.uz/rss/",
			},
			"ru": {
				"gazeta.uz": "https://www.gazeta.uz/ru/rss/",
				"uza.uz":    "https://uza.uz/ru/rss",
				"daryo.uz":  "https://daryo.uz/ru/rss-full/",
			},
			// The BBC's Uzbek service writes in Cyrillic and links to
			// bbc.com; its Russian and world services link to bbc.co.uk.
			"world-uz": {"bbc.com": "https://feeds.bbci.co.uk/uzbek/cyr/rss.xml"},
			"world-ru": {"bbc.co.uk": "https://feeds.bbci.co.uk/russian/rss.xml"},
			"world-en": {"bbc.co.uk": "https://feeds.bbci.co.uk/news/world/rss.xml"},
		}
	}
	return b
}

// apis is what the tools share: the clients, one cache and the settings.
type apis struct {
	client *client
	// patient waits longer, for the web search and the MLBB statistics,
	// which often take over 3 s.
	patient       *client
	cache         *cache
	urls          BaseURLs
	location      *time.Location
	now           func() time.Time
	footballToken string
	coinGeckoKey  string
	tavilyKey     string
	tmdbToken     string
	youtubeKey    string
	wolframAppID  string
}

// Tools returns the live-data tools. Football is included only when
// FootballToken is set. The tools share a client and a cache, so call
// Tools once and register what it returns.
func Tools(opts Options) []tools.Tool {
	a := newAPIs(opts)
	list := []tools.Tool{
		a.currencyTool(),
		a.weatherTool(),
		a.prayerTool(),
		a.newsTool(),
		a.wikiTool(),
		a.cryptoTool(),
		a.musicTool(),
		a.mlbbTool(),
		a.quakeTool(),
		a.qrTool(),
		a.freeGamesTool(),
		a.catTool(),
		a.dictionaryTool(),
		a.animeTool(),
		a.quranTool(),
		a.readerTool(),
		a.funTool(),
		a.moreTool(),
		a.studyTool(),
		a.gamesTool(),
	}
	if a.footballToken != "" {
		list = append(list, a.footballTool())
	}
	if a.tavilyKey != "" {
		list = append(list, a.searchTool())
	}
	if a.tmdbToken != "" {
		list = append(list, a.moviesTool())
	}
	if a.youtubeKey != "" {
		list = append(list, a.youtubeTool())
	}
	if a.wolframAppID != "" {
		list = append(list, a.wolframTool())
	}
	return list
}

func newAPIs(opts Options) *apis {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Location == nil {
		opts.Location = defaultLocation()
	}
	return &apis{
		client:        newClient(opts.HTTPClient, cmp.Or(opts.UserAgent, defaultUserAgent)),
		patient:       patientClient(opts.HTTPClient, cmp.Or(opts.UserAgent, defaultUserAgent)),
		cache:         newCache(opts.Now),
		urls:          opts.BaseURLs.withDefaults(),
		location:      opts.Location,
		now:           opts.Now,
		footballToken: opts.FootballToken,
		coinGeckoKey:  opts.CoinGeckoKey,
		tavilyKey:     opts.TavilyKey,
		tmdbToken:     opts.TMDBToken,
		youtubeKey:    opts.YouTubeKey,
		wolframAppID:  opts.WolframAppID,
	}
}

// defaultLocation is Tashkent, or its fixed offset on a host without
// timezone data (Uzbekistan has no daylight saving time).
func defaultLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Tashkent")
	if err != nil {
		return time.FixedZone("Asia/Tashkent", 5*60*60)
	}
	return location
}

// today is the current date where the bot lives, at midnight.
func (a *apis) today() time.Time {
	now := a.now().In(a.location)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, a.location)
}

// parseDay reads an optional YYYY-MM-DD argument; empty means today.
func (a *apis) parseDay(value string) (time.Time, error) {
	if value == "" {
		return a.today(), nil
	}
	day, err := time.ParseInLocation(time.DateOnly, value, a.location)
	if err != nil {
		return time.Time{}, argumentError("date must look like YYYY-MM-DD, got %q", value)
	}
	return day, nil
}

// decodeArgs unmarshals arguments into a typed struct and rejects
// properties the tool does not define. It mirrors the unexported
// decodeArgs in internal/tools on purpose; keep the two alike, or export
// that one when the tools are wired in.
func decodeArgs(raw json.RawMessage, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return argumentError("arguments do not match the tool's schema: %v", err)
	}
	return nil
}

func argumentError(format string, args ...any) error {
	return &tools.ArgumentError{Message: fmt.Sprintf(format, args...)}
}

// shouldFallBack reports whether a failed first source is worth a second:
// not when the arguments are wrong or the tool's own time is up.
func shouldFallBack(ctx context.Context, err error) bool {
	var argErr *tools.ArgumentError
	return !errors.As(err, &argErr) && ctx.Err() == nil
}

// round keeps decimals digits after the point.
func round(value float64, decimals int) float64 {
	scale := math.Pow(10, float64(decimals))
	return math.Round(value*scale) / scale
}
