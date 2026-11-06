package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/tools"
)

const (
	// memeTemplatesTTL keeps Imgflip's hundred popular templates, which
	// change slowly.
	memeTemplatesTTL = 24 * time.Hour
	maxMemeTextRunes = 100
	// memeFont has Cyrillic letters: Imgflip's default, Impact, drops them.
	memeFont = "Noto Sans"
	// memeTemplatesListed is how many template names a refusal offers.
	memeTemplatesListed = 40
	// memeRecentKept is how many of a chat's latest templates are not used
	// again there unless the asker names one: the model, left to itself,
	// drew Drake ten times in nineteen memes.
	memeRecentKept = 4
	// memeRecentTTL is how long a chat's latest templates are remembered.
	memeRecentTTL = 7 * 24 * time.Hour
)

type memeArgs struct {
	Template string   `json:"template"`
	Texts    []string `json:"texts"`
}

type memeTemplate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BoxCount int    `json:"box_count"`
}

func (a *apis) memeTool() tools.Tool {
	return tools.Tool{
		Name: "make_meme",
		Description: "Make a meme from one of Imgflip's hundred popular templates and send it as a picture. Use it when someone asks for a meme (\"mem qil\", \"мем сделай\"). " +
			"Pick the template that fits this joke best, from the whole range, not the same few: a template used in this chat's last few memes is refused unless the asker named it, with others offered. " +
			"texts go in the template's boxes in Imgflip's order, mostly top to bottom. Some, with their boxes: Drake Hotline Bling: rejected, preferred; Distracted Boyfriend: the other woman, the boyfriend, the girlfriend; " +
			"Two Buttons: the two buttons, the one sweating; Expanding Brain: four ideas, each \"smarter\"; Gru's Plan: four steps, the last one the bad surprise; Left Exit 12 Off Ramp: the straight road, the exit, the car; " +
			"Batman Slapping Robin: Robin's line, Batman's; Woman Yelling At Cat: her accusation, the cat's reply; UNO Draw 25 Cards: the card's demand, the one who draws 25; Is This A Pigeon: the man, the butterfly, the wrong guess; " +
			"Tuxedo Winnie The Pooh: the plain way, the fancy way; Epic Handshake: one side, the other, what they share; Change My Mind: the claim; Mocking Spongebob: the line mocked, then mocked; " +
			"Running Away Balloon, Bike Fall, Panik Kalm Panik, Anakin Padme 4 Panel, They're The Same Picture, Hide the Pain Harold, Sad Pablo Escobar, Waiting Skeleton, Disaster Girl, Monkey Puppet, Always Has Been, Roll Safe Think About It, One Does Not Simply, Buff Doge vs. Cheems and more. " +
			"Write the texts in the asker's language and script, short.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"template": {"type": "string", "description": "The template's English name or part of it, as \"Drake\", \"Distracted Boyfriend\" or \"Two Buttons\"."},
				"texts": {"type": "array", "items": {"type": "string"}, "minItems": 1, "maxItems": 5, "description": "One text per box, in the template's order."}
			},
			"required": ["template", "texts"],
			"additionalProperties": false
		}`),
		Run: a.makeMeme,
	}
}

func (a *apis) makeMeme(ctx context.Context, raw json.RawMessage) (any, error) {
	var args memeArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	for i, text := range args.Texts {
		args.Texts[i] = strings.TrimSpace(text)
		if utf8.RuneCountInString(args.Texts[i]) > maxMemeTextRunes {
			return nil, argumentError("each text must be at most %d characters", maxMemeTextRunes)
		}
	}
	sink, ok := tools.SinkFrom(ctx)
	if !ok {
		return nil, argumentError("no turn to send the picture with")
	}
	templates, err := a.memeTemplates(ctx)
	if err != nil {
		return nil, err
	}
	template, ok := findMemeTemplate(templates, args.Template)
	if !ok {
		return nil, argumentError("Imgflip has no popular template called %q; pick one of: %s", args.Template, memeTemplateNames(templates))
	}
	if len(args.Texts) == 0 || len(args.Texts) > template.BoxCount {
		return nil, argumentError("%s takes 1 to %d texts", template.Name, template.BoxCount)
	}
	caller, _ := tools.CallerFrom(ctx)
	recent := a.recentMemes(caller.ChatID)
	if slices.Contains(recent, template.ID) && !namesTemplate(caller.Words, template) {
		return nil, argumentError("%s was used in one of this chat's last memes; pick another that fits, such as: %s", template.Name, freshMemeTemplates(templates, recent))
	}
	link, err := a.captionMeme(ctx, template, args.Texts)
	if err != nil {
		return nil, err
	}
	// The picture is fetched only from Imgflip, whatever the answer says.
	if !strings.HasPrefix(link, "https://i.imgflip.com/") && !strings.HasPrefix(link, a.urls.Imgflip+"/") {
		return nil, fmt.Errorf("Imgflip sent the meme from an unknown place")
	}
	data, err := a.client.get(ctx, call{service: "Imgflip", url: link, limit: pictureLimit})
	if err != nil {
		return nil, err
	}
	sink.Add(tools.Drawing{Data: data, MimeType: http.DetectContentType(data)})
	a.rememberMeme(caller.ChatID, recent, template.ID)
	return map[string]any{"sent": true, "template": template.Name, "note": pictureSentNote}, nil
}

// memeTemplates are Imgflip's hundred most captioned templates.
func (a *apis) memeTemplates(ctx context.Context) ([]memeTemplate, error) {
	return remember(a.cache, "imgflip:templates", memeTemplatesTTL, func() ([]memeTemplate, error) {
		var answer struct {
			Success bool `json:"success"`
			Data    struct {
				Memes []memeTemplate `json:"memes"`
			} `json:"data"`
		}
		if err := a.client.getJSON(ctx, call{service: "Imgflip", url: a.urls.Imgflip + "/get_memes"}, &answer); err != nil {
			return nil, err
		}
		if !answer.Success || len(answer.Data.Memes) == 0 {
			return nil, fmt.Errorf("Imgflip sent no templates")
		}
		return answer.Data.Memes, nil
	})
}

// findMemeTemplate is the template whose name is name, or else the first
// whose name holds it, in any case.
func findMemeTemplate(templates []memeTemplate, name string) (memeTemplate, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return memeTemplate{}, false
	}
	if i := slices.IndexFunc(templates, func(t memeTemplate) bool { return strings.ToLower(t.Name) == name }); i >= 0 {
		return templates[i], true
	}
	i := slices.IndexFunc(templates, func(t memeTemplate) bool { return strings.Contains(strings.ToLower(t.Name), name) })
	if i < 0 {
		return memeTemplate{}, false
	}
	return templates[i], true
}

func memeTemplateNames(templates []memeTemplate) string {
	names := make([]string, 0, memeTemplatesListed)
	for _, t := range templates[:min(len(templates), memeTemplatesListed)] {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

// recentMemes are the IDs of the templates of a chat's latest memes, the
// latest last, as this instance remembers them.
func (a *apis) recentMemes(chatID int64) []string {
	recent, _ := a.cache.get(memeRecentKey(chatID))
	ids, _ := recent.([]string)
	return ids
}

// rememberMeme adds id to a chat's latest templates. The cached slice is
// shared, so a new one is stored.
func (a *apis) rememberMeme(chatID int64, recent []string, id string) {
	latest := append(slices.Clone(recent), id)
	a.cache.put(memeRecentKey(chatID), latest[max(0, len(latest)-memeRecentKept):], memeRecentTTL)
}

func memeRecentKey(chatID int64) string { return fmt.Sprintf("imgflip:recent:%d", chatID) }

// namesTemplate reports whether the asker's own words name template, by its
// name or its first word, such as "drake": then it is what they want.
func namesTemplate(words string, template memeTemplate) bool {
	words = strings.ToLower(words)
	name := strings.ToLower(template.Name)
	first, _, _ := strings.Cut(name, " ")
	return strings.Contains(words, name) || (utf8.RuneCountInString(first) >= 4 && strings.Contains(words, first))
}

// freshMemeTemplates names the popular templates not among recent, from a
// random place in the list, so that a refusal does not always offer the
// same few, with how many texts each takes.
func freshMemeTemplates(templates []memeTemplate, recent []string) string {
	var names []string
	start := rand.IntN(len(templates))
	for i := range templates {
		t := templates[(start+i)%len(templates)]
		if !slices.Contains(recent, t.ID) {
			names = append(names, fmt.Sprintf("%s (%d)", t.Name, t.BoxCount))
		}
		if len(names) == memeTemplatesListed/2 {
			break
		}
	}
	return strings.Join(names, ", ")
}

// captionMeme asks Imgflip to write texts on template and returns the
// picture's link. The key goes in a header, never in the URL.
func (a *apis) captionMeme(ctx context.Context, template memeTemplate, texts []string) (string, error) {
	form := url.Values{"template_id": {template.ID}, "font": {memeFont}}
	for i, text := range texts {
		form.Set(fmt.Sprintf("boxes[%d][text]", i), text)
	}
	var answer struct {
		Success bool   `json:"success"`
		Error   string `json:"error_message"`
		Data    struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	err := a.client.getJSON(ctx, call{
		service: "Imgflip", url: a.urls.Imgflip + "/caption_image",
		header:      http.Header{"Authorization": {"Bearer " + a.imgflipKey}},
		body:        []byte(form.Encode()),
		contentType: "application/x-www-form-urlencoded",
	}, &answer)
	if err != nil {
		return "", err
	}
	if !answer.Success || answer.Data.URL == "" {
		return "", fmt.Errorf("Imgflip made no meme: %s", answer.Error)
	}
	return answer.Data.URL, nil
}
