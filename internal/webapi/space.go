package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"telegram-ai-bot/internal/tools"
)

// space answers questions about the sky, as the owners asked on 2026-11-27:
// what a star, planet, galaxy or nebula is, what happens in the sky in the
// coming days, when an object is up tonight and what is up now. The data
// is SpaceCatalog.org's, free and keyless but for 60 requests a minute, so
// answers are kept a while.

const (
	spaceService = "SpaceCatalog"
	spaceSource  = "SpaceCatalog.org"
	// spaceObjectTTL keeps an object's facts, which change with a data
	// release; spaceSkyTTL keeps what is up, rounded by the catalogue to
	// five minutes anyway.
	spaceObjectTTL = 12 * time.Hour
	spaceEventsTTL = 3 * time.Hour
	spaceSkyTTL    = 10 * time.Minute
	spaceBodyLimit = 2 << 20
	spaceMaxDays   = 60
	spaceShown     = 12
	spaceFact      = 400
	// tashkentLat and tashkentLon are where the sky is seen from unless the
	// asker names another place.
	tashkentLat = 41.31
	tashkentLon = 69.28
)

type spaceArgs struct {
	Action string   `json:"action"`
	Name   string   `json:"name"`
	Days   int      `json:"days"`
	Kind   string   `json:"kind"`
	Lat    *float64 `json:"lat"`
	Lon    *float64 `json:"lon"`
}

func (a *apis) spaceTool() tools.Tool {
	return tools.Tool{
		Name: "space",
		Description: "The sky, from SpaceCatalog.org. object: what a star, planet, moon, galaxy or nebula is (name). events: eclipses, meteor showers, " +
			"conjunctions, oppositions and Moon phases in the next days. tonight: when an object rises, is highest and sets. visible: what is up now. Places default to Tashkent.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["object", "events", "tonight", "visible"]},
				"name": {"type": "string", "description": "For object and tonight: the object, in English or a catalogue name, such as Jupiter, Andromeda, M42, Betelgeuse."},
				"days": {"type": "integer", "description": "For events: how many days ahead, 1 to 60; 30 when left out."},
				"kind": {"type": "string", "enum": ["eclipse", "shower", "conjunction", "opposition", "elongation", "moon", "season", "flyby"], "description": "For events: one kind only."},
				"lat": {"type": "number", "description": "For tonight and visible: the place's latitude, when not Tashkent."},
				"lon": {"type": "number", "description": "For tonight and visible: the place's longitude, when not Tashkent."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		Timeout: patientTimeout,
		Run:     a.space,
	}
}

func (a *apis) space(ctx context.Context, raw json.RawMessage) (any, error) {
	var args spaceArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	lat, lon := tashkentLat, tashkentLon
	if args.Lat != nil && args.Lon != nil && math.Abs(*args.Lat) <= 90 && math.Abs(*args.Lon) <= 180 {
		lat, lon = *args.Lat, *args.Lon
	}
	switch args.Action {
	case "object":
		return a.spaceObject(ctx, strings.TrimSpace(args.Name))
	case "events":
		return a.spaceEvents(ctx, args.Days, args.Kind)
	case "tonight":
		return a.spaceTonight(ctx, strings.TrimSpace(args.Name), lat, lon)
	case "visible":
		return a.spaceVisible(ctx, lat, lon)
	}
	return nil, argumentError("action must be object, events, tonight or visible")
}

func spaceGet[T any](ctx context.Context, a *apis, path string, ttl time.Duration) (T, error) {
	link := a.urls.SpaceCatalog + path
	return remember(a.cache, "space:"+link, ttl, func() (T, error) {
		var out T
		err := a.patient.getJSON(ctx, call{service: spaceService, url: link, limit: spaceBodyLimit}, &out)
		return out, err
	})
}

type spaceRecord struct {
	Slug            string   `json:"slug"`
	Name            string   `json:"name"`
	Category        string   `json:"category"`
	ObjectType      string   `json:"object_type"`
	Mag             *float64 `json:"mag"`
	DistanceLy      *float64 `json:"distance_ly"`
	DistanceMethod  string   `json:"distance_method"`
	Constellation   string   `json:"constellation"`
	Discovered      string   `json:"discovered"`
	MassMsun        *float64 `json:"mass_msun"`
	MassKg          *float64 `json:"mass_kg"`
	RadiusKm        *float64 `json:"radius_km"`
	Orbits          string   `json:"orbits"`
	SemiMajorAxisKm *float64 `json:"semi_major_axis_km"`
	Description     string   `json:"description"`
	URL             string   `json:"url"`
	Source          struct {
		Name    string `json:"name"`
		License string `json:"license"`
	} `json:"source"`
	Properties struct {
		DiscoveredBy string `json:"discovered_by"`
		HubbleType   string `json:"hubble_type"`
		Spectral     string `json:"spectral_type"`
	} `json:"properties"`
}

// spaceSlug is the catalogue's slug for a name: resolved as a designation
// first, then searched for by name.
func (a *apis) spaceSlug(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", argumentError("name the object")
	}
	resolved, err := spaceGet[struct {
		Object *spaceRecord `json:"object"`
	}](ctx, a, "/api/v1/resolve?id="+url.QueryEscape(name), spaceObjectTTL)
	if err == nil && resolved.Object != nil && resolved.Object.Slug != "" {
		return resolved.Object.Slug, nil
	}
	found, err := spaceGet[struct {
		Results []spaceRecord `json:"results"`
	}](ctx, a, "/api/v1/search?limit=1&q="+url.QueryEscape(name), spaceObjectTTL)
	if err != nil {
		return "", err
	}
	if len(found.Results) == 0 {
		return "", nil
	}
	return found.Results[0].Slug, nil
}

func (a *apis) spaceObject(ctx context.Context, name string) (any, error) {
	slug, err := a.spaceSlug(ctx, name)
	if err != nil {
		return nil, err
	}
	if slug == "" {
		return map[string]any{"name": name, "found": false, "note": "Not in the catalogue: say so; never make its facts up.", "source": spaceSource}, nil
	}
	o, err := spaceGet[spaceRecord](ctx, a, "/api/v1/objects/"+url.PathEscape(slug), spaceObjectTTL)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"name": o.Name, "type": o.ObjectType, "category": o.Category, "url": o.URL,
		"source": spaceSource + ", from " + o.Source.Name + " (" + o.Source.License + ")",
		"note":   "Give its facts in the asker's language, in a few lines; the description is in English, to be told, not quoted."}
	put := func(key string, value any) {
		switch v := value.(type) {
		case string:
			if v != "" {
				out[key] = v
			}
		case *float64:
			if v != nil {
				out[key] = *v
			}
		}
	}
	put("magnitude", o.Mag)
	put("distance_ly", o.DistanceLy)
	put("distance_method", o.DistanceMethod)
	put("constellation", o.Constellation)
	put("discovered", o.Discovered)
	put("discovered_by", o.Properties.DiscoveredBy)
	put("hubble_type", o.Properties.HubbleType)
	put("spectral_type", o.Properties.Spectral)
	put("mass_suns", o.MassMsun)
	put("mass_kg", o.MassKg)
	put("radius_km", o.RadiusKm)
	put("orbits", o.Orbits)
	put("orbit_semi_major_axis_km", o.SemiMajorAxisKm)
	put("description", cleanText(o.Description, spaceFact))
	return out, nil
}

type spaceEvent struct {
	Kind    string `json:"kind"`
	Time    string `json:"time"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

func (a *apis) spaceEvents(ctx context.Context, days int, kind string) (any, error) {
	if days <= 0 {
		days = 30
	}
	days = min(days, spaceMaxDays)
	from := a.now().In(a.location)
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	query := url.Values{"from": {start.Format(time.DateOnly)}, "to": {start.AddDate(0, 0, days).Format(time.DateOnly)}, "limit": {strconv.Itoa(spaceShown)}}
	if kind != "" {
		query.Set("kind", kind)
	}
	got, err := spaceGet[struct {
		Events []spaceEvent `json:"events"`
	}](ctx, a, "/api/v1/events?"+query.Encode(), spaceEventsTTL)
	if err != nil {
		return nil, err
	}
	events := make([]map[string]string, 0, len(got.Events))
	for _, e := range got.Events {
		events = append(events, map[string]string{"when_tashkent": a.spaceTime(e.Time), "kind": e.Kind, "title": e.Title, "summary": cleanText(e.Summary, spaceFact)})
	}
	note := "Times are Tashkent's. Tell them in the asker's language."
	if len(events) == 0 {
		note = "Nothing of this kind in these days: say so."
	}
	return map[string]any{"days": days, "events": events, "note": note, "source": spaceSource}, nil
}

// spaceTime is a catalogue time as Tashkent's, "28 Nov 05:25"; "" for none.
func (a *apis) spaceTime(at string) string {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return ""
	}
	return t.In(a.location).Format("2 Jan 15:04")
}

func (a *apis) spaceTonight(ctx context.Context, name string, lat, lon float64) (any, error) {
	slug, err := a.spaceSlug(ctx, name)
	if err != nil {
		return nil, err
	}
	if slug == "" {
		return map[string]any{"name": name, "found": false, "note": "Not in the catalogue: say so.", "source": spaceSource}, nil
	}
	day := a.now().In(a.location).Format(time.DateOnly)
	query := url.Values{"object": {slug}, "lat": {fmtCoord(lat)}, "lon": {fmtCoord(lon)}, "date": {day}}
	got, err := spaceGet[struct {
		Target struct {
			Name string `json:"name"`
		} `json:"target"`
		Night struct {
			Sunset    string `json:"sunset"`
			DarkStart string `json:"dark_start"`
			DarkEnd   string `json:"dark_end"`
		} `json:"night"`
		Events struct {
			Rise        string   `json:"rise"`
			Set         string   `json:"set"`
			Transit     string   `json:"transit"`
			TransitAlt  *float64 `json:"transit_altitude_deg"`
			Circumpolar bool     `json:"circumpolar"`
			NeverRises  bool     `json:"never_rises"`
		} `json:"events"`
		Observability struct {
			Hours float64 `json:"hours_observable"`
			Best  struct {
				Time string `json:"time"`
			} `json:"best"`
		} `json:"observability"`
		Moon struct {
			PhaseName    string  `json:"phase_name"`
			Illumination float64 `json:"illumination"`
		} `json:"moon"`
	}](ctx, a, "/api/v1/ephemeris?"+query.Encode(), spaceObjectTTL)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"name": got.Target.Name, "night_of": day,
		"rises": a.spaceTime(got.Events.Rise), "highest": a.spaceTime(got.Events.Transit), "sets": a.spaceTime(got.Events.Set),
		"never_sets": got.Events.Circumpolar, "never_rises": got.Events.NeverRises,
		"hours_well_placed": math.Round(got.Observability.Hours*10) / 10, "best_time": a.spaceTime(got.Observability.Best.Time),
		"sunset": a.spaceTime(got.Night.Sunset), "dark_from": a.spaceTime(got.Night.DarkStart), "dark_until": a.spaceTime(got.Night.DarkEnd),
		"moon": fmt.Sprintf("%s, %d%% lit", got.Moon.PhaseName, int(math.Round(got.Moon.Illumination*100))),
		"note": "Times are Tashkent time. No rise means it is already up at sunset, no set that it is still up at sunrise. Tell them in the asker's language.", "source": spaceSource}
	if got.Events.TransitAlt != nil {
		out["highest_altitude_deg"] = math.Round(*got.Events.TransitAlt)
	}
	return out, nil
}

func (a *apis) spaceVisible(ctx context.Context, lat, lon float64) (any, error) {
	query := url.Values{"lat": {fmtCoord(lat)}, "lon": {fmtCoord(lon)}, "limit": {strconv.Itoa(spaceShown)}, "min_alt": {"20"}}
	got, err := spaceGet[struct {
		Sun struct {
			Condition string `json:"condition"`
		} `json:"sun"`
		Objects []struct {
			Name       string   `json:"name"`
			ObjectType string   `json:"object_type"`
			Mag        *float64 `json:"mag"`
			Altitude   *float64 `json:"altitude_deg"`
			Azimuth    *float64 `json:"azimuth_deg"`
		} `json:"objects"`
	}](ctx, a, "/api/v1/visible?"+query.Encode(), spaceSkyTTL)
	if err != nil {
		return nil, err
	}
	up := make([]map[string]any, 0, len(got.Objects))
	var faint []string
	for _, o := range got.Objects {
		if o.Name == "Sun" {
			continue
		}
		if !seenByEye(got.Sun.Condition, o.Name, o.Mag) {
			faint = append(faint, o.Name)
			continue
		}
		row := map[string]any{"name": o.Name, "type": o.ObjectType}
		if o.Mag != nil {
			row["magnitude"] = *o.Mag
		}
		if o.Altitude != nil {
			row["altitude_deg"] = math.Round(*o.Altitude)
		}
		if o.Azimuth != nil {
			row["direction"] = compass(*o.Azimuth)
		}
		up = append(up, row)
	}
	out := map[string]any{"sky": got.Sun.Condition, "up": up, "time_tashkent": a.now().In(a.location).Format("15:04"),
		"note": "up is what the eye can see now, brightest first. above_horizon_unseen are up but lost in the Sun's light: never call them visible. With up empty, say nothing can be seen by eye now and when it gets dark.", "source": spaceSource}
	if len(faint) > 0 {
		out["above_horizon_unseen"] = faint
	}
	return out, nil
}

// seenByEye is whether an object of magnitude mag shows to the naked eye
// under the sky condition: in daylight only the Moon and Venus, at dusk
// the brightest stars and planets, at night all the catalogue gives.
func seenByEye(condition, name string, mag *float64) bool {
	if name == "Moon" {
		return true
	}
	limit, dim := map[string]float64{"daylight": -3.5, "civil twilight": 1, "nautical twilight": 3}[condition]
	return !dim || (mag != nil && *mag <= limit)
}

func fmtCoord(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// compass is an azimuth as a direction, in Uzbek.
func compass(deg float64) string {
	names := []string{"shimol", "shimoli-sharq", "sharq", "janubi-sharq", "janub", "janubi-g'arb", "g'arb", "shimoli-g'arb"}
	return names[int(math.Mod(deg+22.5+360, 360)/45)%8]
}
