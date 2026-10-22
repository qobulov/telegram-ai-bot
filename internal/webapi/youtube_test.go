package webapi

import (
	"strings"
	"testing"
)

func youtubeOptions(u *upstream) Options {
	opts := u.options()
	opts.YouTubeKey = "yt-test"
	return opts
}

func TestYouTubeSearchGivesVideosWithLengthAndViews(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/youtube/search": ok(`{"items":[
			{"id":{"videoId":"C85g4BVKSrM"},"snippet":{"title":"How to Play Ling","channelTitle":"Rickfil ML","publishedAt":"2026-05-26T10:00:00Z"}},
			{"id":{"channelId":"UCx"},"snippet":{"title":"A channel"}},
			{"id":{"videoId":"live1"},"snippet":{"title":"Live","channelTitle":"Stream","publishedAt":"2026-10-01T00:00:00Z"}}]}`),
		"/youtube/videos": ok(`{"items":[
			{"id":"C85g4BVKSrM","contentDetails":{"duration":"PT12M3S"},"statistics":{"viewCount":"120345"}},
			{"id":"live1","contentDetails":{"duration":"P0D"},"statistics":{"viewCount":"5"}}]}`),
	})

	out := result[youtubeOutput](t, run(t, youtubeOptions(u), "youtube_search", `{"query":" ling  guide ","recent":true}`))

	want := youtubeVideo{Title: "How to Play Ling", Channel: "Rickfil ML", Published: "2026-05-26", Duration: "12:03", Views: "120345", Link: "https://youtu.be/C85g4BVKSrM"}
	if len(out.Videos) != 2 || out.Videos[0] != want || out.Videos[1].Duration != "" {
		t.Errorf("videos = %+v, want the channel dropped and a live stream without length", out.Videos)
	}
	search := u.requests("/youtube/search")[0]
	if q := search.URL.Query(); q.Get("q") != "ling guide" || q.Get("order") != "date" || q.Get("type") != "video" || q.Get("safeSearch") != "moderate" {
		t.Errorf("query = %v", q)
	}
	if search.Header.Get("X-Goog-Api-Key") != "yt-test" || strings.Contains(search.URL.String(), "yt-test") {
		t.Error("the key must go in the header only")
	}
	if q := u.requests("/youtube/videos")[0].URL.Query(); q.Get("id") != "C85g4BVKSrM,live1" {
		t.Errorf("details asked for %q", q.Get("id"))
	}
}

// Length and views are extras: without them the videos are still given.
func TestYouTubeSearchWithoutDetails(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/youtube/search": ok(`{"items":[{"id":{"videoId":"a"},"snippet":{"title":"A","channelTitle":"C","publishedAt":"2026-01-01T00:00:00Z"}}]}`),
		"/youtube/videos": status(500),
	})
	if out := result[youtubeOutput](t, run(t, youtubeOptions(u), "youtube_search", `{"query":"a"}`)); len(out.Videos) != 1 || out.Videos[0].Duration != "" {
		t.Errorf("videos = %+v", out.Videos)
	}
}

func TestYouTubeSearchRefusals(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/youtube/search": ok(`{"items":[]}`)})
	wantError(t, run(t, youtubeOptions(u), "youtube_search", `{"query":"zzqx"}`), "invalid_arguments", "no video")
	wantError(t, run(t, youtubeOptions(u), "youtube_search", `{"query":"  "}`), "invalid_arguments", "query")
	if strings.Contains(toolNames(Tools(u.options())), "youtube_search") {
		t.Error("youtube_search offered without a key")
	}
}

func TestVideoLength(t *testing.T) {
	for iso, want := range map[string]string{"PT12M3S": "12:03", "PT1H2M3S": "1:02:03", "PT45S": "0:45", "PT2H": "2:00:00", "P0D": "", "PT": "", "P1DT2H": ""} {
		if got := videoLength(iso); got != want {
			t.Errorf("videoLength(%q) = %q, want %q", iso, got, want)
		}
	}
}
