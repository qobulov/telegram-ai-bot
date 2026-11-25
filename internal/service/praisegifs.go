package service

import "strings"

// PraiseGIF is the GIF a praise of the bot is answered with, as the owners
// asked on 2026-11-25: one of the bot's own, cut from the owners' videos and
// served from the site's public/gifs, picked at random.
const PraiseGIF = "praised"

// praiseGIFFiles are the bot's own GIFs in public/gifs, added as the owners
// send videos.
var praiseGIFFiles = []string{
	"praise-number-one.mp4",          // "Lady Sunbringer", then "Sorry but I'm number one"
	"praise-number-one-poses.mp4",    // her face, glowing eye and poses
	"praise-gloria-faces.mp4",        // her spear, her outfit and three faces
	"praise-sunbringer-closeups.mp4", // her face, a strike over the sea, her outfit
	"praise-sunbringer-gem.mp4",      // her profile, her eyes, the crystal in her hands
	"praise-glance.mp4",              // her glance with the sword
	"praise-pose.mp4",                // her pose, then her face
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
