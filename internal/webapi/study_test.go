package webapi

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestMathIsComputedByMathJS(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/mathjs/v4/": ok(`{"result":"2 * x + cos(x)","error":null}`)})

	env := run(t, u.options(), "study", `{"action":"math","query":"derivative('x^2+sin(x)','x')"}`)

	if !env.OK || !strings.Contains(env.raw, `"result":"2 * x + cos(x)"`) {
		t.Fatalf("math = %s", env.raw)
	}
	body, _ := io.ReadAll(u.requests("/mathjs/v4/")[0].Body)
	var sent struct {
		Expr      string `json:"expr"`
		Precision int    `json:"precision"`
	}
	if err := json.Unmarshal(body, &sent); err != nil || sent.Expr != "derivative('x^2+sin(x)','x')" || sent.Precision != 14 {
		t.Errorf("sent %s", body)
	}
}

// An expression math.js cannot read comes back with its explanation, for
// the model to fix.
func TestMathErrorsAreExplained(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/mathjs/v4/": {status: 400, body: `{"result":null,"error":"Error: Unexpected end of expression (char 4)"}`}})
	wantError(t, run(t, u.options(), "study", `{"action":"math","query":"1/("}`), "invalid_arguments", "Unexpected end of expression")
	wantError(t, run(t, u.options(), "study", `{"action":"math","query":"  "}`), "invalid_arguments", "query")
}

func TestPapersFromCrossref(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/crossref/works": ok(`{"message":{"items":[
		{"title":["Water use in Uzbekistan"],"author":[{"given":"","family":""},{"given":"A.","family":"Karimov"},{"name":"UNDP"}],"issued":{"date-parts":[[2021,5]]},"DOI":"10.1/x","container-title":["Water Journal"],"is-referenced-by-count":12},
		{"title":["No year"],"issued":{"date-parts":[[null]]},"DOI":"10.1/y"},
		{"title":[],"DOI":"10.1/z"}]}}`)})

	out := result[struct {
		Papers []paper `json:"papers"`
	}](t, run(t, u.options(), "study", `{"action":"papers","query":"water uzbekistan"}`))

	if len(out.Papers) != 2 || out.Papers[0].Year != 2021 || out.Papers[0].Link != "https://doi.org/10.1/x" || strings.Join(out.Papers[0].Authors, "|") != "A. Karimov|UNDP" || out.Papers[1].Year != 0 {
		t.Errorf("papers = %+v", out.Papers)
	}
}

func TestArxivPreprintsWithPDFLinks(t *testing.T) {
	u := newUpstream(t, map[string]stub{"/arxiv/api/query": ok(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><entry>
		<id>http://arxiv.org/abs/2110.05151v3</id><title>WeTS: A Benchmark
		for Translation Suggestion</title><summary>Translation Suggestion helps.</summary><published>2021-10-11T00:00:00Z</published>
		<author><name>Zhen Yang</name></author>
		<link href="https://arxiv.org/abs/2110.05151v3" rel="alternate"/><link href="https://arxiv.org/pdf/2110.05151v3" rel="related" title="pdf"/>
	</entry></feed>`)})

	out := result[struct {
		Papers []paper `json:"papers"`
	}](t, run(t, u.options(), "study", `{"action":"arxiv","query":"transformer translation"}`))

	if len(out.Papers) != 1 || out.Papers[0].Title != "WeTS: A Benchmark for Translation Suggestion" || out.Papers[0].PDF != "https://arxiv.org/pdf/2110.05151v3" || out.Papers[0].Link != "https://arxiv.org/abs/2110.05151v3" || out.Papers[0].Year != 2021 {
		t.Errorf("papers = %+v", out.Papers)
	}
	if q := u.requests("/arxiv/api/query")[0].URL.Query().Get("search_query"); q != "all:transformer AND all:translation" {
		t.Errorf("search_query = %q", q)
	}
}

func TestEnglishWordsFromDatamuse(t *testing.T) {
	u := newUpstream(t, map[string]stub{
		"/datamuse/words?rel_syn=happy": ok(`[{"word":"glad"},{"word":"content"}]`),
		"/datamuse/words?rel_ant=qwzx":  ok(`[]`),
	})
	if env := run(t, u.options(), "study", `{"action":"synonyms","query":"Happy"}`); !env.OK || !strings.Contains(env.raw, `"synonyms":["glad","content"]`) {
		t.Errorf("synonyms = %s", env.raw)
	}
	wantError(t, run(t, u.options(), "study", `{"action":"antonyms","query":"qwzx"}`), "invalid_arguments", "no antonyms")
}
