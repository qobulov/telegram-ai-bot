package webapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	"telegram-ai-bot/internal/tools"
)

const (
	shikimoriSource = "Shikimori"
	traceMoeSource  = "trace.moe"
	// sureScene is the similarity under which trace.moe's match is only a
	// guess: its own advice is that below 0.9 it is likely wrong.
	sureScene = 0.9
)

// --- Russian titles and descriptions (Shikimori) ---

type shikimoriTitle struct {
	Russian  string   `json:"russian"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind,omitempty"`
	Score    string   `json:"score,omitempty"`
	Episodes int      `json:"episodes,omitempty"`
	Status   string   `json:"status,omitempty"`
	Aired    string   `json:"aired,omitempty"`
	Genres   []string `json:"genres,omitempty"`
	Studio   string   `json:"studio,omitempty"`
	About    string   `json:"about,omitempty"`
	Link     string   `json:"link"`
}

// bbCode is Shikimori's markup in descriptions, "[character=7407]...[/character]".
var bbCode = regexp.MustCompile(`\[/?[a-z_]+(?:=[^\]]*)?\]`)

// shikimori finds anime by name with Russian titles; the first gets its
// description, genres and studio too.
func (a *apis) shikimori(ctx context.Context, query string) (any, error) {
	var found []struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Russian  string `json:"russian"`
		URL      string `json:"url"`
		Kind     string `json:"kind"`
		Score    string `json:"score"`
		Status   string `json:"status"`
		Episodes int    `json:"episodes"`
		AiredOn  string `json:"aired_on"`
	}
	link := a.urls.Shikimori + "/api/animes?limit=5&search=" + url.QueryEscape(query)
	if err := a.cachedJSON(ctx, shikimoriSource, link, animeTTL, &found); err != nil {
		return nil, err
	}
	var out []shikimoriTitle
	for _, f := range found {
		t := shikimoriTitle{Russian: cleanText(f.Russian, 100), Name: cleanText(f.Name, 100), Kind: f.Kind, Episodes: f.Episodes, Status: f.Status, Aired: f.AiredOn, Link: a.urls.Shikimori + f.URL}
		if f.Score != "0.0" {
			t.Score = f.Score
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, argumentError("Shikimori found no anime; try the English or romaji title")
	}
	var detail struct {
		Description string `json:"description"`
		Genres      []struct {
			Russian string `json:"russian"`
		} `json:"genres"`
		Studios []struct {
			Name string `json:"name"`
		} `json:"studios"`
	}
	if err := a.cachedJSON(ctx, shikimoriSource, fmt.Sprintf("%s/api/animes/%d", a.urls.Shikimori, found[0].ID), animeTTL, &detail); err == nil {
		out[0].About = cleanText(bbCode.ReplaceAllString(detail.Description, ""), shownAnimeDesc*2)
		for _, g := range detail.Genres {
			out[0].Genres = append(out[0].Genres, g.Russian)
		}
		if len(detail.Studios) > 0 {
			out[0].Studio = cleanText(detail.Studios[0].Name, 60)
		}
	}
	return map[string]any{"anime": out, "source": shikimoriSource}, nil
}

// --- Which anime a picture is from (trace.moe) ---

// animeScene asks trace.moe which anime, episode and moment the turn's
// picture is from. The picture is uploaded, so that no Telegram file link,
// which carries the bot's token, leaves the bot.
func (a *apis) animeScene(ctx context.Context) (any, error) {
	caller, _ := tools.CallerFrom(ctx)
	if len(caller.Picture) == 0 {
		return nil, argumentError("no picture came with the message: ask them to send a screenshot from the anime, or reply to one, with the question")
	}
	var answer struct {
		Error  string `json:"error"`
		Result []struct {
			Anilist struct {
				Title struct {
					Romaji  string `json:"romaji"`
					English string `json:"english"`
					Native  string `json:"native"`
				} `json:"title"`
				IsAdult bool `json:"isAdult"`
			} `json:"anilist"`
			Episode    any     `json:"episode"`
			From       float64 `json:"from"`
			Similarity float64 `json:"similarity"`
		} `json:"result"`
	}
	header := http.Header{"Content-Type": {http.DetectContentType(caller.Picture)}}
	if err := a.patient.getJSON(ctx, call{service: traceMoeSource, url: a.urls.TraceMoe + "/search?anilistInfo&cutBorders", header: header, body: caller.Picture}, &answer); err != nil {
		return nil, err
	}
	if answer.Error != "" {
		return nil, fmt.Errorf("%s: %s", traceMoeSource, cleanText(answer.Error, 200))
	}
	if len(answer.Result) == 0 {
		return nil, argumentError("trace.moe found no anime in this picture")
	}
	best := answer.Result[0]
	if best.Anilist.IsAdult {
		return nil, argumentError("the picture is from an adult anime, which you do not name")
	}
	minutes, seconds := int(best.From)/60, int(best.From)%60
	out := map[string]any{
		"anime":      firstNonEmpty(best.Anilist.Title.English, best.Anilist.Title.Romaji),
		"romaji":     best.Anilist.Title.Romaji,
		"native":     best.Anilist.Title.Native,
		"at":         fmt.Sprintf("%d:%02d", minutes, seconds),
		"similarity": fmt.Sprintf("%.0f%%", best.Similarity*100),
		"source":     traceMoeSource,
	}
	if best.Episode != nil {
		out["episode"] = fmt.Sprint(best.Episode)
	}
	if best.Similarity < sureScene {
		out["note"] = "The match is weak: say it is only a guess. trace.moe works on screenshots of the anime itself, not on fan art."
	}
	return out, nil
}
