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
	"praise-number-one-poses.mp4",    // Sunbringer's face, glowing eye and poses
	"praise-gloria-faces.mp4",        // Sunbringer's faces and eyes, cut after cut
	"praise-gloria-strike.mp4",       // a burst among the stars, her eyes, her pose with the spear
	"praise-sunbringer-closeups.mp4", // Sunbringer's face, a strike and her eyes
	"praise-lady-sunbringer.mp4",     // "Lady Sunbringer", her face and the moon
	"praise-gloria-vortex.mp4",       // lightning, a blue vortex and a pillar of crystal
	"praise-gloria-flight.mp4",       // a blue city, red flames, her flight
	"praise-gloria-slash.mp4",        // her spear's blade flashing, a white swirl
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
