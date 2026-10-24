package webapi

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"regexp"
	"strconv"

	"telegram-ai-bot/internal/tools"
)

// The fun tool gathers small keyless APIs under one schema, so that each
// costs the prompt a word, not a tool: Open Trivia DB, Useless Facts,
// Advice Slip, ZenQuotes, Dog CEO, JokeAPI and the Bored API.

type funArgs struct {
	Action     string `json:"action"`
	Category   string `json:"category"`
	Difficulty string `json:"difficulty"`
}

// triviaCategories are Open Trivia DB's category ids.
var triviaCategories = map[string]int{
	"general": 9, "books": 10, "film": 11, "music": 12, "games": 15, "science": 17, "computers": 18,
	"math": 19, "sports": 21, "geography": 22, "history": 23, "animals": 27,
}

func (a *apis) funTool() tools.Tool {
	return tools.Tool{
		Name: "fun",
		Description: "Light fun: trivia gives a quiz question with its options and answer (category, difficulty optional; ask it and keep the answer until they guess), fact a random fact, advice a piece of advice, quote a famous quote with its author, dog sends a dog picture, joke a clean joke, activity something to do when bored. " +
			"They are in English: say them in the asker's language.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["trivia", "fact", "advice", "quote", "dog", "joke", "activity"]},
				"category": {"type": "string", "enum": ["general", "books", "film", "music", "games", "science", "computers", "math", "sports", "geography", "history", "animals"]},
				"difficulty": {"type": "string", "enum": ["easy", "medium", "hard"]}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		Run: a.fun,
	}
}

func (a *apis) fun(ctx context.Context, raw json.RawMessage) (any, error) {
	var args funArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	switch args.Action {
	case "trivia":
		return a.trivia(ctx, args)
	case "fact":
		return a.uselessFact(ctx)
	case "advice":
		return a.advice(ctx)
	case "quote":
		return a.quote(ctx)
	case "dog":
		return a.dogPicture(ctx)
	case "joke":
		return a.joke(ctx)
	case "activity":
		return a.activity(ctx)
	}
	return nil, argumentError("action must be trivia, fact, advice, quote, dog, joke or activity")
}

func (a *apis) trivia(ctx context.Context, args funArgs) (any, error) {
	link := a.urls.OpenTrivia + "/api.php?amount=1&type=multiple"
	if args.Category != "" {
		id, ok := triviaCategories[args.Category]
		if !ok {
			return nil, argumentError("unknown category %q", args.Category)
		}
		link += "&category=" + strconv.Itoa(id)
	}
	switch args.Difficulty {
	case "":
	case "easy", "medium", "hard":
		link += "&difficulty=" + args.Difficulty
	default:
		return nil, argumentError("difficulty must be easy, medium or hard")
	}
	var answer struct {
		Code    int `json:"response_code"`
		Results []struct {
			Category   string   `json:"category"`
			Difficulty string   `json:"difficulty"`
			Question   string   `json:"question"`
			Correct    string   `json:"correct_answer"`
			Incorrect  []string `json:"incorrect_answers"`
		} `json:"results"`
	}
	if err := a.client.getJSON(ctx, call{service: "Open Trivia DB", url: link}, &answer); err != nil {
		return nil, err
	}
	// Code 5 is Open Trivia DB's rate limit: one question each 5 s per IP.
	if answer.Code == 5 {
		return nil, &busyError{service: "Open Trivia DB"}
	}
	if len(answer.Results) == 0 {
		return nil, argumentError("no question in that category just now; try another")
	}
	q := answer.Results[0]
	options := []string{cleanText(q.Correct, 120)}
	for _, wrong := range q.Incorrect {
		options = append(options, cleanText(wrong, 120))
	}
	rand.Shuffle(len(options), func(i, j int) { options[i], options[j] = options[j], options[i] })
	return map[string]any{
		"question": cleanText(q.Question, 400), "options": options, "answer": cleanText(q.Correct, 120),
		"category": q.Category, "difficulty": q.Difficulty, "source": "Open Trivia DB",
	}, nil
}

// unfitFact finds a fact about sex, drink or drugs, which the bot does not
// tell; Useless Facts has some.
var unfitFact = regexp.MustCompile(`(?i)\b(sex\w*|porn\w*|nud\w*|naked|penis\w*|vagin\w*|orgasm\w*|erotic\w*|condom\w*|prostitut\w*|breast\w*|testic\w*|masturbat\w*|beer|wine|alcohol\w*|drunk\w*|cocaine|heroin|marijuana)\b`)

// factTries is how many facts are asked for before giving up on a fit one.
const factTries = 3

func (a *apis) uselessFact(ctx context.Context) (any, error) {
	for range factTries {
		var answer struct {
			Text string `json:"text"`
		}
		if err := a.client.getJSON(ctx, call{service: "Useless Facts", url: a.urls.UselessFacts + "/api/v2/facts/random?language=en"}, &answer); err != nil {
			return nil, err
		}
		if fact := cleanText(answer.Text, 400); fact != "" && !unfitFact.MatchString(fact) {
			return map[string]any{"fact": fact, "source": "uselessfacts.jsph.pl"}, nil
		}
	}
	return nil, &tools.InternalError{Message: "no fitting fact just now; tell one you know"}
}

func (a *apis) advice(ctx context.Context) (any, error) {
	var answer struct {
		Slip struct {
			Advice string `json:"advice"`
		} `json:"slip"`
	}
	if err := a.client.getJSON(ctx, call{service: "Advice Slip", url: a.urls.AdviceSlip + "/advice"}, &answer); err != nil {
		return nil, err
	}
	return map[string]any{"advice": cleanText(answer.Slip.Advice, 300), "source": "Advice Slip"}, nil
}

func (a *apis) quote(ctx context.Context) (any, error) {
	var answer []struct {
		Quote  string `json:"q"`
		Author string `json:"a"`
	}
	if err := a.client.getJSON(ctx, call{service: "ZenQuotes", url: a.urls.ZenQuotes + "/api/random"}, &answer); err != nil {
		return nil, err
	}
	if len(answer) == 0 || answer[0].Quote == "" {
		return nil, &tools.InternalError{Message: "no quote just now; try again"}
	}
	return map[string]any{"quote": cleanText(answer[0].Quote, 400), "author": cleanText(answer[0].Author, 80), "source": "ZenQuotes"}, nil
}

func (a *apis) dogPicture(ctx context.Context) (any, error) {
	sink, ok := tools.SinkFrom(ctx)
	if !ok {
		return nil, argumentError("no turn to send the picture with")
	}
	var found struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	}
	if err := a.client.getJSON(ctx, call{service: "Dog CEO", url: a.urls.DogCEO + "/api/breeds/image/random"}, &found); err != nil {
		return nil, err
	}
	if found.Status != "success" || found.Message == "" {
		return nil, &tools.InternalError{Message: "no dog picture just now; try again"}
	}
	data, err := a.client.get(ctx, call{service: "Dog CEO", url: found.Message, limit: pictureLimit})
	if err != nil {
		return nil, err
	}
	sink.Add(tools.Drawing{Data: data, MimeType: http.DetectContentType(data)})
	return map[string]any{"sent": true, "note": pictureSentNote}, nil
}

// joke is a clean joke: JokeAPI's safe mode, with every kind that could
// offend in a group left out too.
func (a *apis) joke(ctx context.Context) (any, error) {
	link := a.urls.JokeAPI + "/joke/Misc,Programming,Pun,Spooky?safe-mode&blacklistFlags=nsfw,religious,political,racist,sexist,explicit"
	var answer struct {
		Error    bool   `json:"error"`
		Type     string `json:"type"`
		Joke     string `json:"joke"`
		Setup    string `json:"setup"`
		Delivery string `json:"delivery"`
	}
	if err := a.client.getJSON(ctx, call{service: "JokeAPI", url: link}, &answer); err != nil {
		return nil, err
	}
	if answer.Error {
		return nil, &tools.InternalError{Message: "no joke just now; tell one you know"}
	}
	if answer.Type == "twopart" {
		return map[string]any{"setup": cleanText(answer.Setup, 300), "punchline": cleanText(answer.Delivery, 300),
			"note": "In English: retell it in the asker's language only if it still works there.", "source": "JokeAPI"}, nil
	}
	return map[string]any{"joke": cleanText(answer.Joke, 500),
		"note": "In English: retell it in the asker's language only if it still works there.", "source": "JokeAPI"}, nil
}

// activity is something to do for someone bored, from the Bored API.
func (a *apis) activity(ctx context.Context) (any, error) {
	var answer struct {
		Activity     string `json:"activity"`
		Type         string `json:"type"`
		Participants int    `json:"participants"`
		KidFriendly  bool   `json:"kidFriendly"`
	}
	if err := a.client.getJSON(ctx, call{service: "the Bored API", url: a.urls.BoredAPI + "/random"}, &answer); err != nil {
		return nil, err
	}
	return map[string]any{"activity": cleanText(answer.Activity, 200), "kind": answer.Type, "people": answer.Participants,
		"note": "Suggest it in the asker's language, fitted to their life.", "source": "Bored API"}, nil
}
