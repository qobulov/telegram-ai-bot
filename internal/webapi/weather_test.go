package webapi

import (
	"net/http"
	"strings"
	"testing"
)

type weatherResult struct {
	Place string `json:"place"`
	Now   struct {
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
				if r.Air != nil || !strings.Contains(r.Source, "Open-Meteo") {
					t.Errorf("result = %+v", r)
				}
				query := u.requests(forecastPath)[0].URL.Query()
				if query.Get("latitude") != "41.2995" || query.Get("longitude") != "69.2401" || query.Get("forecast_days") != "1" || query.Get("timezone") != "Asia/Tashkent" {
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
			name:     "a Cyrillic name goes to the geocoder in Russian",
			override: map[string]stub{geocodingPath + "?language=ru": ok(fixture(t, "geocoding_urgut.json"))},
			args:     `{"city":"Ургут"}`,
			check: func(t *testing.T, r weatherResult, u *upstream) {
				if r.Place != "Ургут, Узбекистан" {
					t.Errorf("place = %q", r.Place)
				}
				if query := u.requests(forecastPath)[0].URL.Query(); query.Get("latitude") != "39.419" || query.Get("longitude") != "67.2612" {
					t.Errorf("forecast query = %v", query)
				}
			},
		},
		{
			name:     "a Latin name tries Uzbek, then English, and prefers Uzbekistan",
			override: map[string]stub{geocodingPath + "?language=en": ok(fixture(t, "geocoding_gazli.json"))},
			args:     `{"city":"Gazli"}`,
			check: func(t *testing.T, r weatherResult, u *upstream) {
				geocoder := u.requests(geocodingPath)
				if r.Place != "Gazli, Uzbekistan" || len(geocoder) != 2 || geocoder[0].URL.Query().Get("language") != "uz" {
					t.Errorf("place = %q after %d geocoder calls", r.Place, len(geocoder))
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
				request := u.requests(metPath)[0]
				if request.URL.Query().Get("lat") != "41.2995" || request.Header.Get("User-Agent") == "" {
					t.Errorf("MET request = %s %v", request.URL, request.Header)
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
		{name: "no such place", args: `{"city":"Qwzxqy"}`, code: "invalid_arguments", words: []string{"Qwzxqy"}},
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
			name:     "the geocoder is busy",
			override: map[string]stub{geocodingPath: status(http.StatusTooManyRequests)},
			args:     `{"city":"Gazli"}`,
			code:     "execution_failed",
			words:    []string{"busy"},
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
