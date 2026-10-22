package gifs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"telegram-ai-bot/internal/stickers"
)

func TestSearchAsksForAFamilyGIFOfTheMood(t *testing.T) {
	var asked string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"data":[{"images":{"fixed_height":{"mp4":"https://media.giphy.com/1.mp4"}}},{"images":{"fixed_height":{}}}],"meta":{"status":200}}`))
	}))
	defer server.Close()
	c := &Client{APIKey: "k", BaseURL: server.URL}

	links, err := c.Search(context.Background(), "facepalm")

	if err != nil || len(links) != 1 || links[0] != "https://media.giphy.com/1.mp4" {
		t.Fatalf("links = %q, err = %v", links, err)
	}
	for _, want := range []string{"q=facepalm", "rating=pg", "limit=10", "api_key=k"} {
		if !strings.Contains(asked, want) {
			t.Errorf("query %q lacks %q", asked, want)
		}
	}
}

func TestSearchFailureHidesTheKey(t *testing.T) {
	c := &Client{APIKey: "secret-key", BaseURL: "http://127.0.0.1:1"}
	_, err := c.Search(context.Background(), "funny")
	if err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Errorf("err = %v; want a failure that does not show the key", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	if _, err := (&Client{APIKey: "k", BaseURL: server.URL}).Search(context.Background(), "funny"); err == nil {
		t.Error("a 429 was taken for an answer")
	}
}

// Every sticker category has a search, and nothing else does.
func TestEveryCategoryHasAQuery(t *testing.T) {
	for _, c := range stickers.Categories {
		if queries[c] == "" {
			t.Errorf("no GIF search for %q", c)
		}
	}
	if len(queries) != len(stickers.Categories) {
		t.Errorf("%d queries for %d categories", len(queries), len(stickers.Categories))
	}
}

// A topic, which is no mood, is searched as written; nothing is not searched.
func TestSearchForATopic(t *testing.T) {
	var asked string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query().Get("q")
		_, _ = w.Write([]byte(`{"data":[{"title":"PUBG Winner GIF","images":{"fixed_height":{"mp4":"https://media.giphy.com/2.mp4"}}}]}`))
	}))
	defer server.Close()
	c := &Client{APIKey: "k", BaseURL: server.URL}

	if links, err := c.Search(context.Background(), "pubg winner"); err != nil || len(links) != 1 || asked != "pubg winner" {
		t.Errorf("links = %q, err = %v, asked %q", links, err, asked)
	}
	asked = ""
	if links, err := c.Search(context.Background(), "  "); links != nil || err != nil || asked != "" {
		t.Errorf("an empty topic searched: %q, %v", links, err)
	}
}

// A GIF of a topic must be about it: one about the game alone is not a GIF
// of one of its heroes.
func TestSearchKeepsGIFsAboutTheTopic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"title":"Mobile Legends Gg GIF","slug":"mobilelegends-gg-abc","images":{"fixed_height":{"mp4":"https://g/1.mp4"}}},
			{"title":"Free Fire Ff GIF","slug":"freefire-xyz","images":{"fixed_height":{"mp4":"https://g/2.mp4"}}},
			{"title":"","slug":"mlbb-ling-skin-123","images":{"fixed_height":{"mp4":"https://g/3.mp4"}}}]}`))
	}))
	defer server.Close()
	c := &Client{APIKey: "k", BaseURL: server.URL}

	if links, _ := c.Search(context.Background(), "mobile legends ling"); len(links) != 1 || links[0] != "https://g/3.mp4" {
		t.Errorf("ling = %q, want only the GIF about Ling", links)
	}
	if links, _ := c.Search(context.Background(), "mlbb"); len(links) != 3 {
		t.Errorf("mlbb = %q, want every GIF: the topic is the game", links)
	}
	if links, _ := c.Search(context.Background(), "facepalm"); len(links) != 3 {
		t.Errorf("a mood = %q, want every GIF", links)
	}
}
