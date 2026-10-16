package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// actionPoll is how a poll is recorded in the moderation audit.
const actionPoll = "poll"

const (
	// Telegram's limits for a poll.
	maxPollQuestionRunes = 300
	maxPollOptionRunes   = 100
	minPollOptions       = 2
	maxPollOptions       = 10
	// A group gets one poll per pollWindow, so that asking for polls cannot
	// flood it.
	pollWindow = 10 * time.Minute
	// pollTimeout is above the registry's 5 s default, which could cut off a
	// slow sendPoll and leave unknown whether the poll was posted.
	pollTimeout = 15 * time.Second
)

const (
	notInAGroupForPoll = "a poll can only be made in a group"
	// pollTooSoon says "or asked for" because a poll counts from its record,
	// made before it is sent: the poll counted may still be on its way, or
	// have gone unanswered.
	pollTooSoon  = "a poll was made or asked for here less than 10 minutes ago"
	pollNoAnswer = "Telegram did not answer, so the poll may be in the chat or not; to keep it from showing twice, this group gets no other poll for 10 minutes"
	pollSentNote = "The poll is in the chat now. Don't repeat its question or options; reply in one short line at most."
	// pollAskedNote answers a second create_poll in one update: Telegram
	// delivered the update again, or the model asked for two polls. The
	// first may still be on its way, so this says nothing of how it went.
	pollAskedNote = "This message already asked for a poll, so no other was made; a group gets one poll every 10 minutes. Don't say whether the first worked."
)

// PollSender posts a poll in a chat.
type PollSender interface {
	SendPoll(ctx context.Context, poll telegram.OutgoingPoll) (int64, error)
}

// PollCounter keeps the polls the bot posted, or may have posted, in the
// moderation audit, which tells how recently a group got one and whether an
// update already asked.
type PollCounter interface {
	RecordModeration(ctx context.Context, a storage.ModerationAction) (bool, error)
	ForgetModeration(ctx context.Context, updateID int64, action string) error
	ChatModeration(ctx context.Context, chatID int64, action string, since time.Time) (int, error)
}

// PollOptions configures the create_poll tool. Sender and Store are
// required.
type PollOptions struct {
	Sender PollSender
	Store  PollCounter
	Now    func() time.Time
}

type pollArgs struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Multiple bool     `json:"multiple"`
}

// PollTool lets anyone in a group have the bot post an anonymous poll there.
// The poll goes to the group the turn is in, never one the model names.
func PollTool(opts PollOptions) Tool {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	p := polls{opts}
	return Tool{
		Name: "create_poll",
		Description: "Post an anonymous poll in this group when someone asks for a poll or a vote. Anyone here may ask; a group gets one poll every 10 minutes. " +
			"The poll appears in the chat by itself, so don't repeat its question or options: reply in one short line at most.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"question": {"type": "string", "description": "The question, 1 to 300 characters, in the language of the person asking."},
				"options": {"type": "array", "items": {"type": "string"}, "minItems": 2, "maxItems": 10, "description": "2 to 10 different answers, each 1 to 100 characters."},
				"multiple": {"type": "boolean", "description": "Whether a voter may pick more than one answer. Default false."}
			},
			"required": ["question", "options"],
			"additionalProperties": false
		}`),
		GroupOnly: true,
		Timeout:   pollTimeout,
		Run:       p.run,
	}
}

type polls struct {
	PollOptions
}

// run records the poll, then has Telegram post it. The record comes first
// and is unique per update, so an update Telegram delivers twice posts one
// poll, and it counts against the group at once, so two updates at the same
// moment cannot both post. A poll that surely was not posted, because the
// group had one too recently or Telegram refused it, has its record dropped
// again.
func (p polls) run(ctx context.Context, raw json.RawMessage) (any, error) {
	var args pollArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, err := pollAsker(ctx)
	if err != nil {
		return nil, err
	}
	poll, err := newPoll(caller.ChatID, args)
	if err != nil {
		return nil, err
	}
	first, err := p.Store.RecordModeration(ctx, storage.ModerationAction{
		ChatID: caller.ChatID, Action: actionPoll, RequesterID: caller.UserID, UpdateID: caller.UpdateID,
	})
	if err != nil {
		return nil, internal(err)
	}
	if !first {
		return map[string]any{"already_handled": true, "action": actionPoll, "note": pollAskedNote}, nil
	}
	if err := p.checkPace(ctx, caller.ChatID); err != nil {
		return nil, p.forget(ctx, caller, err)
	}
	if _, err := p.Sender.SendPoll(ctx, poll); err != nil {
		return nil, p.notSent(ctx, caller, err)
	}
	return map[string]any{"sent": true, "note": pollSentNote}, nil
}

// pollAsker is who the turn is for, if anyone in a group: a person or an
// allowed bot. Without the update, a poll could not be posted once only.
func pollAsker(ctx context.Context) (Caller, error) {
	caller, ok := CallerFrom(ctx)
	if !ok || caller.ChatID >= 0 || caller.UpdateID == 0 {
		return Caller{}, &DeniedError{Message: notInAGroupForPoll}
	}
	return caller, nil
}

// newPoll is the poll asked for, trimmed and within Telegram's limits.
func newPoll(chatID int64, args pollArgs) (telegram.OutgoingPoll, error) {
	question := strings.TrimSpace(args.Question)
	if !runesWithin(question, maxPollQuestionRunes) {
		return telegram.OutgoingPoll{}, &ArgumentError{Message: fmt.Sprintf("question must be 1 to %d characters", maxPollQuestionRunes)}
	}
	options, err := checkedOptions(args.Options)
	if err != nil {
		return telegram.OutgoingPoll{}, err
	}
	return telegram.OutgoingPoll{ChatID: chatID, Question: question, Options: options, Multiple: args.Multiple}, nil
}

// checkedOptions trims the options and refuses two that differ only in case
// or spaces, which voters could not tell apart.
func checkedOptions(raw []string) ([]string, error) {
	if len(raw) < minPollOptions || len(raw) > maxPollOptions {
		return nil, &ArgumentError{Message: fmt.Sprintf("give %d to %d options", minPollOptions, maxPollOptions)}
	}
	options := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, option := range raw {
		option = strings.TrimSpace(option)
		if !runesWithin(option, maxPollOptionRunes) {
			return nil, &ArgumentError{Message: fmt.Sprintf("each option must be 1 to %d characters", maxPollOptionRunes)}
		}
		key := strings.ToLower(option)
		if seen[key] {
			return nil, &ArgumentError{Message: "the options must all be different"}
		}
		seen[key] = true
		options = append(options, option)
	}
	return options, nil
}

// runesWithin reports whether text has 1 to limit characters.
func runesWithin(text string, limit int) bool {
	count := utf8.RuneCountInString(text)
	return count >= 1 && count <= limit
}

// checkPace allows a group one poll per pollWindow. The count includes the
// record this update has just made, so any other poll is one too many.
func (p polls) checkPace(ctx context.Context, chatID int64) error {
	made, err := p.Store.ChatModeration(ctx, chatID, actionPoll, p.Now().Add(-pollWindow))
	if err != nil {
		return internal(err)
	}
	if made > 1 {
		return &ArgumentError{Message: pollTooSoon}
	}
	return nil
}

// notSent says why the poll may not be in the chat. A poll Telegram refused
// is forgotten, so that it does not count against the group and may be
// asked for again. When Telegram did not answer, the poll may have been
// posted all the same, and a poll, unlike a second mute, would show twice:
// its record is kept and the group waits out pollWindow instead.
func (p polls) notSent(ctx context.Context, caller Caller, sendErr error) error {
	if _, refused := telegramRefusal(sendErr); refused {
		return p.forget(ctx, caller, telegramFailure(sendErr))
	}
	return errors.New(pollNoAnswer)
}

// forget drops the record of a poll that was not posted, so that it does
// not count against the group and the update may try again, and returns why
// it was not posted. A record that cannot be dropped is reported too.
func (p polls) forget(ctx context.Context, caller Caller, why error) error {
	if err := p.Store.ForgetModeration(context.WithoutCancel(ctx), caller.UpdateID, actionPoll); err != nil {
		return errors.Join(why, internal(err))
	}
	return why
}
