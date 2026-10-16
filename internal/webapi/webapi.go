// Package webapi gives the model live data from public web APIs: exchange
// rates, weather, prayer times, news headlines, Wikipedia, football and
// crypto prices. Every result names its source, and text written by outside
// sites comes back under fields the tool descriptions call untrusted.
package webapi

import (
	"bytes"
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

const defaultUserAgent = "telegram-ai-bot/1.0"

// Options configures the tools. Zero values mean production defaults.
type Options struct {
	// HTTPClient makes every request. Nil means a client with short
	// timeouts. Either way redirects may only stay on the same host.
	HTTPClient *http.Client
	// UserAgent identifies the bot to every API. Wikipedia and MET Norway
	// throttle anonymous agents, so give a contact, for example
	// "MariaBot/1.0 (+https://t.me/<bot>)".
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
	// Wikipedia is a format string with %s for the language code.
	Wikipedia string
	// News maps a language (uz, ru) to its feeds by source name.
	News map[string]map[string]string
}

func (b BaseURLs) withDefaults() BaseURLs {
	b.CBU = or(b.CBU, "https://cbu.uz/uz/arkhiv-kursov-valyut/json")
	b.CurrencyAPI = or(b.CurrencyAPI, "https://latest.currency-api.pages.dev/v1/currencies/usd.json")
	b.CurrencyAPIMirror = or(b.CurrencyAPIMirror, "https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/usd.json")
	b.OpenMeteo = or(b.OpenMeteo, "https://api.open-meteo.com")
	b.AirQuality = or(b.AirQuality, "https://air-quality-api.open-meteo.com")
	b.Geocoding = or(b.Geocoding, "https://geocoding-api.open-meteo.com")
	b.METNorway = or(b.METNorway, "https://api.met.no")
	b.Aladhan = or(b.Aladhan, "https://api.aladhan.com")
	b.FootballData = or(b.FootballData, "https://api.football-data.org")
	b.CoinGecko = or(b.CoinGecko, "https://api.coingecko.com")
	b.Wikipedia = or(b.Wikipedia, "https://%s.wikipedia.org")
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
		}
	}
	return b
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// apis is what the tools share: one client, one cache and the settings.
type apis struct {
	client        *client
	cache         *cache
	urls          BaseURLs
	location      *time.Location
	now           func() time.Time
	footballToken string
	coinGeckoKey  string
}

// Tools returns the live-data tools. Football is included only when
// FootballToken is set. The tools share a client and a cache, so call
// Tools once and register what it returns.
func Tools(opts Options) []tools.Tool {
	a := newAPIs(opts)
	return []tools.Tool{
		a.currencyTool(),
		a.weatherTool(),
	}
}

func newAPIs(opts Options) *apis {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Location == nil {
		opts.Location = defaultLocation()
	}
	return &apis{
		client:        newClient(opts.HTTPClient, or(opts.UserAgent, defaultUserAgent)),
		cache:         newCache(opts.Now),
		urls:          opts.BaseURLs.withDefaults(),
		location:      opts.Location,
		now:           opts.Now,
		footballToken: opts.FootballToken,
		coinGeckoKey:  opts.CoinGeckoKey,
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
// Future dates are refused, because the APIs silently answer them with
// another day's data.
func (a *apis) parseDay(value string) (time.Time, error) {
	if value == "" {
		return a.today(), nil
	}
	day, err := time.ParseInLocation(time.DateOnly, value, a.location)
	if err != nil {
		return time.Time{}, argumentError("date must look like YYYY-MM-DD, got %q", value)
	}
	if day.After(a.today()) {
		return time.Time{}, argumentError("%s is in the future", value)
	}
	return day, nil
}

// decodeArgs unmarshals arguments into a typed struct and rejects
// properties the tool does not define.
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
