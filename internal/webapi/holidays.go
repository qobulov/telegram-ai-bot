package webapi

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/tools"
)

const (
	holidaysTTL    = 24 * time.Hour
	holidaysSource = "Calendarific"
	// homeCountry gets every entry: working Saturdays, Ramadan and other
	// observances. Other countries get only national holidays, because
	// the full list of a country with states (the US: 365 KB) is too
	// large to read.
	homeCountry    = "UZ"
	minHolidayYear = 2000
	maxHolidayYear = 2049
	seasonType     = "Season"
)

var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

type holidaysArgs struct {
	Country string `json:"country"`
	Year    int    `json:"year"`
}

type holiday struct {
	Date   string `json:"date"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	InDays int    `json:"in_days"`
}

func (a *apis) holidaysTool() tools.Tool {
	return tools.Tool{
		Name: "holidays",
		Description: "Public holidays and days off of a year: Navruz, Independence Day, both Eids, extra days off the government gives and Saturdays made working days. " +
			"in_days counts from today (negative is past), so use it for \"how many days until Navruz\" or \"when is the next day off\". " +
			"Uzbekistan by default; for another country only its national holidays. Names come in English: translate them.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"country": {"type": "string", "description": "ISO 3166 two-letter code, such as UZ, RU or TR. Default UZ."},
				"year": {"type": "integer", "description": "Default this year. Ask for next year when this year's holiday has passed."}
			},
			"additionalProperties": false
		}`),
		Run: a.holidays,
	}
}

func (a *apis) holidays(ctx context.Context, raw json.RawMessage) (any, error) {
	var args holidaysArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	today := a.today()
	country := cmp.Or(strings.ToUpper(strings.TrimSpace(args.Country)), homeCountry)
	if !countryCode.MatchString(country) {
		return nil, argumentError("country must be a two-letter code such as UZ, got %q", args.Country)
	}
	year := args.Year
	if year == 0 {
		year = today.Year()
	}
	if year < minHolidayYear || year > maxHolidayYear {
		return nil, argumentError("year must be %d to %d", minHolidayYear, maxHolidayYear)
	}
	list, err := a.fetchHolidays(ctx, country, year)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, argumentError("%s has no holidays for %s %d; check the country code", holidaysSource, country, year)
	}
	for i := range list {
		day, _ := time.ParseInLocation(time.DateOnly, list[i].Date, a.location)
		list[i].InDays = int(day.Sub(today).Round(24*time.Hour).Hours() / 24)
	}
	return map[string]any{"country": country, "year": year, "today": today.Format(time.DateOnly), "holidays": list, "source": holidaysSource}, nil
}

// fetchHolidays returns a copy of the cached year, because the caller
// writes in_days, which changes every day, into it.
func (a *apis) fetchHolidays(ctx context.Context, country string, year int) ([]holiday, error) {
	query := url.Values{"country": {country}, "year": {strconv.Itoa(year)}}
	if country != homeCountry {
		query.Set("type", "national")
	}
	link := a.urls.Calendarific + "/api/v2/holidays?" + query.Encode()
	list, err := remember(a.cache, "holidays:"+link, holidaysTTL, func() ([]holiday, error) {
		var answer struct {
			// Response is an empty array, not an object, for an unknown
			// country.
			Response json.RawMessage `json:"response"`
		}
		// The key goes in a header, so that it is in no URL a log shows.
		header := http.Header{"Authorization": {"Bearer " + a.calendarificKey}}
		if err := a.client.getJSON(ctx, call{service: holidaysSource, url: link, header: header}, &answer); err != nil {
			return nil, err
		}
		return parseHolidays(answer.Response)
	})
	return append([]holiday(nil), list...), err
}

// parseHolidays keeps the day, name and kind of each holiday, without
// equinoxes and solstices.
func parseHolidays(response json.RawMessage) ([]holiday, error) {
	if strings.HasPrefix(strings.TrimSpace(string(response)), "[") {
		return nil, nil
	}
	var found struct {
		Holidays []struct {
			Name string `json:"name"`
			Date struct {
				ISO string `json:"iso"`
			} `json:"date"`
			PrimaryType string `json:"primary_type"`
		} `json:"holidays"`
	}
	if err := json.Unmarshal(response, &found); err != nil {
		return nil, fmt.Errorf("%s sent an answer that could not be read", holidaysSource)
	}
	var list []holiday
	for _, h := range found.Holidays {
		if h.PrimaryType == seasonType {
			continue
		}
		// A clock change comes with a time; only the day is kept.
		date, _, _ := strings.Cut(h.Date.ISO, "T")
		list = append(list, holiday{Date: date, Name: h.Name, Type: h.PrimaryType})
	}
	return list, nil
}
