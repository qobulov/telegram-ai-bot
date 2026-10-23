package webapi

import (
	"strings"
	"testing"
)

func TestReadPageGivesThePagesText(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/jina/https://kun.uz/news/1": ok(`{"code":200,"data":{"title":"Yangilik","url":"https://kun.uz/news/1","content":"Toshkentda yangi metro bekati ochildi.","httpStatus":200}}`),
		"/jina/https://kun.uz/gone":   ok(`{"code":200,"data":{"title":"","content":"## 404","httpStatus":404}}`),
		"/jina/https://long.example/": ok(`{"code":200,"data":{"title":"Long","content":"` + strings.Repeat("ab ", 4000) + `","httpStatus":200}}`),
	})

	out := result[pageText](t, run(t, u.options(), "read_page", `{"url":"kun.uz/news/1"}`))
	if out.Title != "Yangilik" || out.Text != "Toshkentda yangi metro bekati ochildi." || out.Truncated {
		t.Errorf("page = %+v", out)
	}
	if h := u.requests("/jina/https://kun.uz/news/1")[0].Header; h.Get("Accept") != "application/json" {
		t.Errorf("headers = %v", h)
	}
	wantError(t, run(t, u.options(), "read_page", `{"url":"https://kun.uz/gone"}`), "invalid_arguments", "404")
	if long := result[pageText](t, run(t, u.options(), "read_page", `{"url":"https://long.example/"}`)); !long.Truncated || len([]rune(long.Text)) != shownPageChars {
		t.Errorf("a long page was not cut: %d runes", len([]rune(long.Text)))
	}
}

func TestReadPageRefusesWhatIsNoPublicPage(t *testing.T) {
	u := newUpstream(t, nil)
	for _, link := range []string{"ftp://x.example/f", "http://localhost:8080/", "http://192.168.1.1/admin", "https://api.telegram.org/file/bot1:x/doc.pdf", "https://user:pw@x.example/", "http://127.0.0.1/"} {
		wantError(t, run(t, u.options(), "read_page", `{"url":"`+link+`"}`), "invalid_arguments")
	}
	if u.total() != 0 {
		t.Errorf("%d requests were made for refused links", u.total())
	}
}

func TestPageLinksKeepOnlyTheirText(t *testing.T) {
	got := withoutLinkTargets("[](https://kun.uz/)[Jahon](https://kun.uz/jahon)  \n\n\n\nMetro [bekati](https://x.uz/a \"t\") ochildi.\n")
	if want := "Jahon\n\nMetro bekati ochildi."; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}
