package webapi

import (
	"strings"
	"testing"
)

// uzHolidays is the start and end of Calendarific's answer for Uzbekistan
// in 2026, descriptions cut.
const uzHolidays = `{"meta":{"code":200},"response":{"holidays":[
	{"name":"Nowruz","date":{"iso":"2026-03-21"},"type":["National holiday"],"primary_type":"Public Holiday"},
	{"name":"September Equinox","date":{"iso":"2026-09-23T17:05:16+05:00"},"type":["Season"],"primary_type":"Season"},
	{"name":"Teachers Day","date":{"iso":"2026-10-01"},"type":["National holiday"],"primary_type":"Public Holiday"},
	{"name":"Constitution Day","date":{"iso":"2026-12-08"},"type":["National holiday"],"primary_type":"Public Holiday"},
	{"name":"Working Day","date":{"iso":"2026-12-12"},"type":["Weekend"],"primary_type":"Working Day on a Weekend"}
]}}`

func TestHolidaysCountsTheDaysToEachHoliday(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/calendarific/api/v2/holidays": ok(uzHolidays)})
	opts := u.options()
	opts.CalendarificKey = "KEY"

	got := result[struct {
		Country  string    `json:"country"`
		Year     int       `json:"year"`
		Today    string    `json:"today"`
		Holidays []holiday `json:"holidays"`
	}](t, run(t, opts, "holidays", `{}`))

	want := []holiday{
		{Date: "2026-03-21", Name: "Nowruz", Type: "Public Holiday", InDays: -209},
		{Date: "2026-10-01", Name: "Teachers Day", Type: "Public Holiday", InDays: -15},
		{Date: "2026-12-08", Name: "Constitution Day", Type: "Public Holiday", InDays: 53},
		{Date: "2026-12-12", Name: "Working Day", Type: "Working Day on a Weekend", InDays: 57},
	}
	if got.Country != "UZ" || got.Year != 2026 || got.Today != "2026-10-16" || len(got.Holidays) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got.Holidays[i] != want[i] {
			t.Errorf("holiday %d = %+v, want %+v", i, got.Holidays[i], want[i])
		}
	}
	r := u.requests("/calendarific/api/v2/holidays")[0]
	query := r.URL.Query()
	if r.Header.Get("Authorization") != "Bearer KEY" || strings.Contains(r.URL.String(), "KEY") || query.Get("country") != "UZ" || query.Get("year") != "2026" || query.Has("type") {
		t.Errorf("request %s, header %q", r.URL, r.Header.Get("Authorization"))
	}
}

func TestHolidaysOfAnotherCountryAreOnlyNational(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/calendarific/api/v2/holidays":            ok(uzHolidays),
		"/calendarific/api/v2/holidays?country=XX": ok(`{"meta":{"code":200},"response":[]}`),
	})
	opts := u.options()
	opts.CalendarificKey = "KEY"

	if env := run(t, opts, "holidays", `{"country":"tr","year":2027}`); !env.OK {
		t.Fatalf("env = %s", env.raw)
	}
	query := u.requests("/calendarific/api/v2/holidays")[0].URL.Query()
	if query.Get("country") != "TR" || query.Get("year") != "2027" || query.Get("type") != "national" {
		t.Errorf("query = %v", query)
	}
	wantError(t, run(t, opts, "holidays", `{"country":"XX"}`), "invalid_arguments", "country code")
	wantError(t, run(t, opts, "holidays", `{"country":"Uzbekistan"}`), "invalid_arguments", "two-letter")
	wantError(t, run(t, opts, "holidays", `{"year":1990}`), "invalid_arguments", "year")
}

func TestHolidaysNeedsItsKey(t *testing.T) {
	if strings.Contains(toolNames(Tools(newUpstream(t, nil).options())), "holidays") {
		t.Error("the holidays tool is there without a key")
	}
}
