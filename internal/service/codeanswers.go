package service

import (
	"context"
	"encoding/json"

	"telegram-ai-bot/internal/tools"
)

// The menus' requests are answered in code where they can be: the tool is
// run on the sender's turn and its result written here, without the
// model, which is faster and costs nothing. A request without a code
// answer still goes to the model, with the tool, outside the day's limit.

// codeAnswer answers a menu's request in code; value is what the user
// gave, a hero, a city or a league, or "" for none.
type codeAnswer func(ctx context.Context, s *Service, in incoming, value string) outcome

// codeAnswers are the menus' requests answered in code, by their key: an
// item of «📋 Ko'proq» or a choice of the Mobile Legends menu. Each file
// that writes some adds them in its init.
var codeAnswers = map[string]codeAnswer{}

// toolResult runs a tool on the sender's turn with args and decodes its
// result into out, and gives the pictures and songs it made; ok is false
// when it failed.
func (s *Service) toolResult(ctx context.Context, in incoming, name string, args, out any) (pictures []tools.Drawing, audios []tools.Audio, ok bool) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, nil, false
	}
	sink := &tools.Sink{}
	if !s.runToolInto(ctx, in, name, string(encoded), out, sink) {
		return nil, nil, false
	}
	return sink.Take(), sink.TakeAudios(), true
}

// pictureAnswer is an answer of pictures, with text as the caption.
func pictureAnswer(text string, pictures []tools.Drawing) outcome {
	return outcome{text: text, drawings: pictures}
}
