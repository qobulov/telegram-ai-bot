package webapi

import (
	"cmp"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/tools"
)

// The study tool gathers keyless APIs for students under one schema:
// math.js computes, Crossref and arXiv find papers, Datamuse finds words.

const (
	studyTTL       = 6 * time.Hour
	maxExpression  = 300
	maxStudyQuery  = 120
	maxPapers      = 5
	maxRelatedWord = 12
	mathPrecision  = 14
)

type studyArgs struct {
	Action string `json:"action"`
	Query  string `json:"query"`
}

func (a *apis) studyTool() tools.Tool {
	return tools.Tool{
		Name: "study",
		Description: "For study: math computes exactly with math.js (\"derivative('x^2+sin(x)','x')\", \"det([1,2;3,4])\", \"5 km to mile\", big numbers; no integrals); use it rather than counting in your head. " +
			"papers finds scholarly articles (Crossref); arxiv preprints in science and IT, with PDF links; synonyms, antonyms, rhymes and means_like find English words.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["math", "papers", "arxiv", "synonyms", "antonyms", "rhymes", "means_like"]},
				"query": {"type": "string", "description": "The math.js expression, the topic in English, or the English word."}
			},
			"required": ["action", "query"],
			"additionalProperties": false
		}`),
		Timeout: patientTimeout,
		Run:     a.study,
	}
}

func (a *apis) study(ctx context.Context, raw json.RawMessage) (any, error) {
	var args studyArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(args.Query)
	limit := maxStudyQuery
	if args.Action == "math" {
		limit = maxExpression
	}
	if query == "" || utf8.RuneCountInString(query) > limit {
		return nil, argumentError("query must be 1 to %d characters", limit)
	}
	switch args.Action {
	case "math":
		return a.mathjs(ctx, query)
	case "papers":
		return a.crossref(ctx, query)
	case "arxiv":
		return a.arxiv(ctx, query)
	case "synonyms", "antonyms", "rhymes", "means_like":
		return a.datamuse(ctx, args.Action, query)
	}
	return nil, argumentError("action must be math, papers, arxiv, synonyms, antonyms, rhymes or means_like")
}

// mathjs evaluates an expression on api.mathjs.org. An expression it
// cannot read comes back as an argument error with its own explanation,
// for the model to fix.
func (a *apis) mathjs(ctx context.Context, expression string) (any, error) {
	body, err := json.Marshal(map[string]any{"expr": expression, "precision": mathPrecision})
	if err != nil {
		return nil, err
	}
	var answer struct {
		Result *string `json:"result"`
		Error  string  `json:"error"`
	}
	raw, err := a.patient.get(ctx, call{service: "math.js", url: a.urls.MathJS + "/v4/", body: body})
	var status *statusError
	if errors.As(err, &status) && status.status == http.StatusBadRequest && json.Unmarshal(status.body, &answer) == nil && answer.Error != "" {
		return nil, argumentError("math.js could not compute it: %s", cleanText(answer.Error, 200))
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &answer); err != nil || answer.Result == nil {
		return nil, fmt.Errorf("math.js sent an answer that could not be read")
	}
	return map[string]any{"expression": expression, "result": *answer.Result, "source": "math.js"}, nil
}

type paper struct {
	Title   string   `json:"title"`
	Authors []string `json:"authors,omitempty"`
	Year    int      `json:"year,omitempty"`
	Journal string   `json:"journal,omitempty"`
	Cited   int      `json:"cited_by,omitempty"`
	Summary string   `json:"summary,omitempty"`
	Link    string   `json:"link"`
	PDF     string   `json:"pdf,omitempty"`
}

func (a *apis) crossref(ctx context.Context, query string) (any, error) {
	link := a.urls.Crossref + "/works?rows=5&select=title,author,issued,DOI,container-title,is-referenced-by-count&query=" + url.QueryEscape(query)
	var answer struct {
		Message struct {
			Items []struct {
				Title  []string `json:"title"`
				Author []struct {
					Given  string `json:"given"`
					Family string `json:"family"`
					Name   string `json:"name"`
				} `json:"author"`
				Issued struct {
					DateParts [][]*int `json:"date-parts"`
				} `json:"issued"`
				DOI     string   `json:"DOI"`
				Journal []string `json:"container-title"`
				Cited   int      `json:"is-referenced-by-count"`
			} `json:"items"`
		} `json:"message"`
	}
	if err := a.cachedJSON(ctx, "Crossref", link, studyTTL, &answer); err != nil {
		return nil, err
	}
	var out []paper
	for _, item := range answer.Message.Items[:min(len(answer.Message.Items), maxPapers)] {
		if len(item.Title) == 0 || item.DOI == "" {
			continue
		}
		p := paper{Title: cleanText(item.Title[0], 250), Cited: item.Cited, Link: "https://doi.org/" + item.DOI}
		for _, author := range item.Author {
			if name := cleanText(cmp.Or(strings.TrimSpace(author.Given+" "+author.Family), author.Name), 80); name != "" && len(p.Authors) < 3 {
				p.Authors = append(p.Authors, name)
			}
		}
		if len(item.Issued.DateParts) > 0 && len(item.Issued.DateParts[0]) > 0 && item.Issued.DateParts[0][0] != nil {
			p.Year = *item.Issued.DateParts[0][0]
		}
		if len(item.Journal) > 0 {
			p.Journal = cleanText(item.Journal[0], 150)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, argumentError("Crossref found no paper; try the topic in English with other words")
	}
	return map[string]any{"papers": out, "source": "Crossref", "note": "Cite them as given; never invent a paper."}, nil
}

func (a *apis) arxiv(ctx context.Context, query string) (any, error) {
	var terms []string
	for _, word := range strings.Fields(query) {
		terms = append(terms, "all:"+word)
	}
	link := a.urls.ArXiv + "/api/query?max_results=5&sortBy=relevance&search_query=" + url.QueryEscape(strings.Join(terms, " AND "))
	raw, err := remember(a.cache, "study:"+link, studyTTL, func() ([]byte, error) {
		return a.patient.get(ctx, call{service: "arXiv", url: link, limit: largeBodyLimit})
	})
	if err != nil {
		return nil, err
	}
	var feed struct {
		Entries []struct {
			ID        string `xml:"id"`
			Title     string `xml:"title"`
			Summary   string `xml:"summary"`
			Published string `xml:"published"`
			Authors   []struct {
				Name string `xml:"name"`
			} `xml:"author"`
			Links []struct {
				Href  string `xml:"href,attr"`
				Title string `xml:"title,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(raw, &feed); err != nil {
		return nil, fmt.Errorf("arXiv sent an answer that could not be read")
	}
	var out []paper
	for _, e := range feed.Entries[:min(len(feed.Entries), maxPapers)] {
		p := paper{Title: cleanText(e.Title, 250), Summary: cleanText(e.Summary, 400), Link: strings.Replace(e.ID, "http://", "https://", 1)}
		if len(e.Published) >= 4 {
			fmt.Sscanf(e.Published[:4], "%d", &p.Year)
		}
		for _, author := range e.Authors[:min(len(e.Authors), 3)] {
			p.Authors = append(p.Authors, cleanText(author.Name, 80))
		}
		for _, l := range e.Links {
			if l.Title == "pdf" {
				p.PDF = l.Href
			}
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, argumentError("arXiv found no preprint; try the topic in English with fewer words")
	}
	return map[string]any{"papers": out, "source": "arXiv", "note": "Preprints, not always peer reviewed. Cite them as given."}, nil
}

// datamuseRelations are Datamuse's parameters for each word action.
var datamuseRelations = map[string]string{"synonyms": "rel_syn", "antonyms": "rel_ant", "rhymes": "rel_rhy", "means_like": "ml"}

func (a *apis) datamuse(ctx context.Context, action, word string) (any, error) {
	link := fmt.Sprintf("%s/words?max=%d&%s=%s", a.urls.Datamuse, maxRelatedWord, datamuseRelations[action], url.QueryEscape(strings.ToLower(word)))
	var answer []struct {
		Word string `json:"word"`
	}
	if err := a.cachedJSON(ctx, "Datamuse", link, studyTTL, &answer); err != nil {
		return nil, err
	}
	var words []string
	for _, w := range answer {
		words = append(words, w.Word)
	}
	if len(words) == 0 {
		return nil, argumentError("Datamuse knows no %s of %q", strings.ReplaceAll(action, "_", " "), word)
	}
	return map[string]any{"word": word, action: words, "source": "Datamuse"}, nil
}
