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

	"telegram-ai-bot/internal/tools"
)

const (
	prayerTTL     = 24 * time.Hour
	prayerTimeout = 6 * time.Second
	// aladhanAttempt bounds the first call: about one call in twelve hung
	// when the live API was tried, and a second call usually answers.
	aladhanAttempt = 2500 * time.Millisecond
	ramadanMonth   = 9
	// ramadanMargin moves saharlik earlier and iftar later, so that the
	// minute or so Aladhan can be off from the Muslim Board never ends a
	// fast late or breaks it early.
	ramadanMargin = time.Minute
	// muslimUzTune shifts Aladhan's times (Imsak, Fajr, Sunrise, Dhuhr,
	// Asr, Maghrib, Sunset, Isha, Midnight) to the Muslim Board's
	// published ones; see askAladhan.
	muslimUzTune = "0,-3,0,0,0,4,0,3,0"

	prayerSource = "Aladhan, approximate (within a few minutes); official times and Ramadan dates: muslim.uz"
)

// hijriMonths are the Islamic months as they are said in Uzbek.
var hijriMonths = [...]string{"", "Muharram", "Safar", "Rabiul avval", "Rabiul oxir", "Jumodul avval", "Jumodul oxir", "Rajab", "Sha'bon", "Ramazon", "Shavvol", "Zulqa'da", "Zulhijja"}

type prayerArgs struct {
	Region string `json:"region"`
	Date   string `json:"date"`
	Mode   string `json:"mode"`
}

type prayerDayOutput struct {
	Region string    `json:"region"`
	Date   string    `json:"date"`
	Hijri  hijriDate `json:"hijri"`
	Bomdod string    `json:"bomdod"`
	Quyosh string    `json:"quyosh"`
	Peshin string    `json:"peshin"`
	Asr    string    `json:"asr"`
	Shom   string    `json:"shom"`
	Xufton string    `json:"xufton"`
	Source string    `json:"source"`
}

type hijriDate struct {
	Day   int    `json:"day"`
	Month string `json:"month"`
	Year  int    `json:"year"`
}

type ramadanOutput struct {
	Region    string       `json:"region"`
	HijriYear int          `json:"hijri_year"`
	Days      []ramadanDay `json:"days"`
	Note      string       `json:"note"`
	Source    string       `json:"source"`
}

type ramadanDay struct {
	Date        string `json:"date"`
	SaharlikEnd string `json:"saharlik_end"`
	Iftar       string `json:"iftar"`
}

func (a *apis) prayerTool() tools.Tool {
	regions, _ := json.Marshal(regionKeys())
	return tools.Tool{
		Name:        "prayer_times",
		Description: "Namoz times for an Uzbek region centre on a day, or the Ramadan saharlik and iftar timetable. Times are approximate (within a few minutes); muslim.uz announces the official ones.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"region": {"type": "string", "enum": ` + string(regions) + `, "description": "Region centre; use the nearest one for other towns. Default toshkent."},
				"date": {"type": "string", "description": "YYYY-MM-DD; omit for today."},
				"mode": {"type": "string", "enum": ["day", "ramadan"], "description": "day: the day's prayers; ramadan: saharlik end and iftar for each day of the current or next Ramadan. Default day."}
			},
			"additionalProperties": false
		}`),
		Timeout: prayerTimeout,
		Run:     a.prayerTimes,
	}
}

func regionKeys() []string {
	var keys []string
	for _, p := range places {
		if p.region != "" {
			keys = append(keys, p.region)
		}
	}
	return keys
}

func (a *apis) prayerTimes(ctx context.Context, raw json.RawMessage) (any, error) {
	var args prayerArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	region, found := regionPlace(or(args.Region, "toshkent"))
	if !found {
		return nil, argumentError("region must be one of %s", strings.Join(regionKeys(), ", "))
	}
	day, err := a.parseDay(args.Date)
	if err != nil {
		return nil, err
	}
	switch args.Mode {
	case "", "day":
		return a.prayerDay(ctx, region, day)
	case "ramadan":
		return a.ramadan(ctx, region, day)
	default:
		return nil, argumentError("mode must be day or ramadan")
	}
}

// aladhanDay is one day of Aladhan's answer. Times can carry a " (+05)"
// suffix.
type aladhanDay struct {
	Timings map[string]string `json:"timings"`
	Date    struct {
		Gregorian struct {
			Date string `json:"date"`
		} `json:"gregorian"`
		Hijri struct {
			Day   string `json:"day"`
			Month struct {
				Number int `json:"number"`
			} `json:"month"`
			Year string `json:"year"`
		} `json:"hijri"`
	} `json:"date"`
}

// times returns the named times as HH:MM, in order. Anything else is an
// error rather than text passed on to the model.
func (d aladhanDay) times(names ...string) ([]string, error) {
	var times []string
	for _, name := range names {
		hhmm, _, _ := strings.Cut(d.Timings[name], " ")
		if _, err := time.Parse("15:04", hhmm); err != nil || len(hhmm) != len("15:04") {
			return nil, errors.New("Aladhan sent a time that could not be read")
		}
		times = append(times, hhmm)
	}
	return times, nil
}

// shift moves an HH:MM time that times has checked by d.
func shift(hhmm string, d time.Duration) string {
	clock, _ := time.Parse("15:04", hhmm)
	return clock.Add(d).Format("15:04")
}

// isoDate turns Aladhan's DD-MM-YYYY into YYYY-MM-DD.
func (d aladhanDay) isoDate() string {
	day, err := time.Parse("02-01-2006", d.Date.Gregorian.Date)
	if err != nil {
		return ""
	}
	return day.Format(time.DateOnly)
}

func (a *apis) prayerDay(ctx context.Context, region place, day time.Time) (prayerDayOutput, error) {
	date := day.Format(time.DateOnly)
	answer, err := remember(a.cache, "prayer:"+region.region+":"+date, prayerTTL, func() (aladhanDay, error) {
		var answer struct {
			Data aladhanDay `json:"data"`
		}
		err := a.askAladhan(ctx, "/v1/timings/"+day.Format("02-01-2006"), region, &answer)
		// Aladhan answers a date it cannot use with today's times.
		if err == nil && answer.Data.isoDate() != date {
			err = errors.New("Aladhan answered for another day")
		}
		if err == nil {
			_, err = answer.Data.times(prayerNames...)
		}
		return answer.Data, err
	})
	if err != nil {
		return prayerDayOutput{}, err
	}
	times, _ := answer.times(prayerNames...)
	return prayerDayOutput{
		Region: region.name,
		Date:   date,
		Hijri:  answer.hijri(),
		Bomdod: times[0],
		Quyosh: times[1],
		Peshin: times[2],
		Asr:    times[3],
		Shom:   times[4],
		Xufton: times[5],
		Source: prayerSource,
	}, nil
}

// prayerNames are Aladhan's names for bomdod, quyosh, peshin, asr, shom
// and xufton, in that order.
var prayerNames = []string{"Fajr", "Sunrise", "Dhuhr", "Asr", "Maghrib", "Isha"}

func (d aladhanDay) hijri() hijriDate {
	hijri := d.Date.Hijri
	day, _ := strconv.Atoi(hijri.Day)
	year, _ := strconv.Atoi(hijri.Year)
	month := ""
	if hijri.Month.Number >= 1 && hijri.Month.Number < len(hijriMonths) {
		month = hijriMonths[hijri.Month.Number]
	}
	return hijriDate{Day: day, Month: month, Year: year}
}

// ramadan gives the Ramadan that is under way or next after day.
func (a *apis) ramadan(ctx context.Context, region place, day time.Time) (ramadanOutput, error) {
	date := day.Format(time.DateOnly)
	year := hijriYear(day)
	days, err := a.ramadanDays(ctx, region, year)
	if err == nil && days[len(days)-1].Date < date {
		year++
		days, err = a.ramadanDays(ctx, region, year)
	}
	if err != nil {
		return ramadanOutput{}, err
	}
	return ramadanOutput{Region: region.name, HijriYear: year, Days: days, Note: ramadanNote, Source: prayerSource}, nil
}

const ramadanNote = "saharlik_end is a minute before bomdod and iftar a minute after shom, to be safe"

func (a *apis) ramadanDays(ctx context.Context, region place, year int) ([]ramadanDay, error) {
	key := fmt.Sprintf("ramadan:%s:%d", region.region, year)
	return remember(a.cache, key, prayerTTL, func() ([]ramadanDay, error) {
		var answer struct {
			Data []aladhanDay `json:"data"`
		}
		path := fmt.Sprintf("/v1/hijriCalendar/%d/%d", year, ramadanMonth)
		if err := a.askAladhan(ctx, path, region, &answer); err != nil {
			return nil, err
		}
		var days []ramadanDay
		for _, d := range answer.Data {
			if d.Date.Hijri.Month.Number != ramadanMonth || d.isoDate() == "" {
				continue
			}
			times, err := d.times("Fajr", "Maghrib")
			if err != nil {
				return nil, err
			}
			days = append(days, ramadanDay{Date: d.isoDate(), SaharlikEnd: shift(times[0], -ramadanMargin), Iftar: shift(times[1], ramadanMargin)})
		}
		if len(days) == 0 {
			return nil, errors.New("Aladhan sent no Ramadan timetable")
		}
		return days, nil
	})
}

// askAladhan calls Aladhan with the settings that match the Muslim Board
// of Uzbekistan (muslim.uz): 15° for Fajr and Isha, Hanafi Asr, and
// muslimUzTune. On 2026-10-16 in Tashkent that gave muslim.uz's times to
// the minute except Asr, a minute late. Method 3, which many sites use,
// is 12 to 14 minutes off. A first call that hangs is tried once more.
func (a *apis) askAladhan(ctx context.Context, path string, region place, into any) error {
	query := url.Values{
		"latitude":       {coordinate(region.lat)},
		"longitude":      {coordinate(region.lon)},
		"method":         {"99"},
		"methodSettings": {"15,null,15"},
		"school":         {"1"},
		"timezonestring": {"Asia/Tashkent"},
		"tune":           {muslimUzTune},
	}
	request := call{service: "Aladhan", url: a.urls.Aladhan + path + "?" + query.Encode()}
	first, cancel := context.WithTimeout(ctx, aladhanAttempt)
	err := a.client.getJSON(first, request, into)
	cancel()
	var timeout *timeoutError
	if err == nil || !errors.As(err, &timeout) || ctx.Err() != nil {
		return err
	}
	return a.client.getJSON(ctx, request, into)
}

// hijriYear estimates the Islamic year of day with the arithmetic
// calendar. It can be a day or two off Aladhan's, which is fine: the
// caller needs only the year and checks the dates it gets back.
func hijriYear(day time.Time) int {
	utc := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, time.UTC)
	julianDay := int(utc.Unix()/86400) + 2440588
	l := julianDay - 1948440 + 10632
	n := (l - 1) / 10631
	l = l - 10631*n + 354
	j := ((10985-l)/5316)*((50*l)/17719) + (l/5670)*((43*l)/15238)
	return 30*n + j - 30
}
