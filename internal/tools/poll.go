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

// actionPoll is how a poll is recorded in the moderation audit, and
// actionPollStop closing one.
const (
	actionPoll     = storage.ActionPoll
	actionPollStop = storage.ActionPollStop
)

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

	noOpenPoll     = "there is no open poll I made in this group to close"
	repliedNotOpen = "the poll they replied to is not an open poll I made here: I can only close my own, while they are open"
	notAskedToStop = "their own message does not ask to close a poll, so nothing was done"
	notPollsAsker  = "only whoever asked for this poll, the group's admins or my owners can close it"
	pollStoppedOff = "Telegram says this poll is closed already."
	pollStopNote   = "The poll is closed, and Telegram shows its results in the chat. Give the counts in one or two short lines, in their language."
	pollStopAsked  = "This message already closed a poll; don't say it again: reply with exactly [silent] unless something else needs an answer."
)

// stopWords are what a person asking to close a poll says, folded as says
// folds them: "so'rovnomani yop", "ovoz berishni to'xtat", "закрой опрос".
var stopWords = []string{"yop", "toxtat", "tugat", "yakun", "stop", "close", "закр", "останов", "заверш", "хватит", "ёп", "тўхтат"}

// PollSender posts a poll in a chat, and closes one it posted.
type PollSender interface {
	SendPoll(ctx context.Context, poll telegram.OutgoingPoll) (int64, error)
	StopPoll(ctx context.Context, chatID, messageID int64) (telegram.Poll, error)
}

// PollBook is the audit polls are counted in, which also keeps each poll's
// message, to close it later.
type PollBook interface {
	PollCounter
	SetModerationMessage(ctx context.Context, updateID int64, action string, messageID int64) error
	OpenPoll(ctx context.Context, chatID, messageID int64) (storage.ModerationAction, bool, error)
}

// PollCounter keeps the polls the bot posted, or may have posted, in the
// moderation audit, which tells how recently a group got one and whether an
// update already asked.
type PollCounter interface {
	RecordModeration(ctx context.Context, a storage.ModerationAction) (bool, error)
	ForgetModeration(ctx context.Context, updateID int64, action string) error
	ChatModeration(ctx context.Context, chatID int64, action string, since time.Time) (int, error)
}

// PollOptions configures the create_poll tool. Sender, Store, Members and
// BotID are required.
type PollOptions struct {
	Sender PollSender
	Store  PollBook
	// Members tells who administers the group, for closing a poll someone
	// else asked for.
	Members MemberLookup
	BotID   int64
	// OwnersByID is true when the owners are known by their Telegram ID:
	// only then may an owner close anyone's poll, as in moderation.
	OwnersByID bool
	Now        func() time.Time
}

type pollArgs struct {
	Action   string   `json:"action"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Multiple bool     `json:"multiple"`
}

// PollTool lets anyone in a group have the bot post an anonymous poll there,
// and whoever asked for one, the group's admins or the owners close it.
// The poll goes to the group the turn is in, never one the model names, and
// only a poll the bot made there is closed.
func PollTool(opts PollOptions) Tool {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	p := polls{opts}
	return Tool{
		Name: "create_poll",
		Description: "Post an anonymous poll in this group when someone asks for a poll or a vote. Anyone here may ask; a group gets one poll every 10 minutes. " +
			"The poll appears in the chat by itself, so don't repeat its question or options: reply in one short line at most. " +
			"action stop closes a poll you made here, the one they replied to or else your latest, when they ask to close or end it (\"so'rovnomani yop\", \"закрой опрос\"), and gives its final counts; only whoever asked for that poll, the group's admins or your owners may.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["create", "stop"], "description": "create (the default) posts a poll; stop closes one and needs nothing else."},
				"question": {"type": "string", "description": "For create: the question, 1 to 300 characters, in the language of the person asking."},
				"options": {"type": "array", "items": {"type": "string"}, "minItems": 2, "maxItems": 10, "description": "For create: 2 to 10 different answers, each 1 to 100 characters."},
				"multiple": {"type": "boolean", "description": "For create: whether a voter may pick more than one answer. Default false."}
			},
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
	switch args.Action {
	case "stop":
		return p.stop(ctx, caller)
	case "", "create":
	default:
		return nil, &ArgumentError{Message: "action must be create or stop"}
	}
	poll, err := newPoll(caller, args)
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
	if err := p.checkPace(ctx, caller.ChatID, actionPoll); err != nil {
		return nil, p.forget(ctx, caller, actionPoll, err)
	}
	sentID, err := p.Sender.SendPoll(ctx, poll)
	if err != nil {
		return nil, p.notSent(ctx, caller, actionPoll, err)
	}
	// Keeping its message only lets the poll be closed later; the poll is
	// in the chat either way.
	_ = p.Store.SetModerationMessage(context.WithoutCancel(ctx), caller.UpdateID, actionPoll, sentID)
	return map[string]any{"sent": true, "note": pollSentNote}, nil
}

// pollResult is one answer of a closed poll and its votes.
type pollResult struct {
	Option string `json:"option"`
	Votes  int    `json:"votes"`
}

// stop closes a poll the bot made in the caller's group: the one they
// replied to, or else the latest still open. Like moderation, it is done
// only when their own words ask, and only for whoever asked for the poll,
// an admin of the group or an owner. The record comes first, as a poll's
// does, so an update delivered twice closes once.
func (p polls) stop(ctx context.Context, caller Caller) (any, error) {
	if !caller.Human {
		return nil, &DeniedError{Message: notPollsAsker}
	}
	if !says(caller.Words, stopWords) {
		return nil, &DeniedError{Message: notAskedToStop}
	}
	poll, err := p.pollToStop(ctx, caller)
	if err != nil {
		return nil, err
	}
	if err := p.mayStop(ctx, caller, poll); err != nil {
		return nil, err
	}
	first, err := p.Store.RecordModeration(ctx, storage.ModerationAction{
		ChatID: caller.ChatID, Action: actionPollStop, RequesterID: caller.UserID, TargetMessageID: poll.TargetMessageID, UpdateID: caller.UpdateID,
	})
	if err != nil {
		return nil, internal(err)
	}
	if !first {
		return map[string]any{"already_handled": true, "note": pollStopAsked}, nil
	}
	closed, err := p.Sender.StopPoll(ctx, caller.ChatID, poll.TargetMessageID)
	if refusal, refused := telegramRefusal(err); refused && strings.Contains(refusal.Description, "already been closed") {
		// Closed by Telegram's own timer or by hand: the record stays true.
		return map[string]any{"stopped": true, "note": pollStoppedOff}, nil
	}
	if err != nil {
		// Closing twice does no harm, so whatever Telegram said, it may be
		// asked again.
		return nil, p.forget(ctx, caller, actionPollStop, telegramFailure(err))
	}
	results := make([]pollResult, 0, len(closed.Options))
	for _, option := range closed.Options {
		results = append(results, pollResult{Option: option.Text, Votes: option.VoterCount})
	}
	return map[string]any{"stopped": true, "question": closed.Question, "results": results, "voters": closed.TotalVoterCount, "note": pollStopNote}, nil
}

// pollToStop is the poll the caller means: the one they replied to, which
// must be an open poll of the bot's here, or else the latest open one.
func (p polls) pollToStop(ctx context.Context, caller Caller) (storage.ModerationAction, error) {
	var messageID int64
	if t := caller.Target; t != nil && t.Poll {
		if t.UserID != p.BotID {
			return storage.ModerationAction{}, &DeniedError{Message: repliedNotOpen}
		}
		messageID = t.MessageID
	}
	poll, found, err := p.Store.OpenPoll(ctx, caller.ChatID, messageID)
	switch {
	case err != nil:
		return storage.ModerationAction{}, internal(err)
	case !found && messageID != 0:
		return storage.ModerationAction{}, &DeniedError{Message: repliedNotOpen}
	case !found:
		return storage.ModerationAction{}, &DeniedError{Message: noOpenPoll}
	}
	return poll, nil
}

// mayStop lets whoever asked for the poll close it, an owner known by ID
// any poll, and anyone else only when Telegram says they administer the
// group. When Telegram cannot say, the answer is no.
func (p polls) mayStop(ctx context.Context, caller Caller, poll storage.ModerationAction) error {
	if poll.RequesterID == caller.UserID || (caller.Owner && p.OwnersByID) {
		return nil
	}
	member, err := p.Members.GetChatMember(ctx, caller.ChatID, caller.UserID)
	if err != nil {
		return errLookup
	}
	if !member.IsAdmin() {
		return &DeniedError{Message: notPollsAsker}
	}
	return nil
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

// newPoll is the poll asked for, trimmed and within Telegram's limits. It
// replies to the request: in a forum, that keeps it in the request's topic.
func newPoll(caller Caller, args pollArgs) (telegram.OutgoingPoll, error) {
	question := strings.TrimSpace(args.Question)
	if !runesWithin(question, maxPollQuestionRunes) {
		return telegram.OutgoingPoll{}, &ArgumentError{Message: fmt.Sprintf("question must be 1 to %d characters", maxPollQuestionRunes)}
	}
	options, err := checkedOptions(args.Options)
	if err != nil {
		return telegram.OutgoingPoll{}, err
	}
	return telegram.OutgoingPoll{ChatID: caller.ChatID, Question: question, Options: options, Multiple: args.Multiple, ReplyToMessageID: caller.MessageID}, nil
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

// checkPace allows a group one poll, or one quiz, per pollWindow. The count
// includes the record this update has just made, so any other is one too
// many.
func (p polls) checkPace(ctx context.Context, chatID int64, action string) error {
	made, err := p.Store.ChatModeration(ctx, chatID, action, p.Now().Add(-pollWindow))
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
func (p polls) notSent(ctx context.Context, caller Caller, action string, sendErr error) error {
	if _, refused := telegramRefusal(sendErr); refused {
		return p.forget(ctx, caller, action, telegramFailure(sendErr))
	}
	return errors.New(pollNoAnswer)
}

// forget drops the record of a poll that was not posted, so that it does
// not count against the group and the update may try again, and returns why
// it was not posted. A record that cannot be dropped is reported too.
func (p polls) forget(ctx context.Context, caller Caller, action string, why error) error {
	if err := p.Store.ForgetModeration(context.WithoutCancel(ctx), caller.UpdateID, action); err != nil {
		return errors.Join(why, internal(err))
	}
	return why
}
