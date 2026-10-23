package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/slides"
)

const (
	minContentSlides = 2
	maxCaptionRunes  = 200
)

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
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Theme    string `json:"theme"`
	Caption  string `json:"caption"`
	Slides   []struct {
		Title   string   `json:"title"`
		Bullets []string `json:"bullets"`
	} `json:"slides"`
}

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
		Name: "make_presentation",
		Description: "Make a PowerPoint presentation (.pptx) and send it as your whole answer, when someone asks for a presentation, slides or taqdimot. " +
			"Write it in the asker's language: a title slide (title, subtitle such as their name or subject if given), then 2 to 12 content slides of a title and 2 to 6 short, factual bullet points. " +
			"caption is your one-line message with the file.",
		Schema: json.RawMessage(fmt.Sprintf(`{
			"type": "object",
			"properties": {
				"title": {"type": "string"},
				"subtitle": {"type": "string"},
				"theme": {"type": "string", "enum": ["%s"]},
				"caption": {"type": "string", "description": "One short line sent with the file, in the asker's language."},
				"slides": {"type": "array", "items": {
					"type": "object",
					"properties": {
						"title": {"type": "string"},
						"bullets": {"type": "array", "items": {"type": "string"}}
					},
					"required": ["title", "bullets"],
					"additionalProperties": false
				}}
			},
			"required": ["title", "slides", "caption"],
			"additionalProperties": false
		}`, strings.Join(slides.Themes, `", "`))),
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
	deck := slides.Deck{Title: strings.TrimSpace(args.Title), Subtitle: strings.TrimSpace(args.Subtitle), Theme: args.Theme}
	if deck.Title == "" || utf8.RuneCountInString(deck.Title) > slides.MaxTitleRunes || utf8.RuneCountInString(deck.Subtitle) > slides.MaxTitleRunes {
		return deck, &ArgumentError{Message: fmt.Sprintf("title and subtitle must be at most %d characters, and title not empty", slides.MaxTitleRunes)}
	}
	if len(args.Slides) < minContentSlides || len(args.Slides) > slides.MaxSlides {
		return deck, &ArgumentError{Message: fmt.Sprintf("give %d to %d content slides", minContentSlides, slides.MaxSlides)}
	}
	for i, s := range args.Slides {
		slide := slides.Slide{Title: strings.TrimSpace(s.Title)}
		if slide.Title == "" || utf8.RuneCountInString(slide.Title) > slides.MaxTitleRunes {
			return deck, &ArgumentError{Message: fmt.Sprintf("slide %d: its title must be 1 to %d characters", i+1, slides.MaxTitleRunes)}
		}
		for _, bullet := range s.Bullets {
			if bullet = strings.Join(strings.Fields(bullet), " "); bullet != "" {
				slide.Bullets = append(slide.Bullets, bullet)
			}
		}
		if len(slide.Bullets) == 0 || len(slide.Bullets) > slides.MaxBullets {
			return deck, &ArgumentError{Message: fmt.Sprintf("slide %d: give 1 to %d bullet points", i+1, slides.MaxBullets)}
		}
		for _, bullet := range slide.Bullets {
			if utf8.RuneCountInString(bullet) > slides.MaxBulletRunes {
				return deck, &ArgumentError{Message: fmt.Sprintf("slide %d: a bullet point is over %d characters; shorten it", i+1, slides.MaxBulletRunes)}
			}
		}
		deck.Slides = append(deck.Slides, slide)
	}
	return deck, nil
}
