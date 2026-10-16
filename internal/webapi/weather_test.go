package webapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

type weatherResult struct {
	Place    string `json:"place"`
	Timezone string `json:"timezone"`
	Now      struct {
		TempC      float64  `json:"temp_c"`
		FeelsLikeC *float64 `json:"feels_like_c"`
		Conditions string   `json:"conditions"`
		WindKmh    float64  `json:"wind_kmh"`
	} `json:"now"`
	Daily []struct {
		Date          string  `json:"date"`
		MinC          float64 `json:"min_c"`
		MaxC          float64 `json:"max_c"`
		RainChancePct *int    `json:"rain_chance_pct"`
		Sunrise       string  `json:"sunrise"`
		Sunset        string  `json:"sunset"`
	} `json:"daily"`
	Air *struct {
		USAQI    int     `json:"us_aqi"`
		Category string  `json:"category"`
		PM25     float64 `json:"pm2_5"`
		Note     string  `json:"note"`
	} `json:"air"`
	Note   string `json:"note"`
	Source string `json:"source"`
}

const (
	forecastPath  = "/open-meteo/v1/forecast"
	airPath       = "/air-quality/v1/air-quality"
	geocodingPath = "/geocoding/v1/search"
	metPath       = "/met/weatherapi/locationforecast/2.0/compact"
)

func weatherRoutes(t *testing.T) map[string]stub {
	return map[string]stub{
		forecastPath:  ok(fixture(t, "open_meteo_forecast.json")),
		airPath:       ok(fixture(t, "open_meteo_air.json")),
		metPath:       ok(fixture(t, "met_norway.json")),
		geocodingPath: ok(fixture(t, "geocoding_none.json")),
	}
}

func TestWeather(t *testing.T) {
	tests := []struct {
		name     string
		override map[string]stub
		args     string
		check    func(t *testing.T, r weatherResult, u *upstream)
	}{
		{
			name: "a known city needs no geocoder",
			args: `{"city":"Tashkent"}`,
			check: func(t *testing.T, r weatherResult, u *upstream) {
				if r.Place != "Toshkent" || r.Now.TempC != 21.9 || r.Now.FeelsLikeC == nil || *r.Now.FeelsLikeC != 20.8 || r.Now.Conditions != "ochiq" || r.Now.WindKmh != 3.6 {
					t.Errorf("result = %+v", r)
				}
				day := r.Daily[0]
				if len(r.Daily) != 1 || day.Date != "2026-10-16" || day.MinC != 11.6 || day.MaxC != 24.3 || day.RainChancePct == nil || *day.RainChancePct != 0 || day.Sunrise != "06:35" || day.Sunset != "17:41" {
					t.Errorf("daily = %+v", r.Daily)
				}
				if r.Air != nil || !strings.Contains(r.Source, "Open-Meteo") || !strings.Contains(r.Source, "places: GeoNames") {
					t.Errorf("result = %+v", r)
				}
				// GeoNames' point for Tashkent.
				query := u.requests(forecastPath)[0].URL.Query()
				if query.Get("latitude") != "41.2647" || query.Get("longitude") != "69.2163" || query.Get("forecast_days") != "1" || query.Get("timezone") != "Asia/Tashkent" {
					t.Errorf("forecast query = %v", query)
				}
				if len(u.requests(geocodingPath)) != 0 {
					t.Error("the geocoder was asked about a known city")
				}
			},
		},
		{
			name: "three days with air quality",
			args: `{"city":"Тошкент","days":3,"include_air":true}`,
			check: func(t *testing.T, r weatherResult, _ *upstream) {
				if len(r.Daily) != 3 || r.Daily[2].Date != "2026-10-18" || r.Daily[2].MaxC != 22 {
					t.Errorf("daily = %+v", r.Daily)
				}
				if r.Air == nil || r.Air.USAQI != 78 || r.Air.Category != "o'rtacha" || r.Air.PM25 != 9 || !strings.Contains(r.Air.Note, "estimate") {
					t.Errorf("air = %+v", r.Air)
				}
			},
		},
		{
			// Live, the Kirov in Andijon is one of the 20 answers, after the
			// Russian city of 507,155 people.
			name: "a Cyrillic name is asked in Russian, and in Uzbek and English in Latin letters",
			override: map[string]stub{
				geocodingPath + "?name=Киров": ok(fixture(t, "geocoding_kirov_ru.json")),
				geocodingPath + "?name=Kirov": ok(fixture(t, "geocoding_kirov_uz.json")),
			},
			args: `{"city":"Киров"}`,
			check: func(t *testing.T, r weatherResult, u *upstream) {
				if r.Place != "Киров, Россия" || r.Timezone != "Europe/Kirov" {
					t.Errorf("place = %q, timezone %q", r.Place, r.Timezone)
				}
				if query := u.requests(forecastPath)[0].URL.Query(); query.Get("latitude") != "58.5981" || query.Get("timezone") != "Europe/Kirov" {
					t.Errorf("forecast query = %v", query)
				}
				wantAsked(t, u, "Киров in ru", "Kirov in uz", "Kirov in en")
			},
		},
		{
			// A city abroad of 15,000 people or more is an Uzbek village's
			// equal: the Uzbek table already has every place of 5,000.
			name: "a Latin name is asked in Uzbek, English and, in Cyrillic letters, Russian",
			override: map[string]stub{
				geocodingPath + "?name=Kirov": ok(fixture(t, "geocoding_kirov_uz.json")),
				geocodingPath + "?name=киров": ok(fixture(t, "geocoding_kirov_ru.json")),
			},
			args: `{"city":"Kirov"}`,
			check: func(t *testing.T, r weatherResult, u *upstream) {
				if r.Place != "Kirov, Rossiya" || r.Timezone != "Europe/Kirov" {
					t.Errorf("place = %q, timezone %q", r.Place, r.Timezone)
				}
				for _, request := range u.requests(geocodingPath) {
					if count := request.URL.Query().Get("count"); count != "20" {
						t.Errorf("count = %s, want 20", count)
					}
				}
				wantAsked(t, u, "Kirov in uz", "Kirov in en", "киров in ru")
			},
		},
		{
			// Live, "Odessa" in Uzbek and English is Odesa first and Odessa,
			// Texas, named as asked; in Russian both are Одесса.
			name: "a city is found by its Russian name and named in Uzbek",
			override: map[string]stub{
				geocodingPath + "?name=Odessa": ok(fixture(t, "geocoding_odessa_uz.json")),
				geocodingPath + "?name=одесса": ok(fixture(t, "geocoding_odessa_ru.json")),
			},
			args: `{"city":"Odessa"}`,
			check: func(t *testing.T, r weatherResult, _ *upstream) {
				if r.Place != "Odesa, Ukraina" || r.Timezone != "Europe/Kyiv" {
					t.Errorf("place = %q, timezone %q", r.Place, r.Timezone)
				}
			},
		},
		{
			name: "an Uzbek Cyrillic name abroad is asked in Uzbek in Latin letters",
			override: map[string]stub{
				geocodingPath + "?name=Damashq": ok(fixture(t, "geocoding_damashq_uz.json")),
			},
			args: `{"city":"Дамашқ"}`,
			check: func(t *testing.T, r weatherResult, _ *upstream) {
				if r.Place != "Damashq, Suriya" || r.Timezone != "Asia/Damascus" {
					t.Errorf("place = %q, timezone %q", r.Place, r.Timezone)
				}
			},
		},
		{
			name: "a place word is not asked of the geocoder",
			override: map[string]stub{
				geocodingPath + "?name=Bo‘ritepa": ok(fixture(t, "geocoding_boritepa_quoted_uz.json")),
			},
			args: `{"city":"Bo'ritepa qishlog'i"}`,
			check: func(t *testing.T, r weatherResult, u *upstream) {
				if r.Place != "Bo‘ritepa, O‘zbekiston" {
					t.Errorf("place = %q", r.Place)
				}
				for _, request := range u.requests(geocodingPath) {
					if name := request.URL.Query().Get("name"); strings.Contains(name, "qishlog") {
						t.Errorf("the geocoder was asked %q", name)
					}
				}
			},
		},
		{
			name: "what follows a comma picks the town abroad",
			override: map[string]stub{
				geocodingPath + "?name=Paris": ok(fixture(t, "geocoding_paris_en.json")),
				geocodingPath + "?name=парис": ok(fixture(t, "geocoding_paris_ru.json")),
			},
			args: `{"city":"Paris, Texas"}`,
			check: func(t *testing.T, r weatherResult, u *upstream) {
				if r.Place != "Paris, United States" || r.Timezone != "America/Chicago" {
					t.Errorf("place = %q, timezone %q", r.Place, r.Timezone)
				}
				if query := u.requests(forecastPath)[0].URL.Query(); query.Get("latitude") != "33.6609" {
					t.Errorf("forecast query = %v", query)
				}
			},
		},
		{
			name: "a country after the comma beats the Uzbek town of the name",
			override: map[string]stub{
				geocodingPath + "?name=Olot": ok(fixture(t, "geocoding_olot_uz.json")),
				geocodingPath + "?name=олот": ok(fixture(t, "geocoding_olot_ru.json")),
			},
			args: `{"city":"Olot, Ispaniya"}`,
			check: func(t *testing.T, r weatherResult, _ *upstream) {
				if r.Place != "Olot, Ispaniya" || r.Timezone != "Europe/Madrid" {
					t.Errorf("place = %q, timezone %q", r.Place, r.Timezone)
				}
			},
		},
		{
			name: "an Uzbek region after the comma picks among Uzbek namesakes",
			args: `{"city":"Yangibozor, Xorazm"}`,
			check: func(t *testing.T, r weatherResult, u *upstream) {
				if query := u.requests(forecastPath)[0].URL.Query(); r.Place != "Yangibozor" || query.Get("latitude") != "41.7116" || len(u.requests(geocodingPath)) != 0 {
					t.Errorf("place = %q, forecast query = %v", r.Place, query)
				}
			},
		},
		{
			name: "a region written first",
			args: `{"city":"Farg'ona viloyati, Qo'qon"}`,
			check: func(t *testing.T, r weatherResult, _ *upstream) {
				if r.Place != "Qo'qon" {
					t.Errorf("place = %q", r.Place)
				}
			},
		},
		{
			name: "a capital named like its country",
			override: map[string]stub{
				geocodingPath + "?name=Mexico": ok(fixture(t, "geocoding_mexico_en.json")),
				geocodingPath + "?name=мехико": ok(fixture(t, "geocoding_mexico_ru.json")),
			},
			args: `{"city":"Mexico"}`,
			check: func(t *testing.T, r weatherResult, _ *upstream) {
				if r.Place != "Mexico City, Mexico" || r.Timezone != "America/Mexico_City" {
					t.Errorf("place = %q, timezone %q", r.Place, r.Timezone)
				}
			},
		},
		{
			name: "a town abroad named as asked, in its own time zone",
			override: map[string]stub{
				geocodingPath + "?language=uz": ok(fixture(t, "geocoding_bursa_uz.json")),
				geocodingPath + "?language=en": ok(fixture(t, "geocoding_bursa_en.json")),
			},
			args: `{"city":"Bursa, Turkey"}`,
			check: func(t *testing.T, r weatherResult, u *upstream) {
				if r.Place != "Bursa, Turkiya" || r.Timezone != "Europe/Istanbul" {
					t.Errorf("place = %q, timezone %q", r.Place, r.Timezone)
				}
				query := u.requests(forecastPath)[0].URL.Query()
				if query.Get("latitude") != "40.1956" || query.Get("timezone") != "Europe/Istanbul" {
					t.Errorf("forecast query = %v", query)
				}
				wantAsked(t, u, "Bursa in uz", "Bursa in en", "бурса in ru")
			},
		},
		{
			// Live, only "Bo‘ritepa" finds the village: GeoNames writes o‘
			// and g‘ with U+2018.
			name:     "a name with apostrophes is also asked as GeoNames writes it",
			override: map[string]stub{geocodingPath + "?name=Bo‘ritepa": ok(fixture(t, "geocoding_boritepa_quoted_uz.json"))},
			args:     `{"city":"Bo'ritepa"}`,
			check: func(t *testing.T, r weatherResult, u *upstream) {
				if r.Place != "Bo‘ritepa, O‘zbekiston" || r.Timezone != "" {
					t.Errorf("place = %q, timezone %q", r.Place, r.Timezone)
				}
				if query := u.requests(forecastPath)[0].URL.Query(); query.Get("latitude") != "40.9604" || query.Get("timezone") != "Asia/Tashkent" {
					t.Errorf("forecast query = %v", query)
				}
				wantAsked(t, u, "Bo'ritepa in uz", "Bo'ritepa in en", "буритепа in ru", "Bo‘ritepa in uz", "Bo’ritepa in uz", "Boritepa in uz")
			},
		},
		{
			name: "a city abroad from the table is forecast in its own time",
			args: `{"city":"Москва","include_air":true}`,
			check: func(t *testing.T, r weatherResult, u *upstream) {
				if r.Place != "Moskva" || r.Timezone != "Europe/Moscow" || len(u.requests(geocodingPath)) != 0 {
					t.Errorf("place = %q, timezone %q", r.Place, r.Timezone)
				}
				for _, path := range []string{forecastPath, airPath} {
					if query := u.requests(path)[0].URL.Query(); query.Get("timezone") != "Europe/Moscow" || query.Get("latitude") != "55.752" {
						t.Errorf("%s query = %v", path, query)
					}
				}
			},
		},
		{
			name:     "MET Norway answers when Open-Meteo is busy",
			override: map[string]stub{forecastPath: status(http.StatusTooManyRequests)},
			args:     `{"city":"Toshkent","days":2}`,
			check: func(t *testing.T, r weatherResult, u *upstream) {
				if !strings.Contains(r.Source, "MET Norway") || r.Now.TempC != 22.5 || r.Now.FeelsLikeC != nil || r.Now.Conditions != "ochiq" || r.Now.WindKmh != 6.1 {
					t.Errorf("result = %+v", r)
				}
				if len(r.Daily) != 2 || r.Daily[0].MinC != 15.6 || r.Daily[0].MaxC != 24.2 || r.Daily[1].Date != "2026-10-17" || r.Daily[1].MinC != 11.9 || r.Daily[0].Sunrise != "" {
					t.Errorf("daily = %+v", r.Daily)
				}
				// MET Norway starts at the current hour: the morning is
				// missing from today's minimum.
				if !strings.Contains(r.Note, "rest of the day") {
					t.Errorf("note = %q", r.Note)
				}
				request := u.requests(metPath)[0]
				if request.URL.Query().Get("lat") != "41.2647" || request.Header.Get("User-Agent") == "" {
					t.Errorf("MET request = %s %v", request.URL, request.Header)
				}
			},
		},
		{
			name:     "MET Norway's days abroad are the place's days",
			override: map[string]stub{forecastPath: status(http.StatusTooManyRequests)},
			args:     `{"city":"Moskva"}`,
			check: func(t *testing.T, r weatherResult, _ *upstream) {
				// Moscow's day runs to 21:00 UTC, two hours past Tashkent's,
				// and its cooler night hours lower the minimum.
				if len(r.Daily) != 1 || r.Daily[0].Date != "2026-10-16" || r.Daily[0].MinC != 13.9 || r.Timezone != "Europe/Moscow" {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name:     "a forecast with gaps is not shown as zeros",
			override: map[string]stub{forecastPath: ok(`{"current":null,"daily":{"time":["2026-10-16"]}}`)},
			args:     `{"city":"Toshkent"}`,
			check: func(t *testing.T, r weatherResult, _ *upstream) {
				if !strings.Contains(r.Source, "MET Norway") || r.Now.TempC != 22.5 {
					t.Errorf("result = %+v", r)
				}
			},
		},
		{
			name:     "a day without its minimum is a gap too",
			override: map[string]stub{forecastPath: ok(strings.Replace(fixture(t, "open_meteo_forecast.json"), `"temperature_2m_min":[11.6`, `"temperature_2m_min":[null`, 1))},
			args:     `{"city":"Toshkent"}`,
			check: func(t *testing.T, r weatherResult, _ *upstream) {
				if !strings.Contains(r.Source, "MET Norway") {
					t.Errorf("source = %q", r.Source)
				}
			},
		},
		{
			name:     "MET Norway answers when Open-Meteo is slow",
			override: map[string]stub{forecastPath: slow},
			args:     `{"city":"Toshkent"}`,
			check: func(t *testing.T, r weatherResult, _ *upstream) {
				if !strings.Contains(r.Source, "MET Norway") {
					t.Errorf("source = %q", r.Source)
				}
			},
		},
		{
			name:     "the forecast comes even when air quality fails",
			override: map[string]stub{airPath: status(http.StatusTooManyRequests)},
			args:     `{"city":"Toshkent","include_air":true}`,
			check: func(t *testing.T, r weatherResult, _ *upstream) {
				if r.Air != nil || !strings.Contains(r.Note, "air quality") || len(r.Daily) != 1 {
					t.Errorf("result = %+v", r)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := weatherRoutes(t)
			for path, answer := range test.override {
				routes[path] = answer
			}
			u := newUpstream(t, routes)
			env := run(t, impatient(u.options()), "get_weather", test.args)
			wantFits(t, env, registryLimit)
			test.check(t, result[weatherResult](t, env), u)
		})
	}
}

func TestWeatherFailures(t *testing.T) {
	tests := []struct {
		name     string
		override map[string]stub
		args     string
		code     string
		words    []string
	}{
		{name: "no such place", args: `{"city":"Qwzxqy"}`, code: "invalid_arguments", words: []string{"Qwzxqy", "nearest town"}},
		{
			// Live, the geocoder's only answer for "Forish" is Farishonki in
			// Russia.
			name:     "the geocoder knows only a lookalike",
			override: map[string]stub{geocodingPath: ok(fixture(t, "geocoding_forish_uz.json"))},
			args:     `{"city":"Forishkent"}`,
			code:     "invalid_arguments",
			words:    []string{"no place called", "nearest town or region centre"},
		},
		{name: "no city", args: `{}`, code: "invalid_arguments"},
		{name: "too many days", args: `{"city":"Toshkent","days":4}`, code: "invalid_arguments"},
		{name: "no days", args: `{"city":"Toshkent","days":0}`, code: "invalid_arguments"},
		{name: "a city name that is a paragraph", args: `{"city":"` + strings.Repeat("a", 101) + `"}`, code: "invalid_arguments"},
		{
			name:     "both sources refuse",
			override: map[string]stub{forecastPath: {status: http.StatusBadRequest, body: fixture(t, "open_meteo_400.json")}, metPath: status(http.StatusServiceUnavailable)},
			args:     `{"city":"Toshkent"}`,
			code:     "execution_failed",
			words:    []string{"Open-Meteo refused the request: Latitude must be in range", "MET Norway is busy"},
		},
		{
			name:     "both sources slow",
			override: map[string]stub{forecastPath: slow, metPath: slow},
			args:     `{"city":"Toshkent"}`,
			code:     "timeout",
		},
		{
			name:     "MET Norway's answer has gaps too",
			override: map[string]stub{forecastPath: status(http.StatusTooManyRequests), metPath: ok(`{"properties":{"timeseries":[{}]}}`)},
			args:     `{"city":"Toshkent"}`,
			code:     "execution_failed",
			words:    []string{"MET Norway sent an incomplete forecast"},
		},
		{
			name:     "the geocoder's only answer is a parish without a country",
			override: map[string]stub{geocodingPath + "?language=uz": ok(fixture(t, "geocoding_montserrat_uz.json"))},
			args:     `{"city":"Sankt-Peterburgga"}`,
			code:     "invalid_arguments",
			words:    []string{"no place called"},
		},
		{
			name:     "the geocoder is busy",
			override: map[string]stub{geocodingPath: status(http.StatusTooManyRequests)},
			args:     `{"city":"Bo'ritepa"}`,
			code:     "execution_failed",
			words:    []string{"busy"},
		},
		{
			// Turkiya is also a village in Nepal.
			name:  "a country is not a town",
			args:  `{"city":"Turkiya"}`,
			code:  "invalid_arguments",
			words: []string{`"Turkiya" is a country`, "which town"},
		},
		{
			// Live, the first answer for Peru is the country; towns in the
			// United States follow.
			name: "the geocoder takes the name for a country",
			override: map[string]stub{
				geocodingPath + "?name=Peru": ok(fixture(t, "geocoding_peru_en.json")),
				geocodingPath + "?name=перу": ok(fixture(t, "geocoding_peru_ru.json")),
			},
			args:  `{"city":"Peru"}`,
			code:  "invalid_arguments",
			words: []string{`"Peru" is a country`},
		},
		{
			name:  "an Uzbek town that is not in the region after the comma",
			args:  `{"city":"Kogon, Xorazm"}`,
			code:  "invalid_arguments",
			words: []string{`no place called "Kogon" was found in Xorazm viloyati`},
		},
		{
			name:  "nothing before the comma",
			args:  `{"city":", Texas"}`,
			code:  "invalid_arguments",
			words: []string{"city must be a name"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := weatherRoutes(t)
			for path, answer := range test.override {
				routes[path] = answer
			}
			wantError(t, run(t, impatient(newUpstream(t, routes).options()), "get_weather", test.args), test.code, test.words...)
		})
	}
}

func TestWeatherIsCachedPerPlace(t *testing.T) {
	u := newUpstream(t, weatherRoutes(t))
	registry := newRegistry(t, u.options())

	for _, city := range []string{"Toshkent", "Tashkent", "Samarqand"} {
		result[weatherResult](t, execute(t, registry, "get_weather", `{"city":"`+city+`"}`))
	}
	if n := len(u.requests(forecastPath)); n != 2 {
		t.Errorf("Open-Meteo was asked %d times, want once per place", n)
	}
}

func TestWeatherGeocoderKeepsMexicoCityApartFromMexico(t *testing.T) {
	routes := weatherRoutes(t)
	routes[geocodingPath] = ok(fixture(t, "geocoding_gazli.json"))
	u := newUpstream(t, routes)
	registry := newRegistry(t, u.options())

	for _, city := range []string{"Mexico", "Mexico City"} {
		execute(t, registry, "get_weather", `{"city":"`+city+`"}`)
	}
	asked := map[string]bool{}
	for _, request := range u.requests(geocodingPath) {
		asked[request.URL.Query().Get("name")] = true
	}
	if !asked["Mexico"] || !asked["Mexico City"] {
		t.Errorf("the geocoder was asked about %v; a cached answer for Mexico must not answer Mexico City", asked)
	}
}

func TestWeatherDoesNotWaitLongForAirQuality(t *testing.T) {
	t.Parallel()
	routes := weatherRoutes(t)
	routes[airPath] = slow
	u := newUpstream(t, routes)
	opts := u.options()
	// A caller's client without short timeouts.
	opts.HTTPClient = &http.Client{Timeout: 10 * time.Second}

	r := result[weatherResult](t, run(t, opts, "get_weather", `{"city":"Toshkent","include_air":true}`))

	if r.Air != nil || !strings.Contains(r.Note, "air quality") || len(r.Daily) != 1 || !strings.Contains(r.Source, "Open-Meteo") {
		t.Errorf("result = %+v", r)
	}
}

// More spellings mean more answers to wait for: one that stalls must not
// take the forecast's time.
func TestWeatherDoesNotWaitLongForTheGeocoder(t *testing.T) {
	t.Parallel()
	routes := weatherRoutes(t)
	routes[geocodingPath+"?name=Bo‘ritepa"] = ok(fixture(t, "geocoding_boritepa_quoted_uz.json"))
	routes[geocodingPath+"?name=Boritepa"] = slow
	u := newUpstream(t, routes)
	opts := u.options()
	// A caller's client without short timeouts.
	opts.HTTPClient = &http.Client{Timeout: 10 * time.Second}

	start := time.Now()
	r := result[weatherResult](t, run(t, opts, "get_weather", `{"city":"Bo'ritepa"}`))

	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("the answer took %v", elapsed)
	}
	if r.Place != "Bo‘ritepa, O‘zbekiston" || !strings.Contains(r.Source, "Open-Meteo") {
		t.Errorf("result = %+v", r)
	}
}

func TestMatchingPlace(t *testing.T) {
	answer := func(names ...string) []geocodingResult { return geocoderAnswer(t, names...) }
	// The live answer for "Boston" in Uzbek, which the tool asks for
	// Bo'ston without its apostrophe: Boston, Massachusetts, first and a
	// Bo‘ston in Andijon 19th.
	boston := answer("geocoding_boston_uz.json")
	var bostonAbroad []geocodingResult
	for _, r := range boston {
		if r.CountryCode != "UZ" {
			bostonAbroad = append(bostonAbroad, r)
		}
	}
	tests := []struct {
		name, query string
		results     []geocodingResult
		want        string
		country     string
		// latitude tells namesakes in one country apart; zero skips it.
		latitude float64
	}{
		{name: "Bo'ston is the Uzbek town, not Boston", query: "Bo'ston", results: boston, want: "Bo‘ston", country: "UZ", latitude: 40.83231},
		{name: "Bo'ston is never a town abroad", query: "Bo'ston", results: bostonAbroad},
		{name: "Boston as written is the city", query: "Boston", results: boston, want: "Boston", country: "US", latitude: 42.35843},
		{name: "a lookalike is not a match", query: "Forish", results: answer("geocoding_forish_uz.json")},
		// The 8th of 20 answers, after five Kogons in the Philippines.
		{name: "the Uzbek town far down the list", query: "Kogon", results: answer("geocoding_kogon_uz.json"), want: "Kogon Shahri", country: "UZ"},
		{name: "the city, not York, Nebraska", query: "New York", results: answer("geocoding_new_york_uz.json", "geocoding_new_york_en.json"), want: "New York", country: "US", latitude: 40.71427},
		{name: "Pop in Namangan, not Puerto Plata", query: "Pop", results: answer("geocoding_pop_uz.json"), want: "Pop", country: "UZ"},
		// GeoNames counts nobody in Keles, Tashkent region, and 3,772
		// people in Keles, Bursa.
		{name: "Uzbekistan before a larger namesake abroad", query: "Keles", results: answer("geocoding_keles_uz.json", "geocoding_keles_en.json"), want: "Keles", country: "UZ", latitude: 41.40333},
		{name: "a Cyrillic name", query: "Келес", results: answer("geocoding_keles_ru.json"), want: "Келес", country: "UZ"},
		{name: "the geocoder's order among uncounted towns", query: "Baxt", results: answer("geocoding_baxt_uz.json", "geocoding_baxt_en.json"), want: "Baxt", country: "UZ", latitude: 40.72},
		{name: "Danghara is another name", query: "Dangara", results: answer("geocoding_dangara_uz.json", "geocoding_dangara_en.json"), want: "Dangara", country: "UZ", latitude: 39.66667},
		// The Uzbek table has every Uzbek place of 5,000 people or more, so
		// an Uzbek answer here is a village, and a city abroad of 15,000 or
		// more is its equal: then the larger wins.
		{name: "a city abroad before a village of its name in Uzbekistan", query: "Chimkent", results: answer("geocoding_chimkent_uz.json"), want: "Chimkent", country: "KZ"},
		{name: "Kirov is the Russian city", query: "Kirov", results: answer("geocoding_kirov_uz.json", "geocoding_kirov_ru.json"), want: "Kirov", country: "RU", latitude: 58.59809},
		{name: "Aqtau is the Kazakh city", query: "Aqtau", results: answer("geocoding_aqtau_uz.json"), want: "Aqtau", country: "KZ"},
		// GeoNames names the city Odesa and the town in Texas Odessa; in
		// Russian both are Одесса, and the larger wins. The answer comes
		// as the first language asked names it.
		{name: "a city by its Russian name", query: "Odessa", results: answer("geocoding_odessa_uz.json", "geocoding_odessa_ru.json"), want: "Odesa", country: "UA"},
		{name: "a city by its Russian name, named in Uzbek", query: "Kioto", results: answer("geocoding_kioto_uz.json", "geocoding_kioto_ru.json"), want: "Kyoto", country: "JP"},
		{name: "a city by its Russian name alone", query: "Kioto", results: answer("geocoding_kioto_ru.json"), want: "Киото", country: "JP"},
		// One word of the name is enough: Эр-Рияд, not the Riyad villages
		// in Mauritania.
		{name: "a word of the name", query: "Riyad", results: answer("geocoding_riyad_ru.json"), want: "Эр-Рияд", country: "SA"},
		{name: "a parish without a country", query: "Sankt-Peterburg", results: answer("geocoding_montserrat_uz.json")},
		{name: "nothing at all", query: "Gazli", results: answer("geocoding_none.json")},
		{name: "a name that folds to nothing", query: "'", results: boston},
	}
	for _, test := range tests {
		got, found := matchingPlace(test.query, test.results)
		if found != (test.want != "") || got.Name != test.want || got.CountryCode != test.country || (test.latitude != 0 && got.Latitude != test.latitude) {
			t.Errorf("%s: matchingPlace(%q) = %s, %s, %v, %v", test.name, test.query, got.Name, got.CountryCode, got.Latitude, found)
		}
	}
}

func TestCountryAnswer(t *testing.T) {
	answer := func(names ...string) []geocodingResult { return geocoderAnswer(t, names...) }
	tests := []struct {
		query   string
		results []geocodingResult
		country bool
		capital string
	}{
		{"Peru", answer("geocoding_peru_en.json", "geocoding_peru_ru.json"), true, ""},
		// Xitoy is China in Uzbek, and five villages in Uzbekistan.
		{"Xitoy", answer("geocoding_xitoy_uz.json"), true, ""},
		{"Mexico", answer("geocoding_mexico_en.json", "geocoding_mexico_ru.json"), true, "Mexico City"},
		// A place word says a town is meant.
		{"Peru shahri", answer("geocoding_peru_en.json"), false, ""},
		{"Kirov", answer("geocoding_kirov_uz.json"), false, ""},
	}
	for _, test := range tests {
		capital, country := countryAnswer(test.query, test.results)
		if country != test.country || capital.Name != test.capital {
			t.Errorf("countryAnswer(%q) = %q, %v; want %q, %v", test.query, capital.Name, country, test.capital, test.country)
		}
	}
}

// geocoderAnswer is what the geocoder said in the named fixtures, one
// after the other, as geocode merges its answers.
func geocoderAnswer(t *testing.T, names ...string) []geocodingResult {
	t.Helper()
	var all []geocodingResult
	for _, name := range names {
		var decoded struct {
			Results []geocodingResult `json:"results"`
		}
		if err := json.Unmarshal([]byte(fixture(t, name)), &decoded); err != nil {
			t.Fatal(err)
		}
		all = append(all, decoded.Results...)
	}
	return all
}

// wantAsked checks the geocoder was asked exactly these questions, each
// written "name in language".
func wantAsked(t *testing.T, u *upstream, want ...string) {
	t.Helper()
	asked := map[string]bool{}
	for _, request := range u.requests(geocodingPath) {
		query := request.URL.Query()
		asked[query.Get("name")+" in "+query.Get("language")] = true
	}
	for _, question := range want {
		if !asked[question] {
			t.Errorf("the geocoder was not asked %q", question)
		}
	}
	if len(asked) != len(want) {
		t.Errorf("the geocoder was asked %v, want %v", asked, want)
	}
}

func TestWeatherWords(t *testing.T) {
	for code, want := range map[int]string{0: "ochiq", 3: "bulutli", 45: "tuman", 63: "yomg'ir", 75: "kuchli qor", 95: "momaqaldiroq", 1234: "noma'lum"} {
		if got := wmoConditions(code); got != want {
			t.Errorf("wmoConditions(%d) = %q, want %q", code, got, want)
		}
	}
	for symbol, want := range map[string]string{"clearsky_night": "ochiq", "partlycloudy_day": "qisman bulutli", "lightrainshowers_day": "yengil jala", "heavyrain": "kuchli yomg'ir", "snowandthunder": "momaqaldiroq", "sleet": "qor aralash yomg'ir", "fog": "tuman"} {
		if got := metConditions(symbol); got != want {
			t.Errorf("metConditions(%q) = %q, want %q", symbol, got, want)
		}
	}
	for aqi, want := range map[int]string{20: "yaxshi", 78: "o'rtacha", 120: "sezgir odamlar uchun zararli", 180: "zararli", 250: "juda zararli", 400: "xavfli"} {
		if got := aqiCategory(aqi); got != want {
			t.Errorf("aqiCategory(%d) = %q, want %q", aqi, got, want)
		}
	}
}
