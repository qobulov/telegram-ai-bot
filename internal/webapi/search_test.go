package webapi

import (
	"encoding/json"
	"strings"
	"testing"
)

const tavilyMLBB = `{"query":"q","answer":"The latest patch is 2.2.16a, released on 14 October.","results":[
	{"title":"MLBB Patch Notes","url":"https://www.mlbbhub.com/patch-notes","content":"Kalea buffed; Aulus nerfed."},
	{"title":"Odd link","url":"javascript:alert(1)","content":"x"}]}`

func searchOptions(u *upstream) Options {
	opts := u.options()
	opts.TavilyKey = "tvly-test"
	return opts
}

func TestWebSearchAsksTavilyAndKeepsWebLinks(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/tavily/search": ok(tavilyMLBB)})

	out := result[searchOutput](t, run(t, searchOptions(u), "web_search", `{"query":"  MLBB latest   patch ","news":true}`))

	if !strings.Contains(out.Answer, "2.2.16a") || out.Source == "" || out.Note == "" {
		t.Errorf("output = %+v", out)
	}
	if len(out.Results) != 1 || out.Results[0] != (searchResult{Title: "MLBB Patch Notes", Site: "mlbbhub.com", URL: "https://www.mlbbhub.com/patch-notes", Snippet: "Kalea buffed; Aulus nerfed."}) {
		t.Errorf("results = %+v, want the javascript link dropped", out.Results)
	}
	req := u.requests("/tavily/search")[0]
	if req.Method != "POST" || req.Header.Get("Authorization") != "Bearer tvly-test" || strings.Contains(req.URL.String(), "tvly-test") {
		t.Errorf("request = %s %s %v, want the key in the header only", req.Method, req.URL, req.Header)
	}
}

func TestWebSearchSendsTheQueryAsJSON(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/tavily/search": ok(tavilyMLBB)})
	run(t, searchOptions(u), "web_search", `{"query":"MLBB patch","news":true}`)
	var sent tavilyRequest
	if err := json.NewDecoder(u.requests("/tavily/search")[0].Body).Decode(&sent); err != nil {
		t.Fatal(err)
	}
	if sent != (tavilyRequest{Query: "MLBB patch", MaxResults: 5, IncludeAnswer: true, Topic: "news", Days: 7}) {
		t.Errorf("sent = %+v", sent)
	}
}

func TestWebSearchOnlyWithAKey(t *testing.T) {
	u := newUpstream(t, nil)
	if strings.Contains(toolNames(Tools(u.options())), "web_search") {
		t.Error("web_search offered without a key")
	}
	if !strings.Contains(toolNames(Tools(searchOptions(u))), "web_search") {
		t.Error("web_search missing with a key")
	}
	wantError(t, run(t, searchOptions(u), "web_search", `{"query":"   "}`), "invalid_arguments", "query")
	if u.total() != 0 {
		t.Errorf("a bad query reached Tavily %d times", u.total())
	}
}
