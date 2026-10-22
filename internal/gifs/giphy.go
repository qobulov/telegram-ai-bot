// Package gifs finds a GIF for a mood on GIPHY, for Maria to send now and
// then instead of a sticker.
package gifs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	defaultBaseURL = "https://api.giphy.com/v1"
	// results is how many GIFs a search brings back to choose from.
	results = 10
	// maxResponseBytes bounds a search's answer; ten results are far less.
	maxResponseBytes = 1 << 20
)

// queries are what is searched for each sticker category: a few English
// words GIPHY's search knows well.
var queries = map[string]string{
	"funny":       "lol laughing",
	"sarcasm":     "sarcastic eye roll",
	"flirting":    "wink flirt",
	"jealousy":    "jealous",
	"sad":         "sad crying",
	"comfort":     "hug comfort",
	"shock":       "shocked surprised",
	"celebration": "celebrate party",
	"facepalm":    "facepalm",
	"angry":       "angry mad",
	"wholesome":   "wholesome cute",
	"cool":        "deal with it cool",
}

// Client searches GIPHY.
type Client struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

type searchResult struct {
	Data []struct {
		Title  string `json:"title"`
		Slug   string `json:"slug"`
		Images struct {
			FixedHeight struct {
				MP4 string `json:"mp4"`
			} `json:"fixed_height"`
		} `json:"images"`
	} `json:"data"`
}

// Search returns the MP4 links of GIFs for subject, a sticker category or
// a topic in a few English words such as "pubg", rated PG at most so that
// nothing unfit for a group comes back.
func (c *Client) Search(ctx context.Context, subject string) ([]string, error) {
	query, ok := queries[subject]
	if !ok {
		query = strings.TrimSpace(subject)
	}
	if query == "" {
		return nil, nil
	}
	base := c.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	params := url.Values{
		"api_key": {c.APIKey}, "q": {query}, "limit": {fmt.Sprint(results)},
		"rating": {"pg"}, "lang": {"en"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/gifs/search?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("giphy: build request: %w", err)
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		// The URL carries the key; the error must not.
		return nil, fmt.Errorf("giphy: search failed: %w", unwrapURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("giphy: search answered %d", resp.StatusCode)
	}
	var found searchResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&found); err != nil {
		return nil, fmt.Errorf("giphy: read the answer: %w", err)
	}
	var links []string
	words := distinctWords(subject)
	for _, g := range found.Data {
		if link := g.Images.FixedHeight.MP4; link != "" && (ok || about(g.Title+" "+g.Slug, words)) {
			links = append(links, link)
		}
	}
	return links, nil
}

// broadWords name a whole game or say nothing: a GIF of "mobile legends
// ling" must be about Ling, not about the game, which GIPHY gives for any
// of its heroes.
var broadWords = map[string]bool{"mobile": true, "legends": true, "legend": true, "mlbb": true, "ml": true, "bang": true, "gif": true, "the": true, "a": true, "of": true, "game": true}

// distinctWords are the words of a topic that a GIF about it must mention.
func distinctWords(topic string) []string {
	var words []string
	for _, w := range strings.Fields(strings.ToLower(topic)) {
		if !broadWords[w] {
			words = append(words, w)
		}
	}
	return words
}

// about reports whether a GIF's title and slug mention one of words; any
// GIF is about a topic made only of broad words.
func about(text string, words []string) bool {
	if len(words) == 0 {
		return true
	}
	text = strings.ToLower(text)
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// unwrapURL drops the request URL, with the key in it, from err.
func unwrapURL(err error) error {
	if urlErr, ok := err.(*url.Error); ok {
		return urlErr.Err
	}
	return err
}
