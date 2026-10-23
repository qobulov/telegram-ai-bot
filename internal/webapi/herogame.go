package webapi

import (
	"cmp"
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"slices"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/tools"
)

const (
	// heroRound is how long a round of "guess the hero" lasts.
	heroRound = 3 * time.Minute
	// heroTries is how many heroes are tried for one with a picture.
	heroTries = 4
	// heroTop is how many players the scoreboard lists.
	heroTop = 10
)

// HeroGames keeps the rounds and the points of "guess the hero". The answer
// is kept there and checked by the service, never given to the model.
type HeroGames interface {
	StartHeroGame(ctx context.Context, chatID int64, hero string, endsAt time.Time) (string, error)
	HeroScores(ctx context.Context, chatID int64, limit int) ([]storage.HeroScore, error)
}

type heroQuizArgs struct {
	Action string `json:"action"`
}

const (
	heroStartNote = "A Mobile Legends hero's picture is sent with your answer, which becomes its caption. Ask in one short line which hero this is: the first right name written in the chat within 3 minutes wins a point, which you check yourself without seeing the answer. You do not know the hero: never guess, name or hint it, and do not answer guesses."
	heroTopNote   = "The group's best players at guessing heroes, most points first. List them briefly."
)

func (a *apis) heroQuizTool() tools.Tool {
	return tools.Tool{
		Name: "hero_quiz",
		Description: "The \"guess the hero\" game, in a group. start: send a random Mobile Legends hero's picture for the group to name; the first right answer in the chat wins a point, checked in code. " +
			"Use it when someone asks to play, start a game or a quiz about heroes. top: the group's scoreboard.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {"action": {"type": "string", "enum": ["start", "top"]}},
			"required": ["action"],
			"additionalProperties": false
		}`),
		GroupOnly: true,
		// A list, a hero's page and its picture, each up to patientTimeout.
		Timeout: 3 * patientTimeout,
		Run:     a.heroQuiz,
	}
}

func (a *apis) heroQuiz(ctx context.Context, raw json.RawMessage) (any, error) {
	var args heroQuizArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := tools.CallerFrom(ctx)
	if !ok || caller.ChatID >= 0 {
		return nil, argumentError("the game is played in a group")
	}
	switch args.Action {
	case "start":
		return a.startHeroRound(ctx, caller.ChatID)
	case "top":
		scores, err := a.heroGames.HeroScores(ctx, caller.ChatID, heroTop)
		if err != nil {
			return nil, err
		}
		return map[string]any{"scores": scores, "note": heroTopNote}, nil
	}
	return nil, argumentError("action must be start or top")
}

// startHeroRound picks a hero with a picture, puts the picture in the turn's
// sink and starts the round with that hero as its answer.
func (a *apis) startHeroRound(ctx context.Context, chatID int64) (any, error) {
	sink, ok := tools.SinkFrom(ctx)
	if !ok {
		return nil, argumentError("no turn to send the picture with")
	}
	names, err := a.mlbbHeroes(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for range heroTries {
		id := ids[rand.IntN(len(ids))]
		detail, err := a.arenaDetail(ctx, id)
		if err != nil {
			continue
		}
		link := cmp.Or(detail.Hero.Data.Picture, detail.Hero.Data.Card)
		if link == "" {
			continue
		}
		data, err := a.patient.get(ctx, call{service: arenaService, url: link, limit: heroPictureLimit})
		if err != nil {
			continue
		}
		previous, err := a.heroGames.StartHeroGame(ctx, chatID, names[id], a.now().Add(heroRound))
		if err != nil {
			return nil, err
		}
		sink.Add(tools.Drawing{Data: data, MimeType: http.DetectContentType(data)})
		out := map[string]any{"started": true, "seconds": int(heroRound.Seconds()), "note": heroStartNote}
		if previous != "" {
			out["previous_answer"] = previous
			out["note"] = heroStartNote + " Say in a few words that nobody guessed the last one, which was previous_answer."
		}
		return out, nil
	}
	return nil, &tools.InternalError{Message: "no hero picture could be fetched just now; try again later"}
}
