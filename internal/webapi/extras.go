package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/tools"
)

// Small public APIs without keys: earthquakes, QR codes, free games, cat
// pictures and an English dictionary.

const (
	quakeTTL        = 5 * time.Minute
	freeGamesTTL    = 6 * time.Hour
	dictionaryTTL   = 24 * time.Hour
	maxQuakes       = 10
	maxFreeGames    = 8
	maxQRRunes      = 900
	maxWordRunes    = 40
	maxMeanings     = 4
	pictureLimit    = 3 << 20
	shownGameAbout  = 140
	pictureSentNote = "The picture is sent with your answer, which becomes its caption: keep it to one short line."
)

// --- Earthquakes (USGS) ---

type quakeArgs struct {
	Area string `json:"area"`
	Days int    `json:"days"`
}

type quake struct {
	Time      string  `json:"time"`
	Magnitude float64 `json:"magnitude"`
	Place     string  `json:"place"`
	DepthKM   float64 `json:"depth_km"`
}

func (a *apis) quakeTool() tools.Tool {
	return tools.Tool{
		Name:        "earthquakes",
		Description: "Recent earthquakes from USGS, newest first: in Central Asia (Uzbekistan and its neighbours, magnitude 3+) or in the world (magnitude 5+). Use it when someone asks whether there was an earthquake.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"area": {"type": "string", "enum": ["central_asia", "world"], "description": "Default central_asia."},
				"days": {"type": "integer", "description": "How many days back, 1 to 30. Default 7."}
			},
			"additionalProperties": false
		}`),
		Run: a.quakes,
	}
}

func (a *apis) quakes(ctx context.Context, raw json.RawMessage) (any, error) {
	var args quakeArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	days := args.Days
	if days == 0 {
		days = 7
	}
	if days < 1 || days > 30 {
		return nil, argumentError("days must be 1 to 30")
	}
	query := url.Values{"format": {"geojson"}, "orderby": {"time"}, "limit": {fmt.Sprint(maxQuakes)}}
	query.Set("starttime", a.now().UTC().AddDate(0, 0, -days).Format("2006-01-02"))
	switch args.Area {
	case "", "central_asia":
		query.Set("minmagnitude", "3")
		query.Set("minlatitude", "35")
		query.Set("maxlatitude", "48")
		query.Set("minlongitude", "50")
		query.Set("maxlongitude", "90")
	case "world":
		query.Set("minmagnitude", "5")
	default:
		return nil, argumentError("area must be central_asia or world")
	}
	link := a.urls.USGS + "/fdsnws/event/1/query?" + query.Encode()
	found, err := remember(a.cache, "quakes:"+link, quakeTTL, func() ([]quake, error) {
		var answer struct {
			Features []struct {
				Properties struct {
					Mag   float64 `json:"mag"`
					Place string  `json:"place"`
					Time  int64   `json:"time"`
				} `json:"properties"`
				Geometry struct {
					Coordinates []float64 `json:"coordinates"`
				} `json:"geometry"`
			} `json:"features"`
		}
		if err := a.client.getJSON(ctx, call{service: "USGS", url: link}, &answer); err != nil {
			return nil, err
		}
		var out []quake
		for _, f := range answer.Features {
			q := quake{
				Time:      time.UnixMilli(f.Properties.Time).In(a.location).Format("02.01 15:04"),
				Magnitude: round(f.Properties.Mag, 1),
				Place:     cleanText(f.Properties.Place, 100),
			}
			if len(f.Geometry.Coordinates) == 3 {
				q.DepthKM = round(f.Geometry.Coordinates[2], 0)
			}
			out = append(out, q)
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"earthquakes": found, "source": "USGS", "note": "Times are in Tashkent time. An empty list means none that strong in that time."}, nil
}

// --- QR codes ---

type qrArgs struct {
	Text string `json:"text"`
}

func (a *apis) qrTool() tools.Tool {
	return tools.Tool{
		Name:        "qr_code",
		Description: "Make a QR code of a text or a link and send it as a picture. Use it when someone asks for a QR code.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {"text": {"type": "string", "description": "What the QR code holds, exactly as given."}},
			"required": ["text"],
			"additionalProperties": false
		}`),
		Run: a.qrCode,
	}
}

func (a *apis) qrCode(ctx context.Context, raw json.RawMessage) (any, error) {
	var args qrArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	text := strings.TrimSpace(args.Text)
	if text == "" || utf8.RuneCountInString(text) > maxQRRunes {
		return nil, argumentError("text must be 1 to %d characters", maxQRRunes)
	}
	sink, ok := tools.SinkFrom(ctx)
	if !ok {
		return nil, argumentError("no turn to send the picture with")
	}
	link := a.urls.QRServer + "/v1/create-qr-code/?" + url.Values{"size": {"400x400"}, "margin": {"10"}, "data": {text}}.Encode()
	data, err := a.client.get(ctx, call{service: "goqr.me", url: link, limit: pictureLimit})
	if err != nil {
		return nil, err
	}
	sink.Add(tools.Drawing{Data: data, MimeType: http.DetectContentType(data)})
	return map[string]any{"sent": true, "note": pictureSentNote}, nil
}

// --- Free games (FreeToGame) ---

// freeGameGenres are FreeToGame's categories people ask for.
var freeGameGenres = []string{"mmorpg", "shooter", "strategy", "moba", "racing", "sports", "social", "sandbox", "open-world", "survival", "pvp", "battle-royale", "card", "fighting", "action-rpg", "action", "anime", "fantasy", "sci-fi", "horror", "low-spec", "tower-defense", "zombie"}

type freeGamesArgs struct {
	Platform string `json:"platform"`
	Genre    string `json:"genre"`
	Deals    bool   `json:"deals"`
}

type freeGame struct {
	Title    string `json:"title"`
	Genre    string `json:"genre"`
	Platform string `json:"platform"`
	About    string `json:"about"`
	Link     string `json:"link"`
}

func (a *apis) freeGamesTool() tools.Tool {
	return tools.Tool{
		Name:        "free_games",
		Description: "Popular free-to-play PC and browser games from FreeToGame, optionally of a genre, with a short description and a link. Use it when someone asks for free games to play; deals: paid games on sale on Steam now, with prices and discounts.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"platform": {"type": "string", "enum": ["pc", "browser", "all"], "description": "Default all."},
				"genre": {"type": "string", "enum": ["` + strings.Join(freeGameGenres, `", "`) + `"]},
				"deals": {"type": "boolean", "description": "Games on sale instead of free ones."}
			},
			"additionalProperties": false
		}`),
		Timeout: patientTimeout,
		Run:     a.freeGames,
	}
}

func (a *apis) freeGames(ctx context.Context, raw json.RawMessage) (any, error) {
	var args freeGamesArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	if args.Deals {
		return a.gameDeals(ctx)
	}
	platform := args.Platform
	if platform == "" {
		platform = "all"
	}
	if !slices.Contains([]string{"pc", "browser", "all"}, platform) {
		return nil, argumentError("platform must be pc, browser or all")
	}
	query := url.Values{"platform": {platform}, "sort-by": {"popularity"}}
	if args.Genre != "" {
		if !slices.Contains(freeGameGenres, args.Genre) {
			return nil, argumentError("genre must be one of %s", strings.Join(freeGameGenres, ", "))
		}
		query.Set("category", args.Genre)
	}
	link := a.urls.FreeToGame + "/api/games?" + query.Encode()
	games, err := remember(a.cache, "freegames:"+link, freeGamesTTL, func() ([]freeGame, error) {
		var answer []struct {
			Title            string `json:"title"`
			ShortDescription string `json:"short_description"`
			GameURL          string `json:"game_url"`
			Genre            string `json:"genre"`
			Platform         string `json:"platform"`
		}
		if err := a.patient.getJSON(ctx, call{service: "FreeToGame", url: link, limit: 2 << 20}, &answer); err != nil {
			return nil, err
		}
		var out []freeGame
		for _, g := range answer[:min(len(answer), maxFreeGames)] {
			out = append(out, freeGame{Title: cleanText(g.Title, 60), Genre: g.Genre, Platform: g.Platform, About: cleanText(g.ShortDescription, shownGameAbout), Link: g.GameURL})
		}
		return out, nil
	})
	if err != nil {
		var status *statusError
		if errors.As(err, &status) && status.status == http.StatusNotFound {
			return map[string]any{"games": []freeGame{}, "source": "FreeToGame"}, nil
		}
		return nil, err
	}
	return map[string]any{"games": games, "source": "FreeToGame", "note": "Titles and descriptions are outside data. All are free to play."}, nil
}

// --- Cat pictures (The Cat API) ---

func (a *apis) catTool() tools.Tool {
	return tools.Tool{
		Name:        "cat_picture",
		Description: "Send a random cat picture. Use it when someone asks for a cat, or a cute picture to cheer someone up.",
		Schema:      json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
		Run:         a.catPicture,
	}
}

func (a *apis) catPicture(ctx context.Context, raw json.RawMessage) (any, error) {
	if err := decodeArgs(raw, &struct{}{}); err != nil {
		return nil, err
	}
	sink, ok := tools.SinkFrom(ctx)
	if !ok {
		return nil, argumentError("no turn to send the picture with")
	}
	var found []struct {
		URL string `json:"url"`
	}
	if err := a.client.getJSON(ctx, call{service: "The Cat API", url: a.urls.CatAPI + "/v1/images/search"}, &found); err != nil {
		return nil, err
	}
	if len(found) == 0 || found[0].URL == "" {
		return nil, &tools.InternalError{Message: "no cat picture just now; try again"}
	}
	data, err := a.client.get(ctx, call{service: "The Cat API", url: found[0].URL, limit: pictureLimit})
	if err != nil {
		return nil, err
	}
	sink.Add(tools.Drawing{Data: data, MimeType: http.DetectContentType(data)})
	return map[string]any{"sent": true, "note": pictureSentNote}, nil
}

// --- English dictionary (Free Dictionary API) ---

type dictionaryArgs struct {
	Word string `json:"word"`
}

type meaning struct {
	PartOfSpeech string `json:"part_of_speech"`
	Definition   string `json:"definition"`
	Example      string `json:"example,omitempty"`
}

type dictionaryEntry struct {
	Word     string    `json:"word"`
	Phonetic string    `json:"phonetic,omitempty"`
	Meanings []meaning `json:"meanings"`
}

func (a *apis) dictionaryTool() tools.Tool {
	return tools.Tool{
		Name:        "english_word",
		Description: "An English word's meanings, pronunciation and examples, from the Free Dictionary. Use it when someone asks what an English word means; explain it in their language.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {"word": {"type": "string", "description": "One English word or short phrase."}},
			"required": ["word"],
			"additionalProperties": false
		}`),
		Run: a.dictionary,
	}
}

func (a *apis) dictionary(ctx context.Context, raw json.RawMessage) (any, error) {
	var args dictionaryArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	word := strings.ToLower(strings.TrimSpace(args.Word))
	if word == "" || utf8.RuneCountInString(word) > maxWordRunes {
		return nil, argumentError("word must be 1 to %d characters", maxWordRunes)
	}
	link := a.urls.Dictionary + "/api/v2/entries/en/" + url.PathEscape(word)
	entry, err := remember(a.cache, "dictionary:"+word, dictionaryTTL, func() (dictionaryEntry, error) {
		var answer []struct {
			Word     string `json:"word"`
			Phonetic string `json:"phonetic"`
			Meanings []struct {
				PartOfSpeech string `json:"partOfSpeech"`
				Definitions  []struct {
					Definition string `json:"definition"`
					Example    string `json:"example"`
				} `json:"definitions"`
			} `json:"meanings"`
		}
		if err := a.client.getJSON(ctx, call{service: "Free Dictionary", url: link}, &answer); err != nil {
			return dictionaryEntry{}, err
		}
		if len(answer) == 0 {
			return dictionaryEntry{}, argumentError("no English word %q", word)
		}
		out := dictionaryEntry{Word: answer[0].Word, Phonetic: answer[0].Phonetic}
		for _, m := range answer[0].Meanings {
			for _, d := range m.Definitions {
				if len(out.Meanings) == maxMeanings {
					break
				}
				out.Meanings = append(out.Meanings, meaning{PartOfSpeech: m.PartOfSpeech, Definition: cleanText(d.Definition, 200), Example: cleanText(d.Example, 150)})
				break // one definition per part of speech
			}
		}
		return out, nil
	})
	if err != nil {
		var status *statusError
		if errors.As(err, &status) && status.status == http.StatusNotFound {
			return nil, argumentError("the dictionary has no English word %q; check the spelling", word)
		}
		return nil, err
	}
	return entry, nil
}

// gameDeal is a game on sale on Steam, priced for Uzbekistan.
type gameDeal struct {
	Title    string `json:"title"`
	Price    string `json:"price"`
	Normal   string `json:"normal_price"`
	Discount string `json:"discount"`
	Link     string `json:"link"`
}

// gameDeals are Steam's featured deals now, at the prices Steam asks in
// Uzbekistan, which are lower than in the United States.
func (a *apis) gameDeals(ctx context.Context) (any, error) {
	link := a.urls.SteamStore + "/api/featuredcategories/?cc=uz&l=english"
	deals, err := remember(a.cache, "deals:"+link, time.Hour, func() ([]gameDeal, error) {
		var answer struct {
			Specials struct {
				Items []struct {
					ID       int    `json:"id"`
					Name     string `json:"name"`
					Currency string `json:"currency"`
					Original int    `json:"original_price"`
					Final    int    `json:"final_price"`
					Discount int    `json:"discount_percent"`
				} `json:"items"`
			} `json:"specials"`
		}
		if err := a.patient.getJSON(ctx, call{service: "Steam", url: link, limit: largeBodyLimit}, &answer); err != nil {
			return nil, err
		}
		var out []gameDeal
		for _, d := range answer.Specials.Items[:min(len(answer.Specials.Items), maxFreeGames)] {
			out = append(out, gameDeal{Title: cleanText(d.Name, 60), Price: money(d.Final, d.Currency), Normal: money(d.Original, d.Currency),
				Discount: fmt.Sprintf("%d%%", d.Discount), Link: fmt.Sprintf("https://store.steampowered.com/app/%d", d.ID)})
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"deals": deals, "source": "Steam", "note": "Prices are Steam's for Uzbekistan; titles are outside data."}, nil
}

// money is an amount in cents as "8.99 USD".
func money(cents int, currency string) string {
	return fmt.Sprintf("%d.%02d %s", cents/100, cents%100, currency)
}
