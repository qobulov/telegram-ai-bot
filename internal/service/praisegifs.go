package service

import "strings"

// PraiseGIF is the GIF a praise of the bot is answered with, as the owners
// asked on 2026-11-25: one of the bot's own, cut from the owners' videos and
// served from the site's public/gifs, picked at random.
const PraiseGIF = "praised"

// praiseGIFFiles are the bot's own GIFs in public/gifs, added as the owners
// send videos.
var praiseGIFFiles = []string{
	"praise-number-one.mp4", // "Sorry but I'm number one", Sunbringer's silhouette
	"praise-eyes.mp4",       // Sunbringer's eyes, then the crystal in her hands
	"praise-glance.mp4",     // Sunbringer's glance over her shoulder
	"praise-pose.mp4",       // Sunbringer's pose, then her face
}

// PraiseGIFLinks are the bot's own GIFs' addresses on the site at base.
func PraiseGIFLinks(base string) []string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		return nil
	}
	links := make([]string, len(praiseGIFFiles))
	for i, f := range praiseGIFFiles {
		links[i] = base + "/gifs/" + f
	}
	return links
}
