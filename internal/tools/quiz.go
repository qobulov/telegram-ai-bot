package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// actionQuiz is how a quiz is recorded in the moderation audit.
const actionQuiz = "quiz"

const (
	maxQuizQuestions = 5
	// maxExplanationRunes is Telegram's limit for a quiz's explanation.
	maxExplanationRunes = 200
	// quizTimeout leaves each question pollTimeout's share.
	quizTimeout = 40 * time.Second

	quizTooSoon   = "a quiz was made or asked for here less than 10 minutes ago"
	quizSentNote  = "The quiz is in the chat now. Don't repeat its questions or give away the answers: say in one short line that it has started."
	quizAskedNote = "This message already asked for a quiz, so no other was made; a group gets one quiz every 10 minutes. Don't say whether the first worked."
)

type quizArgs struct {
	Questions []quizQuestion `json:"questions"`
}

type quizQuestion struct {
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	Correct     int      `json:"correct"`
	Explanation string   `json:"explanation"`
}

// QuizTool lets anyone in a group have the bot post a quiz there: up to
// five questions, each with one right answer that Telegram reveals to whoever
// answers. Like a poll, it goes to the group the turn is in and is paced.
func QuizTool(opts PollOptions) Tool {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	p := polls{opts}
	return Tool{
		Name:        "create_quiz",
		Description: "Post a quiz in this group when someone asks for a quiz, viktorina or test: 1 to 5 questions, each with one right answer, which Telegram shows to whoever answers. Write the questions yourself, on the topic asked, with facts you are sure of. A group gets one quiz every 10 minutes.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"questions": {"type": "array", "minItems": 1, "maxItems": 5, "items": {
					"type": "object",
					"properties": {
						"question": {"type": "string", "description": "1 to 300 characters, in the language of the person asking."},
						"options": {"type": "array", "items": {"type": "string"}, "minItems": 2, "maxItems": 10, "description": "2 to 10 different answers, each 1 to 100 characters."},
						"correct": {"type": "integer", "minimum": 0, "description": "The index of the right answer in options, from 0."},
						"explanation": {"type": "string", "description": "Why it is right, at most 200 characters, shown after answering."}
					},
					"required": ["question", "options", "correct"],
					"additionalProperties": false
				}}
			},
			"required": ["questions"],
			"additionalProperties": false
		}`),
		GroupOnly: true,
		Timeout:   quizTimeout,
		Run:       p.quiz,
	}
}

// quiz records the quiz, then posts its questions in order, the way run
// posts a poll. A quiz whose first question surely did not go out is
// forgotten; one cut off later says how many went.
func (p polls) quiz(ctx context.Context, raw json.RawMessage) (any, error) {
	var args quizArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, err := pollAsker(ctx)
	if err != nil {
		return nil, err
	}
	questions, err := newQuiz(caller, args)
	if err != nil {
		return nil, err
	}
	first, err := p.Store.RecordModeration(ctx, storage.ModerationAction{
		ChatID: caller.ChatID, Action: actionQuiz, RequesterID: caller.UserID, UpdateID: caller.UpdateID,
	})
	if err != nil {
		return nil, internal(err)
	}
	if !first {
		return map[string]any{"already_handled": true, "action": actionQuiz, "note": quizAskedNote}, nil
	}
	if err := p.checkPace(ctx, caller.ChatID, actionQuiz); err != nil {
		if _, tooSoon := err.(*ArgumentError); tooSoon {
			err = &ArgumentError{Message: quizTooSoon}
		}
		return nil, p.forget(ctx, caller, actionQuiz, err)
	}
	for i, question := range questions {
		if _, err := p.Sender.SendPoll(ctx, question); err != nil {
			if i == 0 {
				return nil, p.notSent(ctx, caller, actionQuiz, err)
			}
			return map[string]any{"sent": i, "of": len(questions), "note": fmt.Sprintf("Only the first %d questions reached the chat; Telegram refused the rest.", i)}, nil
		}
	}
	return map[string]any{"sent": len(questions), "note": quizSentNote}, nil
}

// newQuiz is the quiz asked for, trimmed and within Telegram's limits. The
// first question replies to the request, which keeps a forum's quiz in the
// request's topic; the rest follow it.
func newQuiz(caller Caller, args quizArgs) ([]telegram.OutgoingPoll, error) {
	if len(args.Questions) < 1 || len(args.Questions) > maxQuizQuestions {
		return nil, &ArgumentError{Message: fmt.Sprintf("give 1 to %d questions", maxQuizQuestions)}
	}
	quiz := make([]telegram.OutgoingPoll, 0, len(args.Questions))
	for i, q := range args.Questions {
		question := strings.TrimSpace(q.Question)
		if !runesWithin(question, maxPollQuestionRunes) {
			return nil, &ArgumentError{Message: fmt.Sprintf("question %d must be 1 to %d characters", i+1, maxPollQuestionRunes)}
		}
		options, err := checkedOptions(q.Options)
		if err != nil {
			return nil, err
		}
		if q.Correct < 0 || q.Correct >= len(options) {
			return nil, &ArgumentError{Message: fmt.Sprintf("correct in question %d must be the index of one of its options, 0 to %d", i+1, len(options)-1)}
		}
		explanation := strings.TrimSpace(q.Explanation)
		if utf8.RuneCountInString(explanation) > maxExplanationRunes {
			return nil, &ArgumentError{Message: fmt.Sprintf("an explanation must be at most %d characters", maxExplanationRunes)}
		}
		poll := telegram.OutgoingPoll{ChatID: caller.ChatID, Question: question, Options: options, Quiz: true, Correct: q.Correct, Explanation: explanation}
		if i == 0 {
			poll.ReplyToMessageID = caller.MessageID
		}
		quiz = append(quiz, poll)
	}
	return quiz, nil
}
