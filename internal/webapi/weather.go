package webapi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"telegram-ai-bot/internal/tools"
	"telegram-ai-bot/internal/webapi/internal/placename"
)

const (
	weatherTTL     = 10 * time.Minute
	geocodingTTL   = 24 * time.Hour
	weatherTimeout = 6 * time.Second
	// geocodingTimeout leaves the forecast its 3 s and MET Norway a second
	// of the tool's 6, however many spellings of a name are asked.
	geocodingTimeout = 2 * time.Second
	// geocodingCount is how many answers the geocoder is asked for: with 5,
	// Kogon Shahri, its 8th, was missed.
	geocodingCount = "20"
	maxCityName    = 100
	maxPlaceName   = 80
	// metBodyLimit leaves room above MET Norway's usual 40 KB, so the
	// fallback does not break when they add a field.
	metBodyLimit = 128 << 10
	// tashkentZone is what Open-Meteo is asked for places in Uzbekistan.
	tashkentZone = "Asia/Tashkent"

	// The place tables and the Open-Meteo geocoder are GeoNames data.
	openMeteoSource = "Open-Meteo (CC BY 4.0); places: GeoNames (CC BY 4.0)"
	metSource       = "MET Norway (CC BY 4.0); places: GeoNames (CC BY 4.0)"
	airNote         = "model estimate (CAMS), not a station reading"
	// metTodayNote warns that MET Norway's timeseries starts at the
	// current hour, so the morning is missing from today's figures.
	metTodayNote = "today's min_c and max_c cover only the rest of the day"
)

type weatherArgs struct {
	City       string `json:"city"`
	Days       *int   `json:"days"`
	IncludeAir bool   `json:"include_air"`
}

type weatherOutput struct {
	Place string `json:"place"`
	// Timezone is set for places abroad: their sunrise and sunset are in
	// local time.
	Timezone string       `json:"timezone,omitempty"`
	Now      weatherNow   `json:"now"`
	Daily    []weatherDay `json:"daily"`
	Air      *airOutput   `json:"air,omitempty"`
	Note     string       `json:"note,omitempty"`
	Source   string       `json:"source"`
}

func (o *weatherOutput) addNote(note string) {
	if o.Note != "" {
		o.Note += "; "
	}
	o.Note += note
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
		Description: "Current weather and a forecast of up to 3 days for a city (best for Uzbekistan, any spelling), optionally with air quality. Temperatures in °C, conditions in Uzbek; sunrise and sunset are the place's local time.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"city": {"type": "string", "description": "City name in any spelling, e.g. Toshkent, Самарканд, Moscow."},
				"days": {"type": "integer", "minimum": 1, "maximum": 3, "description": "Days of forecast from today. Default 1."},
				"include_air": {"type": "boolean", "description": "Also give air quality (US AQI and PM2.5): set it whenever someone asks about the air, smog or dust (\"havo iflosmi\", \"havo sifati\", \"smog\", \"чанг\", \"загрязнение воздуха\"), even without asking about the weather."}
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
	// zone is the time zone of a place abroad. Nil means Tashkent time,
	// which all of Uzbekistan keeps.
	zone *time.Location
}

// coordinate keeps at most four decimals, which MET Norway requires and
// which is about 10 m, plenty for weather.
func coordinate(value float64) string {
	return strconv.FormatFloat(round(value, 4), 'f', -1, 64)
}

func (s spot) key() string { return coordinate(s.lat) + "," + coordinate(s.lon) }

// zoneName is the time zone to ask Open-Meteo for, so that sunrise,
// sunset and the days themselves are the place's own.
func (s spot) zoneName() string {
	if s.zone == nil {
		return tashkentZone
	}
	return s.zone.String()
}

func (s spot) location(home *time.Location) *time.Location {
	return cmp.Or(s.zone, home)
}

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
	if parsePlace(city).name == "" || len([]rune(city)) > maxCityName || days < 1 || days > 3 {
		return nil, argumentError("city must be a name of at most %d characters and days 1 to 3", maxCityName)
	}
	where, err := a.findSpot(ctx, city)
	if err != nil {
		return nil, err
	}

	// Air quality is optional, so it gets a deadline of its own: a stalled
	// answer must not cost the forecast.
	air := make(chan airAnswer, 1)
	if args.IncludeAir {
		go func() {
			airCtx, cancel := context.WithTimeout(ctx, primaryTimeout)
			defer cancel()
			air <- a.airQuality(airCtx, where)
		}()
	}
	out, err := a.forecast(ctx, where, days)
	if err != nil {
		return nil, err
	}
	if where.zone != nil {
		out.Timezone = where.zone.String()
	}
	if args.IncludeAir {
		answer := <-air
		out.Air = answer.air
		if answer.err != nil {
			out.addNote("air quality is not available right now")
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

// openMeteoForecast holds pointers so that a missing number is noticed
// instead of shown as 0 °C and "ochiq".
type openMeteoForecast struct {
	Current struct {
		Temperature *float64 `json:"temperature_2m"`
		FeelsLike   *float64 `json:"apparent_temperature"`
		WeatherCode *int     `json:"weather_code"`
		WindSpeed   *float64 `json:"wind_speed_10m"`
	} `json:"current"`
	Daily struct {
		Time       []string   `json:"time"`
		Max        []*float64 `json:"temperature_2m_max"`
		Min        []*float64 `json:"temperature_2m_min"`
		RainChance []*float64 `json:"precipitation_probability_max"`
		Sunrise    []string   `json:"sunrise"`
		Sunset     []string   `json:"sunset"`
	} `json:"daily"`
}

// complete reports whether the answer has every number the output shows.
func (f openMeteoForecast) complete(days int) bool {
	now := f.Current
	if now.Temperature == nil || now.FeelsLike == nil || now.WeatherCode == nil || now.WindSpeed == nil || len(f.Daily.Time) < days {
		return false
	}
	for i := range days {
		if at(f.Daily.Max, i) == nil || at(f.Daily.Min, i) == nil {
			return false
		}
	}
	return true
}

func (a *apis) openMeteo(ctx context.Context, where spot, days int) (weatherOutput, error) {
	query := url.Values{
		"latitude":      {coordinate(where.lat)},
		"longitude":     {coordinate(where.lon)},
		"current":       {"temperature_2m,apparent_temperature,weather_code,wind_speed_10m"},
		"daily":         {"temperature_2m_max,temperature_2m_min,precipitation_probability_max,sunrise,sunset"},
		"timezone":      {where.zoneName()},
		"forecast_days": {strconv.Itoa(days)},
	}
	key := fmt.Sprintf("open-meteo:%s:%d", where.key(), days)
	forecast, err := remember(a.cache, key, weatherTTL, func() (openMeteoForecast, error) {
		var forecast openMeteoForecast
		err := a.client.getJSON(ctx, call{service: "Open-Meteo", url: a.urls.OpenMeteo + "/v1/forecast?" + query.Encode()}, &forecast)
		if err == nil && !forecast.complete(days) {
			err = errors.New("Open-Meteo sent an incomplete forecast")
		}
		return forecast, openMeteoError(err)
	})
	if err != nil {
		return weatherOutput{}, err
	}
	return forecast.output(where.name, days), nil
}

// output reads a forecast that complete has checked.
func (f openMeteoForecast) output(place string, days int) weatherOutput {
	feelsLike := *f.Current.FeelsLike
	out := weatherOutput{
		Place: place,
		Now: weatherNow{
			TempC:      *f.Current.Temperature,
			FeelsLikeC: &feelsLike,
			Conditions: wmoConditions(*f.Current.WeatherCode),
			WindKmh:    *f.Current.WindSpeed,
		},
		Source: openMeteoSource,
	}
	daily := f.Daily
	for i := range days {
		day := weatherDay{
			Date:    daily.Time[i],
			MaxC:    *daily.Max[i],
			MinC:    *daily.Min[i],
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
		"timezone":  {where.zoneName()},
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
				AirTemperature *float64 `json:"air_temperature"`
				WindSpeed      *float64 `json:"wind_speed"`
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

// complete reports whether every point has the numbers the output shows.
func (f metForecast) complete() bool {
	points := f.Properties.Timeseries
	for _, p := range points {
		if p.Data.Instant.Details.AirTemperature == nil || p.Data.Instant.Details.WindSpeed == nil {
			return false
		}
	}
	return len(points) > 0
}

func (a *apis) metNorway(ctx context.Context, where spot, days int) (weatherOutput, error) {
	query := url.Values{"lat": {coordinate(where.lat)}, "lon": {coordinate(where.lon)}}
	forecast, err := remember(a.cache, "met:"+where.key(), weatherTTL, func() (metForecast, error) {
		var forecast metForecast
		err := a.client.getJSON(ctx, call{service: "MET Norway", url: a.urls.METNorway + "/weatherapi/locationforecast/2.0/compact?" + query.Encode(), limit: metBodyLimit}, &forecast)
		if err == nil && !forecast.complete() {
			err = errors.New("MET Norway sent an incomplete forecast")
		}
		return forecast, err
	})
	if err != nil {
		return weatherOutput{}, err
	}
	return a.metOutput(forecast.Properties.Timeseries, where, days), nil
}

// metOutput takes the latest point as "now" and the minimum and maximum
// of each of the place's days as the forecast. MET Norway has no sunrise
// or chance of rain, and no hours before now.
func (a *apis) metOutput(points []metPoint, where spot, days int) weatherOutput {
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
		Place: where.name,
		Now: weatherNow{
			TempC:      *details.AirTemperature,
			Conditions: metConditions(current.symbol()),
			WindKmh:    round(*details.WindSpeed*3.6, 1),
		},
		Note:   metTodayNote,
		Source: metSource,
	}
	location := where.location(a.location)
	local := now.In(location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	for i := range days {
		date := today.AddDate(0, 0, i).Format(time.DateOnly)
		day, found := metDay(points, date, location)
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
		temperature := *p.Data.Instant.Details.AirTemperature
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

// placeQuery is a member's place: a name and what may follow a comma, a
// region, district, state or country it is in.
type placeQuery struct {
	name   string
	within []string
}

func parsePlace(city string) placeQuery {
	parts := strings.Split(city, ",")
	q := placeQuery{name: strings.TrimSpace(parts[0])}
	for _, part := range parts[1:] {
		if part = strings.TrimSpace(part); part != "" {
			q.within = append(q.within, part)
		}
	}
	return q
}

// countryKeys fold the names in countries.
var countryKeys = func() map[string]bool {
	keys := map[string]bool{}
	for _, name := range countries {
		keys[placename.Key(name)] = true
	}
	return keys
}()

// isCountry reports whether name is a country's, written without a place
// word: "O'zbekiston tumani" is a district in Farg'ona.
func isCountry(name string) bool {
	kind, _ := placename.KindOf(name)
	return kind == placename.AnyKind && countryKeys[placename.Key(name)]
}

var uzbekistanKeys = map[string]bool{
	placename.Key("O'zbekiston"): true, placename.Key("Uzbekistan"): true,
	placename.Key("Ўзбекистон"): true, placename.Key("Узбекистан"): true,
}

func isUzbekistan(name string) bool {
	return uzbekistanKeys[placename.Key(name)]
}

// findSpot looks in the place tables first and asks the geocoder only
// when both miss. What follows a comma narrows the answer: "Yangibozor,
// Xorazm" is the Yangibozor in Khorezm, "Paris, Texas" the town in Texas,
// and "Rossiya, Moskva", country first, Moscow.
func (a *apis) findSpot(ctx context.Context, city string) (spot, error) {
	q := parsePlace(city)
	if isCountry(q.name) && len(q.within) > 0 {
		q = placeQuery{name: q.within[0], within: slices.Concat(q.within[1:], []string{q.name})}
	}
	if isCountry(q.name) {
		return spot{}, argumentError("%q is a country, not a town; ask which town or city", q.name)
	}
	if len(q.within) > 0 {
		if where, found, err := a.findWithin(ctx, q); found || err != nil {
			return where, err
		}
	}
	if p, found := lookupPlace(q.name); found {
		return p.spot()
	}
	results, err := a.geocodeName(ctx, q.name)
	if capital, country := countryAnswer(q.name, results); country {
		if capital.Name == "" {
			return spot{}, argumentError("%q is a country, not a town; ask which town or city", q.name)
		}
		return capital.spot()
	}
	if match, found := matchingPlace(q.name, results); found {
		return match.spot()
	}
	if err != nil {
		return spot{}, err
	}
	return spot{}, argumentError("no place called %q was found; ask for the nearest town or region centre", q.name)
}

// findWithin answers a name with what followed its comma. An Uzbek
// region, district or town there, or Uzbekistan itself, picks the Uzbek
// place of the name in that region ("Farg'ona viloyati, Qo'qon" is read
// either way round). Anything else must be the country, state or province
// of one of the geocoder's answers. found is false when the words after
// the comma name nothing ("New York, NY"), and the name is then looked up
// alone.
func (a *apis) findWithin(ctx context.Context, q placeQuery) (where spot, found bool, err error) {
	for _, qualifier := range q.within {
		if isUzbekistan(qualifier) {
			return a.findInUzbekistan(ctx, q.name, nil)
		}
		if row, inTable := bestUzbekPlace(uzbekCandidates(qualifier), kindOfName(qualifier), placename.Key(qualifier)); inTable {
			name, around := q.name, uzbekPlaces[row]
			if head, ok := bestUzbekPlace(uzbekCandidates(name), kindOfName(name), placename.Key(name)); ok && readBackwards(uzbekPlaces[head], around) {
				name, around = qualifier, uzbekPlaces[head]
			}
			return a.findInUzbekistan(ctx, name, &around)
		}
	}
	results, err := a.geocodeName(ctx, q.name)
	var inside []geocodingResult
	for _, r := range results {
		if slices.ContainsFunc(q.within, r.isIn) {
			inside = append(inside, r)
		}
	}
	if match, matched := matchingPlace(q.name, inside); matched {
		where, err := match.spot()
		return where, true, err
	}
	return spot{}, err != nil, err
}

func kindOfName(name string) placename.Kind {
	kind, _ := placename.KindOf(name)
	return kind
}

// readBackwards reports whether "region, town" was written region first:
// the name is a region and what follows is a place inside it.
func readBackwards(head, qualifier uzbekPlace) bool {
	return head.kinds&regionKind != 0 && qualifier.kinds&regionKind == 0 && sameProvince(head, qualifier)
}

// findInUzbekistan finds name among Uzbek places, in around's region when
// around is set: in the table, else among the geocoder's Uzbek answers.
func (a *apis) findInUzbekistan(ctx context.Context, name string, around *uzbekPlace) (spot, bool, error) {
	var rows []int
	for _, row := range uzbekCandidates(name) {
		if around == nil || sameProvince(uzbekPlaces[row], *around) {
			rows = append(rows, row)
		}
	}
	if row, found := bestUzbekPlace(rows, kindOfName(name), placename.Key(name)); found {
		where, err := uzbekPlaces[row].place().spot()
		return where, true, err
	}
	results, err := a.geocodeName(ctx, name)
	var inside []geocodingResult
	for _, r := range results {
		if r.CountryCode == "UZ" && (around == nil || r.inProvince(around.province)) {
			inside = append(inside, r)
		}
	}
	if match, found := matchingPlace(name, inside); found {
		where, err := match.spot()
		return where, true, err
	}
	if err != nil {
		return spot{}, true, err
	}
	where := "Uzbekistan"
	if around != nil {
		where = around.name
	}
	return spot{}, true, argumentError("no place called %q was found in %s; ask for the nearest town or region centre", name, where)
}

// isIn reports whether the words after a comma name r's country, state or
// province, in any language the geocoder answered in, or its country code.
func (r geocodingResult) isIn(qualifier string) bool {
	key := placename.Key(qualifier)
	if key == "" {
		return false
	}
	for _, field := range []string{r.Country, r.Admin1, r.Admin2} {
		if placename.Key(field) == key {
			return true
		}
	}
	return strings.EqualFold(strings.TrimSpace(qualifier), r.CountryCode)
}

// provinceKeys fold the names of each Uzbek region, as the geocoder names
// a result's admin1 ("Sirdaryo viloyati", "Fergana").
var provinceKeys = func() map[int]map[string]bool {
	keys := map[int]map[string]bool{}
	for _, p := range uzbekPlaces {
		if p.kinds&regionKind == 0 {
			continue
		}
		if keys[p.province] == nil {
			keys[p.province] = map[string]bool{}
		}
		for _, name := range append([]string{p.name}, strings.Split(p.aliases, "|")...) {
			keys[p.province][placename.Key(name)] = true
		}
	}
	return keys
}()

// inProvince reports whether r, an Uzbek answer, lies in the region with
// GeoNames' code province; Tashkent and its region count as one.
func (r geocodingResult) inProvince(province int) bool {
	key := placename.Key(r.Admin1)
	if province == tashkentRegion || province == tashkentCity {
		return provinceKeys[tashkentCity][key] || provinceKeys[tashkentRegion][key]
	}
	return provinceKeys[province][key]
}

func (p place) spot() (spot, error) {
	s := spot{name: p.name, lat: p.lat, lon: p.lon}
	if p.timezone == "" {
		return s, nil
	}
	zone, err := time.LoadLocation(p.timezone)
	if err != nil {
		return spot{}, fmt.Errorf("the time zone of %s is not known on this server", p.name)
	}
	s.zone = zone
	return s, nil
}

type geocodingResult struct {
	// ID is the GeoNames id, the same in every language's answer.
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	FeatureCode string  `json:"feature_code"`
	CountryCode string  `json:"country_code"`
	Country     string  `json:"country"`
	Admin1      string  `json:"admin1"`
	Admin2      string  `json:"admin2"`
	Population  int64   `json:"population"`
	Timezone    string  `json:"timezone"`
}

// spot names the place for the model. The name and country are written by
// GeoNames contributors, so they are cleaned like any outside text.
func (r geocodingResult) spot() (spot, error) {
	name := cleanText(r.Name, maxPlaceName)
	if country := cleanText(r.Country, maxPlaceName); country != "" {
		name += ", " + country
	}
	s := spot{name: name, lat: r.Latitude, lon: r.Longitude}
	if r.CountryCode == "UZ" {
		return s, nil
	}
	zone, err := time.LoadLocation(r.Timezone)
	if err != nil {
		return spot{}, errors.New("the Open-Meteo geocoder sent a time zone that could not be read")
	}
	s.zone = zone
	return s, nil
}

// geocoderQuery is one question to the geocoder.
type geocoderQuery struct{ name, language string }

// geocodeName asks the geocoder about name, its place words dropped, in
// the 2 s that leave the forecast its time.
func (a *apis) geocodeName(ctx context.Context, name string) ([]geocodingResult, error) {
	geocoding, cancel := context.WithTimeout(ctx, geocodingTimeout)
	defer cancel()
	return a.geocode(geocoding, geocoderQueries(cmp.Or(placename.Trim(name), name)))
}

// geocoderQueries say what to ask the geocoder about city. A Cyrillic name
// is asked in Russian, and in Uzbek and English in Latin letters (Дамашқ
// is Damashq, Токмок Tokmok). A Latin name is asked in Uzbek and English,
// and in Russian in Cyrillic letters, as the Russian names of cities
// abroad are the ones its Latin spellings follow (Venetsiya is Венеция,
// Xyuston Хьюстон). GeoNames writes the apostrophe of o‘ and g‘ as U+2018
// (Qo‘ng‘irot), as U+2019 (Bulung’ur) or not at all, and finds nothing
// for the other ways, so a Latin name with an apostrophe is also asked in
// Uzbek in each of those spellings.
func geocoderQueries(city string) []geocoderQuery {
	if hasCyrillic(city) {
		latin := placename.Latin(city)
		return []geocoderQuery{{city, "ru"}, {latin, "uz"}, {latin, "en"}}
	}
	queries := []geocoderQuery{{city, "uz"}, {city, "en"}, {placename.Cyrillic(city), "ru"}}
	spellings := []string{city}
	// -1 drops the apostrophe.
	for _, mark := range []rune{'\u2018', '\u2019', -1} {
		spelling := strings.Map(func(r rune) rune {
			if isApostrophe(r) {
				return mark
			}
			return r
		}, city)
		if !slices.Contains(spellings, spelling) {
			spellings = append(spellings, spelling)
			queries = append(queries, geocoderQuery{spelling, "uz"})
		}
	}
	return queries
}

// geocode asks every query at once and merges the answers in the queries'
// order. The error is the first one, if any.
func (a *apis) geocode(ctx context.Context, queries []geocoderQuery) ([]geocodingResult, error) {
	answers := make([][]geocodingResult, len(queries))
	errs := make([]error, len(queries))
	var wg sync.WaitGroup
	for i, query := range queries {
		wg.Go(func() { answers[i], errs[i] = a.geocodeIn(ctx, query.name, query.language) })
	}
	wg.Wait()
	return slices.Concat(answers...), cmp.Or(errs...)
}

func (a *apis) geocodeIn(ctx context.Context, city, language string) ([]geocodingResult, error) {
	query := url.Values{"name": {city}, "count": {geocodingCount}, "language": {language}, "format": {"json"}}
	// The key is the name as sent. normalizeName would drop words such as
	// "city", and "Mexico City" is not "Mexico".
	key := "geocoding:" + language + ":" + strings.ToLower(strings.Join(strings.Fields(city), " "))
	return remember(a.cache, key, geocodingTTL, func() ([]geocodingResult, error) {
		// Nothing found comes as an answer without "results".
		var answer struct {
			Results []geocodingResult `json:"results"`
		}
		err := a.client.getJSON(ctx, call{service: "the Open-Meteo geocoder", url: a.urls.Geocoding + "/v1/search?" + query.Encode()}, &answer)
		return answer.Results, err
	})
}

// cityPopulation is the size GeoNames calls a city (its cities15000).
const cityPopulation = 15000

// matchingPlace picks the geocoder's town named as asked: it also answers
// lookalikes, such as Farishonki in Russia for Forish. A result must be
// named as the query is written (placename.Strict), or have it as one
// word of its name (Ansan for Ansan-si, Riyad for Эр-Рияд), or have a
// Russian name spelled as the query reads in Russian letters (Venetsiya
// for Венеция). In Uzbekistan only, it may also be named as the query
// folds (placename.Key): members drop apostrophes, and the extra
// spellings asked for them (Boston for Bo'ston) must not land abroad.
//
// Uzbekistan comes first, but a city abroad of cityPopulation or more is
// its equal: the Uzbek table already holds every Uzbek place of 5,000
// people or more, so an Uzbek answer here is a village, and Kirov is the
// Russian city before the Kirov in Andijon. Then a name as written before
// a folded one, then the larger, then the geocoder's order. Airports,
// abandoned places and results without a country or time zone, such as
// the parish in Montserrat that "Sankt-Peterburg" finds, are never
// picked.
func matchingPlace(query string, results []geocodingResult) (geocodingResult, bool) {
	type match struct {
		geocodingResult
		written bool
	}
	var matches []match
	for _, r := range results {
		strict, loose := namedAs(query, r)
		if (strict || loose) && isTown(r.FeatureCode) && r.CountryCode != "" && r.Country != "" && r.Timezone != "" {
			matches = append(matches, match{r, strict})
		}
	}
	if len(matches) == 0 {
		return geocodingResult{}, false
	}
	best := slices.MinFunc(matches, func(x, y match) int {
		return cmp.Or(
			trueFirst(x.preferred(), y.preferred()),
			trueFirst(x.written, y.written),
			cmp.Compare(y.Population, x.Population),
		)
	})
	// The place is named in the language asked first: Kyoto, Yaponiya
	// rather than the Russian answer's Киото, Япония that matched "Kioto".
	for _, r := range results {
		if r.ID != 0 && r.ID == best.ID && r.Country != "" {
			return r, true
		}
	}
	return best.geocodingResult, true
}

func (r geocodingResult) preferred() bool {
	return r.CountryCode == "UZ" || r.Population >= cityPopulation
}

// namedAs reports whether r is named as query is written (strict) or, in
// Uzbekistan, as it folds (loose); matchingPlace says how.
func namedAs(query string, r geocodingResult) (strict, loose bool) {
	asked := placename.Strict(query)
	if asked == "" {
		return false, false
	}
	readings := []string{asked}
	if hasCyrillic(r.Name) {
		readings = append(readings, placename.Strict(placename.Cyrillic(query)))
	}
	name, words := placename.Strict(r.Name), placename.Words(r.Name)
	oneWord := len(placename.Words(query)) == 1
	for _, reading := range readings {
		if name == reading || (oneWord && len(words) > 1 && slices.Contains(words, reading)) {
			return true, false
		}
	}
	return false, r.CountryCode == "UZ" && placename.Key(r.Name) == placename.Key(query)
}

// countryAnswer reports whether the geocoder takes query, written without
// a place word, for a country: Turkiya is Turkey, not a village in Nepal.
// A capital named like its country is the answer then (Kuwait, Mexico for
// Mexico City, Braziliya for Brasília).
func countryAnswer(query string, results []geocodingResult) (capital geocodingResult, country bool) {
	if kind, _ := placename.KindOf(query); kind != placename.AnyKind {
		return geocodingResult{}, false
	}
	for _, r := range results {
		if strict, _ := namedAs(query, r); strict && strings.HasPrefix(r.FeatureCode, "PCL") && r.FeatureCode != "PCLH" {
			country = true
			for _, c := range results {
				if named, _ := namedAs(query, c); named && c.FeatureCode == "PPLC" && c.CountryCode == r.CountryCode && c.Timezone != "" && c.Country != "" {
					return c, true
				}
			}
		}
	}
	return geocodingResult{}, country
}

// isTown is true for GeoNames' populated places (PPL, PPLA, PPLC...) that
// still exist: PPLH is historical, PPLQ abandoned and PPLW destroyed.
func isTown(featureCode string) bool {
	return strings.HasPrefix(featureCode, "PPL") && featureCode != "PPLH" && featureCode != "PPLQ" && featureCode != "PPLW"
}

// trueFirst orders true before false.
func trueFirst(x, y bool) int {
	switch {
	case x == y:
		return 0
	case x:
		return -1
	default:
		return 1
	}
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
