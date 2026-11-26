package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"slices"
	"time"

	"telegram-ai-bot/internal/storage"
)

const shipNote = "percent is their match today, the same all day: say it exactly, then a short, funny verdict in their language built on these facts (their pictures, how much they write, their cards), " +
	"as friends, a duo in games or a pair, kindly: never sexual, never a real person's body or looks, never one of them put down. Tag no one else."

// ShipTool matches two members, as the owners asked on 2026-11-25: a
// percent that holds for the day, and what is known of each for Maria's
// verdict.
func ShipTool(opts MemberInfoOptions) Tool {
	avatars := &avatarDescriptions{eyes: opts.Eyes, prompt: memberPhotoPrompt}
	return Tool{
		Name: "ship",
		Description: "How well two members match today (\"moslik\", \"ship qil\", \"@Ali va @Vali mosmi\", \"совместимость\"): the two they tagged, the one tagged and the asker, the one they replied to and the asker, " +
			"or with random the asker and a member picked at random among those who wrote lately (\"menga juft top\"). Gives the percent and facts about each for a playful verdict. Call it every time: never make a percent up.",
		Schema: json.RawMessage(`{"type": "object", "properties": {
			"random": {"type": "boolean", "description": "Match the asker with a member picked at random."}
		}, "additionalProperties": false}`),
		GroupOnly: true,
		Timeout:   memberInfoTimeout,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var args struct {
				Random bool `json:"random"`
			}
			if err := decodeArgs(raw, &args); err != nil {
				return nil, err
			}
			return ship(ctx, opts, avatars, args.Random)
		},
	}
}

func ship(ctx context.Context, opts MemberInfoOptions, avatars *avatarDescriptions, random bool) (any, error) {
	caller, ok := CallerFrom(ctx)
	if !ok || caller.ChatID >= 0 {
		return nil, &DeniedError{Message: statsNotInAGroup}
	}
	pair := shipPair
	if random {
		pair = randomPair
	}
	a, b, err := pair(ctx, opts, caller)
	if err != nil {
		return nil, err
	}
	if a == b {
		return nil, &DeniedError{Message: "that is one member twice; ask them to tag two members, or one besides themselves"}
	}
	location := opts.Location
	if location == nil {
		location = time.UTC
	}
	today := time.Now().In(location)
	return map[string]any{
		"percent": shipPercent(a, b, today),
		"first":   shipFacts(ctx, opts, avatars, caller.ChatID, a, today),
		"second":  shipFacts(ctx, opts, avatars, caller.ChatID, b, today),
		"note":    shipNote,
	}, nil
}

// shipPair is the two members to match: two tagged, else one tagged or
// replied to and the asker.
func shipPair(ctx context.Context, opts MemberInfoOptions, caller Caller) (int64, int64, error) {
	var ids []int64
	for _, m := range caller.Mentions {
		id, err := mentioned(ctx, opts, caller.ChatID, m)
		if err != nil {
			return 0, 0, err
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 && caller.Target != nil && caller.Target.UserID != 0 {
		ids = append(ids, caller.Target.UserID)
	}
	switch len(ids) {
	case 1:
		return caller.UserID, ids[0], nil
	case 2:
		return ids[0], ids[1], nil
	}
	return 0, 0, &DeniedError{Message: "ask them to tag one or two members, or to reply to the member to match with"}
}

// shipRecent is how far back a random match's members wrote.
const shipRecent = 72 * time.Hour

// randomPair is the asker and a member picked at random among the others
// who wrote in the group lately.
func randomPair(ctx context.Context, opts MemberInfoOptions, caller Caller) (int64, int64, error) {
	messages, err := opts.Store.GroupMessagesSince(ctx, caller.ChatID, time.Now().Add(-shipRecent), 500)
	if err != nil {
		return 0, 0, &InternalError{Err: err, Message: infoUnavailable}
	}
	var others []int64
	for _, m := range messages {
		if m.AuthorID > 0 && m.AuthorID != caller.UserID && !slices.Contains(others, m.AuthorID) {
			others = append(others, m.AuthorID)
		}
	}
	if len(others) == 0 {
		return 0, 0, &DeniedError{Message: "nobody else wrote here lately to be matched with; say so"}
	}
	return caller.UserID, others[rand.IntN(len(others))], nil
}

// mentioned is the member a tag names.
func mentioned(ctx context.Context, opts MemberInfoOptions, chatID int64, m Mention) (int64, error) {
	if m.UserID != 0 {
		return m.UserID, nil
	}
	id, err := opts.Store.AuthorByUsername(ctx, chatID, m.Username)
	if errors.Is(err, storage.ErrUnknownAuthor) {
		return 0, &DeniedError{Message: unknownMention}
	}
	if err != nil {
		return 0, &InternalError{Err: err, Message: infoUnavailable}
	}
	return id, nil
}

// shipPercent is the pair's match on day, the same whichever comes first.
func shipPercent(a, b int64, day time.Time) int {
	a, b = min(a, b), max(a, b)
	h := fnv.New32a()
	fmt.Fprintf(h, "ship:%d:%d:%s", a, b, day.Format("2006-01-02"))
	return int(h.Sum32() % 101)
}

// shipFacts are what is known of one member for the verdict.
func shipFacts(ctx context.Context, opts MemberInfoOptions, avatars *avatarDescriptions, chatID, userID int64, today time.Time) map[string]any {
	facts := map[string]any{}
	if member, err := opts.Telegram.GetChatMember(ctx, chatID, userID); err == nil && member.User != nil {
		facts["name"] = member.User.FirstName
	}
	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	if activity, err := opts.Store.MemberActivityOf(ctx, chatID, userID, day); err == nil {
		facts["messages_this_week"] = activity.MessagesWeek
	}
	if _, owned, points, err := opts.Store.CardCounts(ctx, userID); err == nil && points > 0 {
		facts["card_points"] = points
		facts["mythic_cards"] = owned[4]
	}
	if opts.Photo != nil && opts.Eyes != nil {
		facts["photo"] = memberPhoto(ctx, opts, avatars, userID)
	}
	return facts
}
