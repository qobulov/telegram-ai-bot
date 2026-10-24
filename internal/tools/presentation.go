package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/slides"
)

const (
	// A deck has at least ten slides with its title slide, as students'
	// presentations do.
	minContentSlides = 9
	maxCaptionRunes  = 200
	maxEyebrowRunes  = 50
	maxNoteRunes     = 150
	maxIconRunes     = 8
)

// itemsPerLayout are how many items each layout takes.
var itemsPerLayout = map[string][2]int{
	slides.LayoutPoints:    {2, 5},
	slides.LayoutCards:     {2, 4},
	slides.LayoutSteps:     {3, 4},
	slides.LayoutGrid:      {4, 6},
	slides.LayoutTable:     {2, 6},
	slides.LayoutStatement: {0, 1},
	slides.LayoutNumbers:   {2, 4},
	slides.LayoutSplit:     {2, 4},
}

// maxFigureRunes bounds a key figure, such as "1 200+" or "87%".
const maxFigureRunes = 14

// PresentationQuota counts presentations per user per day.
type PresentationQuota interface {
	ReservePresentation(ctx context.Context, userID, chatID int64, since time.Time, limit int) (int64, bool, error)
	ReleasePresentation(ctx context.Context, id int64) error
}

// PresentationOptions configures the make_presentation tool.
type PresentationOptions struct {
	Quota PresentationQuota
	// DailyLimit is how many presentations a user who is not an owner may
	// get per day.
	DailyLimit int
	// Location decides when a day starts.
	Location *time.Location
	Now      func() time.Time
}

type presentationArgs struct {
	Title    string      `json:"title"`
	Subtitle string      `json:"subtitle"`
	Author   string      `json:"author"`
	Theme    string      `json:"theme"`
	Caption  string      `json:"caption"`
	Slides   []slideArgs `json:"slides"`
}

type slideArgs struct {
	Layout  string     `json:"layout"`
	Eyebrow string     `json:"eyebrow"`
	Title   string     `json:"title"`
	Intro   string     `json:"intro"`
	Icon    string     `json:"icon"`
	Columns []string   `json:"columns"`
	Note    string     `json:"note"`
	Items   []itemArgs `json:"items"`
}

type itemArgs struct {
	Icon  string `json:"icon"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// presentationGuide is how to write a deck that looks made by a designer,
// after Anthropic's pptx skill: a theme chosen for the topic, layouts that
// vary, and key figures shown large.
const presentationGuide = "Make a PowerPoint presentation (.pptx), sent as your whole answer, when someone asks for a presentation, slides or taqdimot. " +
	"Write it in the asker's language and script, with real, accurate content: title slide, then 9 to 14 slides: an overview, the topic's parts, a conclusion. " +
	"Pick the theme for the topic's mood (nature, health: forest-canopy, botanical-garden; tech: tech-innovation, midnight-galaxy; history, culture: desert-rose, golden-hour, sunset-boulevard; science, business: ocean-depths, arctic-frost, modern-minimalist). " +
	"Vary layouts, never the same twice in a row: points (2-5 bold terms with a sentence each), split (a section's title on a color block, 2-4 points), cards (2-4), steps (3-4, a sequence), grid (4-6 short tiles with an emoji), table (rows of term and explanation), numbers (2-4 real figures you know to be true, such as years, counts or official statistics, the figure as title and its source in note), statement (one key idea, in intro). " +
	"Never invent a figure, percentage or result: a teacher will ask where it came from; take a country's figures from study statistics first, and without real figures use another layout. " +
	"Give most slides an eyebrow (a short section label such as \"YO'NALISH 01 · DIAGNOSTIKA\", never the title again) and an intro sentence. caption is your one-line message with the file."

// PresentationTool lets the model make a PowerPoint file, which is sent as
// the whole answer with the caption the model wrote: the turn ends there,
// so writing the slides is the only call to the model. Owners have no
// limit; everyone else gets DailyLimit a day.
func PresentationTool(opts PresentationOptions) Tool {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	return Tool{
		Name:        "make_presentation",
		Description: presentationGuide,
		Schema: json.RawMessage(fmt.Sprintf(`{
			"type": "object",
			"properties": {
				"title": {"type": "string"},
				"subtitle": {"type": "string", "description": "The topic's context, one line."},
				"author": {"type": "string", "description": "Who made it, if they said, as \"Bajardi: 23-05 guruh talabasi Ali Valiyev\"."},
				"theme": {"type": "string", "enum": ["%s"]},
				"caption": {"type": "string", "description": "One short line sent with the file, in the asker's language."},
				"slides": {"type": "array", "minItems": %d, "maxItems": %d, "items": {
					"type": "object",
					"properties": {
						"layout": {"type": "string", "enum": ["%s"]},
						"eyebrow": {"type": "string"},
						"title": {"type": "string"},
						"intro": {"type": "string", "description": "A sentence under the title; for a statement, the statement itself."},
						"icon": {"type": "string", "description": "For points: one emoji drawn large beside them."},
						"columns": {"type": "array", "items": {"type": "string"}, "description": "For a table: its two headings."},
						"note": {"type": "string", "description": "A short line at the foot, if needed."},
						"items": {"type": "array", "items": {
							"type": "object",
							"properties": {
								"icon": {"type": "string", "description": "One emoji, for grid tiles and cards."},
								"title": {"type": "string", "description": "A short term or heading."},
								"text": {"type": "string", "description": "One or two sentences."}
							},
							"additionalProperties": false
						}}
					},
					"required": ["layout", "title"],
					"additionalProperties": false
				}}
			},
			"required": ["title", "slides", "caption"],
			"additionalProperties": false
		}`, strings.Join(slides.Themes, `", "`), minContentSlides, slides.MaxSlides-1, strings.Join(slides.Layouts, `", "`))),
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return makePresentation(ctx, opts, raw)
		},
	}
}

func makePresentation(ctx context.Context, opts PresentationOptions, raw json.RawMessage) (any, error) {
	var args presentationArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	deck, err := deckOf(args)
	if err != nil {
		return nil, err
	}
	caption := strings.TrimSpace(args.Caption)
	if caption == "" || utf8.RuneCountInString(caption) > maxCaptionRunes {
		return nil, &ArgumentError{Message: fmt.Sprintf("caption must be one line of 1 to %d characters", maxCaptionRunes)}
	}
	t, ok := ctx.Value(turnKey{}).(turn)
	if !ok || t.sink == nil {
		return nil, errors.New("files cannot be sent from here")
	}
	reservation, err := reserveToday(ctx, t.caller, daily{limit: opts.DailyLimit, location: opts.Location, now: opts.Now, what: "presentations"}, opts.Quota.ReservePresentation)
	if err != nil {
		return nil, err
	}
	data, err := slides.Build(deck)
	if err != nil {
		_ = opts.Quota.ReleasePresentation(context.WithoutCancel(ctx), reservation)
		return nil, internal(err)
	}
	t.sink.AddDocument(Document{Data: data, Name: slides.FileName(deck.Title)})
	t.sink.Finish(caption)
	return map[string]any{"sent": true, "slides": len(deck.Slides) + 1}, nil
}

// deckOf checks the model's slides against the limits, so that every slide
// fits; an error says what to change, for the model to try again.
func deckOf(args presentationArgs) (slides.Deck, error) {
	deck := slides.Deck{Title: clean(args.Title), Subtitle: clean(args.Subtitle), Author: clean(args.Author), Theme: args.Theme}
	if deck.Title == "" || tooLong(deck.Title, slides.MaxTitleRunes) || tooLong(deck.Subtitle, slides.MaxIntroRunes) || tooLong(deck.Author, slides.MaxTitleRunes) {
		return deck, &ArgumentError{Message: fmt.Sprintf("title must be 1 to %d characters, subtitle and author short", slides.MaxTitleRunes)}
	}
	if len(args.Slides) < minContentSlides || len(args.Slides) > slides.MaxSlides-1 {
		return deck, &ArgumentError{Message: fmt.Sprintf("give %d to %d slides after the title slide", minContentSlides, slides.MaxSlides-1)}
	}
	for i, s := range args.Slides {
		slide, err := slideOf(s)
		if err != nil {
			return deck, &ArgumentError{Message: fmt.Sprintf("slide %d (%s): %s", i+1, s.Layout, err)}
		}
		deck.Slides = append(deck.Slides, slide)
	}
	return deck, nil
}

// slideOf is a slide as the model wrote it, made to fit: text a little too
// long is cut at a word, as asking the model again would take as long
// again. Only a slide that cannot be laid out is an error.
func slideOf(s slideArgs) (slides.Slide, error) {
	slide := slides.Slide{Layout: s.Layout, Eyebrow: cut(s.Eyebrow, maxEyebrowRunes), Title: cut(s.Title, slides.MaxTitleRunes),
		Intro: cut(s.Intro, slides.MaxIntroRunes), Note: cut(s.Note, maxNoteRunes)}
	if icon := clean(s.Icon); !tooLong(icon, maxIconRunes) {
		slide.Icon = icon
	}
	limits, ok := itemsPerLayout[s.Layout]
	switch {
	case !ok:
		return slide, fmt.Errorf("layout must be one of %s", strings.Join(slides.Layouts, ", "))
	case slide.Title == "":
		return slide, errors.New("it needs a title")
	case s.Layout == slides.LayoutStatement && slide.Intro == "":
		return slide, errors.New("a statement needs its statement in intro")
	}
	for j, heading := range s.Columns {
		if j < 2 {
			slide.Columns[j] = clean(heading)
		}
	}
	for _, it := range s.Items {
		item := slides.Item{Title: cut(it.Title, slides.MaxItemTitle), Text: cut(it.Text, slides.MaxItemText)}
		if icon := clean(it.Icon); !tooLong(icon, maxIconRunes) {
			item.Icon = icon
		}
		if s.Layout == slides.LayoutSteps {
			// Steps are numbered on the slide; a number in the title too
			// would show twice.
			item.Title = stepNumber.ReplaceAllString(item.Title, "")
		}
		if item.Title == "" && item.Text == "" {
			continue
		}
		if s.Layout == slides.LayoutTable && (item.Title == "" || item.Text == "") {
			return slide, errors.New("each table row needs a title and a text")
		}
		if s.Layout == slides.LayoutNumbers && (item.Title == "" || tooLong(item.Title, maxFigureRunes)) {
			// Not a figure that shows large: the slide becomes points.
			slide.Layout = slides.LayoutPoints
		}
		slide.Items = append(slide.Items, item)
	}
	if n := len(slide.Items); n < limits[0] || n > limits[1] {
		return slide, fmt.Errorf("this layout takes %d to %d items, not %d", limits[0], limits[1], n)
	}
	return slide, nil
}

// stepNumber is a step's own number in front of its title, as "1. ",
// "2)" or "3-bosqich:".
var stepNumber = regexp.MustCompile(`(?i)^(?:\d{1,2}\s*[.):]\s+|\d{1,2}\s*-?\s*(?:bosqich|qadam|step|этап|шаг)\w*\s*[.):]?\s*)`)

// cut is text on one line, cut at a word to at most limit characters.
func cut(text string, limit int) string {
	text = clean(text)
	if !tooLong(text, limit) {
		return text
	}
	runes := []rune(text)[:limit-1]
	if space := strings.LastIndex(string(runes), " "); space > len(string(runes))/2 {
		return strings.TrimRight(string(runes)[:space], " ,;:.") + "…"
	}
	return string(runes) + "…"
}

// clean is text on one line, its spaces collapsed.
func clean(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func tooLong(text string, limit int) bool {
	return utf8.RuneCountInString(text) > limit
}
