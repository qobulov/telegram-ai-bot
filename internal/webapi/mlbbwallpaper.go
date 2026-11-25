package webapi

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"

	"telegram-ai-bot/internal/tools"
)

// A hero's official wallpaper, from Rone Arena's copy of the game's
// gallery, as the owners asked on 2026-11-25. One of the hero's own, not
// those of every hero at once, is sent at random, shrunk to fit a chat.

const (
	// wallpaperWidth shrinks a 2560-wide wallpaper to some 300 KB.
	wallpaperWidth = "1280"
	wallpaperLimit = 4 << 20
)

// arenaWallpaper is a gallery entry: the heroes it shows and its sizes.
type arenaWallpaper struct {
	Heroes   []int `json:"heroid"`
	Pictures []struct {
		Resolution string `json:"resolution"`
		URL        string `json:"url"`
	} `json:"pictures"`
}

func (a *apis) mlbbWallpaper(ctx context.Context, name string) (any, error) {
	names, err := a.mlbbHeroes(ctx)
	if err != nil {
		return nil, err
	}
	id, err := findHero(names, name)
	if err != nil {
		return nil, err
	}
	walls, err := remember(a.cache, fmt.Sprintf("mlbb:wallpapers:%d", id), mlbbHeroesTTL, func() ([]arenaWallpaper, error) {
		return arenaList[arenaWallpaper](ctx, a, fmt.Sprintf("/api/heroes/%d/wallpapers", id))
	})
	if err != nil {
		return nil, err
	}
	var links []string
	for _, w := range walls {
		if len(w.Heroes) != 1 {
			continue
		}
		if link := landscape(w); link != "" {
			links = append(links, link)
		}
	}
	out := map[string]any{"hero": names[id], "wallpapers": len(links), "source": arenaService + ", the game's official gallery"}
	sink, ok := tools.SinkFrom(ctx)
	if len(links) == 0 || !ok {
		out["note"] = "There is no wallpaper of this hero, so none is sent: say so plainly, never as if one were."
		return out, nil
	}
	link := links[rand.IntN(len(links))]
	data, err := a.patient.get(ctx, call{service: arenaService, url: link + "&w=" + wallpaperWidth + "&q=85&output=jpg", limit: wallpaperLimit})
	if err != nil {
		out["note"] = "The wallpaper could not be fetched, so none is sent: say so plainly, never as if one were."
		return out, nil
	}
	sink.Add(tools.Drawing{Data: data, MimeType: http.DetectContentType(data)})
	out["note"] = "One of them, at random, is sent with your answer, which becomes its caption: keep it short. Ask again for another."
	return out, nil
}

// landscape is the wallpaper's 1920 by 1080 picture, else its first.
func landscape(w arenaWallpaper) string {
	for _, p := range w.Pictures {
		if p.Resolution == "1920x1080" && strings.HasPrefix(p.URL, "http") {
			return p.URL
		}
	}
	for _, p := range w.Pictures {
		if strings.HasPrefix(p.URL, "http") {
			return p.URL
		}
	}
	return ""
}
