package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"telegram-ai-bot/internal/tools"
)

const (
	weatherTTL     = 10 * time.Minute
	geocodingTTL   = 24 * time.Hour
	weatherTimeout = 6 * time.Second
	maxCityName    = 100
	// metBodyLimit leaves room above MET Norway's usual 40 KB, so the
	// fallback does not break when they add a field.
	metBodyLimit = 128 << 10

	openMeteoSource = "Open-Meteo (CC BY 4.0)"
	metSource       = "MET Norway (CC BY 4.0)"
	airNote         = "model estimate (CAMS), not a station reading"
)

type weatherArgs struct {
	City       string `json:"city"`
	Days       *int   `json:"days"`
	IncludeAir bool   `json:"include_air"`
}

type weatherOutput struct {
	Place  string       `json:"place"`
	Now    weatherNow   `json:"now"`
	Daily  []weatherDay `json:"daily"`
	Air    *airOutput   `json:"air,omitempty"`
	Note   string       `json:"note,omitempty"`
	Source string       `json:"source"`
}

type weatherNow struct {
	TempC float64 `json:"temp_c"`
	// FeelsLikeC is missing when MET Norway answers.
	FeelsLikeC *float64 `json:"feels_like_c,omitempty"`
	Conditions string   `json:"conditions"`
	WindKmh    float64  `json:"wind_kmh"`
}

type weatherDay struct {
	Date          string  `json:"date"`
	MinC          float64 `json:"min_c"`
	MaxC          float64 `json:"max_c"`
	RainChancePct *int    `json:"rain_chance_pct,omitempty"`
	Sunrise       string  `json:"sunrise,omitempty"`
	Sunset        string  `json:"sunset,omitempty"`
}

type airOutput struct {
	USAQI    int     `json:"us_aqi"`
	Category string  `json:"category"`
	PM25     float64 `json:"pm2_5"`
	Note     string  `json:"note"`
}

func (a *apis) weatherTool() tools.Tool {
	return tools.Tool{
		Name:        "get_weather",
		Description: "Current weather and a forecast of up to 3 days for a city (best for Uzbekistan, any spelling), optionally with air quality. Temperatures in °C, conditions in Uzbek.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"city": {"type": "string", "description": "City name in any spelling, e.g. Toshkent, Самарканд, Moscow."},
				"days": {"type": "integer", "minimum": 1, "maximum": 3, "description": "Days of forecast from today. Default 1."},
				"include_air": {"type": "boolean", "description": "Also give air quality (US AQI and PM2.5)."}
			},
			"required": ["city"],
			"additionalProperties": false
		}`),
		Timeout: weatherTimeout,
		Run:     a.weather,
	}
}

// spot is where to forecast: a known place or a geocoder's answer.
type spot struct {
	name     string
	lat, lon float64
}

// coordinate keeps at most four decimals, which MET Norway requires and
// which is about 10 m, plenty for weather.
func coordinate(value float64) string {
	return strconv.FormatFloat(round(value, 4), 'f', -1, 64)
}

func (s spot) key() string { return coordinate(s.lat) + "," + coordinate(s.lon) }

func (a *apis) weather(ctx context.Context, raw json.RawMessage) (any, error) {
	var args weatherArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	days := 1
	if args.Days != nil {
		days = *args.Days
	}
	city := strings.TrimSpace(args.City)
	if city == "" || len([]rune(city)) > maxCityName || days < 1 || days > 3 {
		return nil, argumentError("city must be a name of at most %d characters and days 1 to 3", maxCityName)
	}
	where, err := a.findSpot(ctx, city)
	if err != nil {
		return nil, err
	}

	air := make(chan airAnswer, 1)
	if args.IncludeAir {
		go func() { air <- a.airQuality(ctx, where) }()
	}
	out, err := a.forecast(ctx, where, days)
	if err != nil {
		return nil, err
	}
	if args.IncludeAir {
		answer := <-air
		out.Air = answer.air
		if answer.err != nil {
			out.Note = "air quality is not available right now"
		}
	}
	return out, nil
}

// forecast asks Open-Meteo and falls back to MET Norway when it fails or
// is slow.
func (a *apis) forecast(ctx context.Context, where spot, days int) (weatherOutput, error) {
	primary, cancel := context.WithTimeout(ctx, primaryTimeout)
	defer cancel()
	out, err := a.openMeteo(primary, where, days)
	if err == nil || !shouldFallBack(ctx, err) {
		return out, err
	}
	out, fallbackErr := a.metNorway(ctx, where, days)
	if fallbackErr != nil {
		return out, fmt.Errorf("%w; %w", err, fallbackErr)
	}
	return out, nil
}

type openMeteoForecast struct {
	Current struct {
		Temperature float64 `json:"temperature_2m"`
		FeelsLike   float64 `json:"apparent_temperature"`
		WeatherCode int     `json:"weather_code"`
		WindSpeed   float64 `json:"wind_speed_10m"`
	} `json:"current"`
	Daily struct {
		Time       []string   `json:"time"`
		Max        []float64  `json:"temperature_2m_max"`
		Min        []float64  `json:"temperature_2m_min"`
		RainChance []*float64 `json:"precipitation_probability_max"`
		Sunrise    []string   `json:"sunrise"`
		Sunset     []string   `json:"sunset"`
	} `json:"daily"`
}

func (a *apis) openMeteo(ctx context.Context, where spot, days int) (weatherOutput, error) {
	query := url.Values{
		"latitude":      {coordinate(where.lat)},
		"longitude":     {coordinate(where.lon)},
		"current":       {"temperature_2m,apparent_temperature,weather_code,wind_speed_10m"},
		"daily":         {"temperature_2m_max,temperature_2m_min,precipitation_probability_max,sunrise,sunset"},
		"timezone":      {"Asia/Tashkent"},
		"forecast_days": {strconv.Itoa(days)},
	}
	key := fmt.Sprintf("open-meteo:%s:%d", where.key(), days)
	forecast, err := remember(a.cache, key, weatherTTL, func() (openMeteoForecast, error) {
		var forecast openMeteoForecast
		err := a.client.getJSON(ctx, call{service: "Open-Meteo", url: a.urls.OpenMeteo + "/v1/forecast?" + query.Encode()}, &forecast)
		if err == nil && len(forecast.Daily.Time) == 0 {
			err = errors.New("Open-Meteo sent no forecast")
		}
		return forecast, openMeteoError(err)
	})
	if err != nil {
		return weatherOutput{}, err
	}
	return forecast.output(where.name, days), nil
}

func (f openMeteoForecast) output(place string, days int) weatherOutput {
	feelsLike := f.Current.FeelsLike
	out := weatherOutput{
		Place: place,
		Now: weatherNow{
			TempC:      f.Current.Temperature,
			FeelsLikeC: &feelsLike,
			Conditions: wmoConditions(f.Current.WeatherCode),
			WindKmh:    f.Current.WindSpeed,
		},
		Source: openMeteoSource,
	}
	daily := f.Daily
	for i := 0; i < days && i < len(daily.Time); i++ {
		day := weatherDay{
			Date:    daily.Time[i],
			MaxC:    at(daily.Max, i),
			MinC:    at(daily.Min, i),
			Sunrise: hourMinute(at(daily.Sunrise, i)),
			Sunset:  hourMinute(at(daily.Sunset, i)),
		}
		if chance := at(daily.RainChance, i); chance != nil {
			percent := int(*chance)
			day.RainChancePct = &percent
		}
		out.Daily = append(out.Daily, day)
	}
	return out
}

// at is values[i], or the zero value when Open-Meteo sent a short list.
func at[T any](values []T, i int) T {
	var zero T
	if i >= len(values) {
		return zero
	}
	return values[i]
}

// hourMinute turns "2026-10-16T06:35" into "06:35".
func hourMinute(timestamp string) string {
	_, hhmm, _ := strings.Cut(timestamp, "T")
	return hhmm
}

// openMeteoError adds Open-Meteo's own explanation to a 400 answer, which
// comes as {"error": true, "reason": "..."}.
func openMeteoError(err error) error {
	var status *statusError
	if !errors.As(err, &status) || status.status != 400 {
		return err
	}
	var answer struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal(status.body, &answer) != nil || answer.Reason == "" {
		return err
	}
	return fmt.Errorf("Open-Meteo refused the request: %s", truncate(answer.Reason, 200))
}

type airAnswer struct {
	air *airOutput
	err error
}

func (a *apis) airQuality(ctx context.Context, where spot) airAnswer {
	query := url.Values{
		"latitude":  {coordinate(where.lat)},
		"longitude": {coordinate(where.lon)},
		"current":   {"us_aqi,pm2_5"},
		"timezone":  {"Asia/Tashkent"},
	}
	type answer struct {
		Current struct {
			USAQI *float64 `json:"us_aqi"`
			PM25  float64  `json:"pm2_5"`
		} `json:"current"`
	}
	got, err := remember(a.cache, "air:"+where.key(), weatherTTL, func() (answer, error) {
		var got answer
		err := a.client.getJSON(ctx, call{service: "Open-Meteo air quality", url: a.urls.AirQuality + "/v1/air-quality?" + query.Encode()}, &got)
		if err == nil && got.Current.USAQI == nil {
			err = errors.New("Open-Meteo sent no air quality")
		}
		return got, err
	})
	if err != nil {
		return airAnswer{err: err}
	}
	aqi := int(*got.Current.USAQI)
	return airAnswer{air: &airOutput{USAQI: aqi, Category: aqiCategory(aqi), PM25: got.Current.PM25, Note: airNote}}
}

// metForecast is the part of MET Norway's compact forecast the tool reads:
// hourly, then six-hourly, points in UTC.
type metForecast struct {
	Properties struct {
		Timeseries []metPoint `json:"timeseries"`
	} `json:"properties"`
}

type metPoint struct {
	Time time.Time `json:"time"`
	Data struct {
		Instant struct {
			Details struct {
				AirTemperature float64 `json:"air_temperature"`
				WindSpeed      float64 `json:"wind_speed"`
			} `json:"details"`
		} `json:"instant"`
		Next1Hours *metSummary `json:"next_1_hours"`
		Next6Hours *metSummary `json:"next_6_hours"`
	} `json:"data"`
}

type metSummary struct {
	Summary struct {
		SymbolCode string `json:"symbol_code"`
	} `json:"summary"`
}

func (p metPoint) symbol() string {
	switch {
	case p.Data.Next1Hours != nil:
		return p.Data.Next1Hours.Summary.SymbolCode
	case p.Data.Next6Hours != nil:
		return p.Data.Next6Hours.Summary.SymbolCode
	default:
		return ""
	}
}

func (a *apis) metNorway(ctx context.Context, where spot, days int) (weatherOutput, error) {
	query := url.Values{"lat": {coordinate(where.lat)}, "lon": {coordinate(where.lon)}}
	forecast, err := remember(a.cache, "met:"+where.key(), weatherTTL, func() (metForecast, error) {
		var forecast metForecast
		err := a.client.getJSON(ctx, call{service: "MET Norway", url: a.urls.METNorway + "/weatherapi/locationforecast/2.0/compact?" + query.Encode(), limit: metBodyLimit}, &forecast)
		if err == nil && len(forecast.Properties.Timeseries) == 0 {
			err = errors.New("MET Norway sent no forecast")
		}
		return forecast, err
	})
	if err != nil {
		return weatherOutput{}, err
	}
	return a.metOutput(forecast.Properties.Timeseries, where.name, days), nil
}

// metOutput takes the latest point as "now" and the minimum and maximum
// of each local day as the forecast. MET Norway has no sunrise or chance
// of rain.
func (a *apis) metOutput(points []metPoint, place string, days int) weatherOutput {
	current := points[0]
	now := a.now()
	for _, p := range points {
		if p.Time.After(now) {
			break
		}
		current = p
	}
	details := current.Data.Instant.Details
	out := weatherOutput{
		Place: place,
		Now: weatherNow{
			TempC:      details.AirTemperature,
			Conditions: metConditions(current.symbol()),
			WindKmh:    round(details.WindSpeed*3.6, 1),
		},
		Source: metSource,
	}
	today := a.today()
	for i := range days {
		date := today.AddDate(0, 0, i).Format(time.DateOnly)
		day, found := metDay(points, date, a.location)
		if !found {
			break
		}
		out.Daily = append(out.Daily, day)
	}
	return out
}

func metDay(points []metPoint, date string, location *time.Location) (weatherDay, bool) {
	day := weatherDay{Date: date}
	found := false
	for _, p := range points {
		if p.Time.In(location).Format(time.DateOnly) != date {
			continue
		}
		temperature := p.Data.Instant.Details.AirTemperature
		if !found || temperature < day.MinC {
			day.MinC = temperature
		}
		if !found || temperature > day.MaxC {
			day.MaxC = temperature
		}
		found = true
	}
	return day, found
}

// findSpot looks in the place table first and asks the geocoder only on a
// miss: Cyrillic names in Russian, Latin ones in Uzbek and then English.
func (a *apis) findSpot(ctx context.Context, city string) (spot, error) {
	if p, found := lookupPlace(city); found {
		return spot{name: p.name, lat: p.lat, lon: p.lon}, nil
	}
	languages := []string{"uz", "en"}
	if hasCyrillic(city) {
		languages = []string{"ru"}
	}
	for _, language := range languages {
		found, ok, err := a.geocode(ctx, city, language)
		if err != nil || ok {
			return found, err
		}
	}
	return spot{}, argumentError("no place called %q was found; try the city's usual name", city)
}

type geocodingResult struct {
	Name        string  `json:"name"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	FeatureCode string  `json:"feature_code"`
	CountryCode string  `json:"country_code"`
	Country     string  `json:"country"`
}

func (a *apis) geocode(ctx context.Context, city, language string) (spot, bool, error) {
	query := url.Values{"name": {city}, "count": {"5"}, "language": {language}, "format": {"json"}}
	key := "geocoding:" + language + ":" + normalizeName(city)
	results, err := remember(a.cache, key, geocodingTTL, func() ([]geocodingResult, error) {
		// Nothing found comes as an answer without "results".
		var answer struct {
			Results []geocodingResult `json:"results"`
		}
		err := a.client.getJSON(ctx, call{service: "the Open-Meteo geocoder", url: a.urls.Geocoding + "/v1/search?" + query.Encode()}, &answer)
		return answer.Results, err
	})
	if err != nil {
		return spot{}, false, err
	}
	best, found := bestPlace(results)
	return best, found, nil
}

// bestPlace prefers a town in Uzbekistan, then a town anywhere, and never
// an airport.
func bestPlace(results []geocodingResult) (spot, bool) {
	bestRank := 3
	var best geocodingResult
	for _, r := range results {
		rank := 2
		switch {
		case r.FeatureCode == "AIRP" || r.FeatureCode == "AIRF":
			continue
		case strings.HasPrefix(r.FeatureCode, "PPL") && r.CountryCode == "UZ":
			rank = 0
		case strings.HasPrefix(r.FeatureCode, "PPL"):
			rank = 1
		}
		if rank < bestRank {
			bestRank, best = rank, r
		}
	}
	if bestRank == 3 {
		return spot{}, false
	}
	name := best.Name
	if best.Country != "" {
		name += ", " + best.Country
	}
	return spot{name: name, lat: best.Latitude, lon: best.Longitude}, true
}

func hasCyrillic(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

var wmoWords = map[int]string{
	0: "ochiq", 1: "asosan ochiq", 2: "qisman bulutli", 3: "bulutli",
	45: "tuman", 48: "qirovli tuman",
	51: "mayda yomg'ir", 53: "mayda yomg'ir", 55: "kuchli mayda yomg'ir", 56: "muzlovchi yomg'ir", 57: "muzlovchi yomg'ir",
	61: "yengil yomg'ir", 63: "yomg'ir", 65: "kuchli yomg'ir", 66: "muzlovchi yomg'ir", 67: "muzlovchi yomg'ir",
	71: "yengil qor", 73: "qor", 75: "kuchli qor", 77: "qor donachalari",
	80: "jala", 81: "jala", 82: "kuchli jala", 85: "qorli jala", 86: "kuchli qorli jala",
	95: "momaqaldiroq", 96: "do'l bilan momaqaldiroq", 99: "do'l bilan momaqaldiroq",
}

var metWords = map[string]string{"clearsky": "ochiq", "fair": "asosan ochiq", "partlycloudy": "qisman bulutli", "cloudy": "bulutli", "fog": "tuman"}

// wmoConditions puts a WMO weather code into a few Uzbek words.
func wmoConditions(code int) string {
	if word, found := wmoWords[code]; found {
		return word
	}
	return "noma'lum"
}

// metConditions puts a MET Norway symbol such as "lightrainshowers_day"
// into Uzbek words.
func metConditions(symbol string) string {
	symbol, _, _ = strings.Cut(symbol, "_")
	strength := ""
	switch {
	case strings.HasPrefix(symbol, "light"):
		strength = "yengil "
	case strings.HasPrefix(symbol, "heavy"):
		strength = "kuchli "
	}
	switch {
	case strings.Contains(symbol, "thunder"):
		return "momaqaldiroq"
	case strings.Contains(symbol, "sleet"):
		return "qor aralash yomg'ir"
	case strings.Contains(symbol, "snow"):
		return strength + "qor"
	case strings.Contains(symbol, "rainshowers"):
		return strength + "jala"
	case strings.Contains(symbol, "rain"):
		return strength + "yomg'ir"
	}
	if word, found := metWords[symbol]; found {
		return word
	}
	return "noma'lum"
}

// aqiCategory names a US AQI band in Uzbek.
func aqiCategory(aqi int) string {
	switch {
	case aqi <= 50:
		return "yaxshi"
	case aqi <= 100:
		return "o'rtacha"
	case aqi <= 150:
		return "sezgir odamlar uchun zararli"
	case aqi <= 200:
		return "zararli"
	case aqi <= 300:
		return "juda zararli"
	default:
		return "xavfli"
	}
}

// truncate cuts text to at most limit characters, marking the cut.
func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}
