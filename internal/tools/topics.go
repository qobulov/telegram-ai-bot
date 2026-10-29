package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"telegram-ai-bot/internal/telegram"
)

// In a forum, a supergroup whose messages are kept in topics, the group's
// admins who may manage topics, and the bot's owners as moderate lets them,
// have the bot open, rename, close, reopen or delete a topic, and hide or
// show the General one. It is moderation like any other: the rights are
// Telegram's, checked for the asker and the bot each time, the asker's own
// words must ask for it, and each is recorded in the audit and counted in
// the pace. Which topic is the one the request was written in, from the
// update, never the model's.

// The forum_topic tool's actions.
const (
	topicCreate        = "create"
	topicRename        = "rename"
	topicClose         = "close"
	topicReopen        = "reopen"
	topicDelete        = "delete"
	topicHideGeneral   = "hide_general"
	topicUnhideGeneral = "unhide_general"
)

// topicAuditPrefix names a topic action in the audit, apart from moderate's
// own: "topic_close", "topic_delete".
const topicAuditPrefix = "topic_"

// maxTopicNameRunes is Telegram's limit on a topic's name.
const maxTopicNameRunes = 128

const (
	notAForum = "this group has no topics; its owner can turn them on in the group's settings"
	noTopic   = "write it in the topic you mean, or reply to a message in it: in the General topic only hide_general and unhide_general act, on General itself"
)

// topicRights are the admin rights each action needs, from the person
// asking and from the bot. A topic is deleted with every message in it, so
// that needs the right to delete messages too, which Telegram asks of the
// bot.
var topicRights = map[string][]string{
	topicCreate:        {telegram.RightManageTopics},
	topicRename:        {telegram.RightManageTopics},
	topicClose:         {telegram.RightManageTopics},
	topicReopen:        {telegram.RightManageTopics},
	topicDelete:        {telegram.RightManageTopics, telegram.RightDeleteMessages},
	topicHideGeneral:   {telegram.RightManageTopics},
	topicUnhideGeneral: {telegram.RightManageTopics},
}

// topicColors are the colours Telegram allows for a new topic's icon.
var topicColors = map[string]int{
	"blue":   0x6FB9F0,
	"yellow": 0xFFD67E,
	"violet": 0xCB86DB,
	"green":  0x8EEE98,
	"rose":   0xFF93B2,
	"red":    0xFB6F5F,
}

// ForumTopics has Telegram act on a forum's topics.
type ForumTopics interface {
	CreateForumTopic(ctx context.Context, chatID int64, name string, iconColor int) (int64, error)
	EditForumTopic(ctx context.Context, chatID, threadID int64, name string) error
	CloseForumTopic(ctx context.Context, chatID, threadID int64) error
	ReopenForumTopic(ctx context.Context, chatID, threadID int64) error
	DeleteForumTopic(ctx context.Context, chatID, threadID int64) error
	HideGeneralForumTopic(ctx context.Context, chatID int64) error
	UnhideGeneralForumTopic(ctx context.Context, chatID int64) error
}

type topicArgs struct {
	Action string `json:"action"`
	Name   string `json:"name"`
	Color  string `json:"color"`
	Reason string `json:"reason"`
}

// TopicsTool is the forum_topic tool, through which the model asks for an
// action on a forum's topics. It shares moderation's checks and audit.
func (m *Moderation) TopicsTool(topics ForumTopics) Tool {
	return Tool{
		Name: "forum_topic",
		Description: "In a group with topics (a forum), whenever someone asks you to: create a new topic (name, optional color); rename (name), close, reopen or delete the topic their request was written in, or that of the message they replied to; hide_general or unhide_general: hide or show the General topic. " +
			"It checks who may ask, which are the group's admins with the right to manage topics (and to delete messages, for delete) and your owners, and refuses anyone else with the reason. " +
			"It does it at once, with no question, but only when the person's own message asks for it; say it is done only when it reports done.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["create", "rename", "close", "reopen", "delete", "hide_general", "unhide_general"]},
				"name": {"type": "string", "description": "For create and rename: the topic's name, in the words they gave, 1 to 128 characters."},
				"color": {"type": "string", "enum": ["blue", "yellow", "violet", "green", "rose", "red"], "description": "For create: the icon's colour, only when they name one."},
				"reason": {"type": "string", "description": "Optional: why, in a few words."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		GroupOnly: true,
		Timeout:   moderationTimeout,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return m.runTopic(ctx, topics, raw)
		},
	}
}

// topicOrder is one request about a topic. Its order is what the audit
// keeps: the action with topicAuditPrefix, and the topic as the target
// message, the ID of the service message that opened it.
type topicOrder struct {
	order
	verb  string
	color int
}

func (m *Moderation) runTopic(ctx context.Context, topics ForumTopics, raw json.RawMessage) (any, error) {
	var args topicArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, err := groupRequester(ctx)
	if err != nil {
		return nil, err
	}
	rights, known := topicRights[args.Action]
	if !known {
		return nil, &ArgumentError{Message: "action must be one of the tool's actions"}
	}
	if !topicAsked(caller.Words, args.Action) {
		return nil, &DeniedError{Message: notAsked}
	}
	if !caller.Forum {
		return nil, &DeniedError{Message: notAForum}
	}
	o, err := newTopicOrder(caller, args)
	if err != nil {
		return nil, err
	}
	if err := m.checkTopic(ctx, caller, rights); err != nil {
		return nil, err
	}
	first, err := m.opts.Store.RecordModeration(ctx, o.audit())
	if err != nil {
		return nil, internal(err)
	}
	if !first {
		return map[string]any{"done": true, "note": alreadyDoneNote}, nil
	}
	if err := o.send(ctx, topics); err != nil {
		return nil, notCarriedOut(m.failed(ctx, o.order, err))
	}
	return map[string]any{"done": true, "said": o.done().String(), "note": doneNote}, nil
}

// newTopicOrder reads caller's request: a name for create and rename, a
// colour for create, and for an action on one topic the topic they wrote
// in.
func newTopicOrder(caller Caller, args topicArgs) (topicOrder, error) {
	o := topicOrder{verb: args.Action, order: order{
		caller: caller, action: topicAuditPrefix + args.Action,
		reason: cutRunes(strings.TrimSpace(args.Reason), maxReasonRunes),
	}}
	switch args.Action {
	case topicCreate, topicRename:
		name := strings.TrimSpace(args.Name)
		if name == "" || utf8.RuneCountInString(name) > maxTopicNameRunes {
			return topicOrder{}, &ArgumentError{Message: fmt.Sprintf("name must be the topic's name, 1 to %d characters", maxTopicNameRunes)}
		}
		o.text = name
	}
	if args.Action == topicCreate && args.Color != "" {
		color, known := topicColors[args.Color]
		if !known {
			return topicOrder{}, &ArgumentError{Message: "color must be blue, yellow, violet, green, rose or red"}
		}
		o.color = color
	}
	switch args.Action {
	case topicRename, topicClose, topicReopen, topicDelete:
		if caller.TopicID == 0 {
			return topicOrder{}, &DeniedError{Message: noTopic}
		}
		o.target = Target{MessageID: caller.TopicID}
	}
	return o, nil
}

// checkTopic decides whether the bot may act: the person asking and the
// bot must both hold every right, as Telegram says now, and the person must
// not have asked too often.
func (m *Moderation) checkTopic(ctx context.Context, caller Caller, rights []string) error {
	for _, right := range rights {
		if err := m.checkRequester(ctx, caller, right); err != nil {
			return err
		}
		if err := m.checkBot(ctx, caller.ChatID, right); err != nil {
			return err
		}
	}
	return m.checkPace(ctx, caller)
}

func (o topicOrder) send(ctx context.Context, topics ForumTopics) error {
	chatID, threadID := o.caller.ChatID, o.target.MessageID
	switch o.verb {
	case topicCreate:
		_, err := topics.CreateForumTopic(ctx, chatID, o.text, o.color)
		return err
	case topicRename:
		return topics.EditForumTopic(ctx, chatID, threadID, o.text)
	case topicClose:
		return topics.CloseForumTopic(ctx, chatID, threadID)
	case topicReopen:
		return topics.ReopenForumTopic(ctx, chatID, threadID)
	case topicDelete:
		return topics.DeleteForumTopic(ctx, chatID, threadID)
	case topicHideGeneral:
		return topics.HideGeneralForumTopic(ctx, chatID)
	case topicUnhideGeneral:
		return topics.UnhideGeneralForumTopic(ctx, chatID)
	}
	return fmt.Errorf("topics: unknown action %q", o.verb)
}

// done says, for the chat, what o did.
func (o topicOrder) done() chatText {
	switch o.verb {
	case topicCreate:
		return chatText{"Bajarildi: «" + o.text + "» mavzusi ochildi.", "Готово: создана тема «" + o.text + "»."}
	case topicRename:
		return chatText{"Bajarildi: mavzu nomi «" + o.text + "» bo'ldi.", "Готово: тема теперь называется «" + o.text + "»."}
	case topicClose:
		return chatText{"Bajarildi: mavzu yopildi.", "Готово: тема закрыта."}
	case topicReopen:
		return chatText{"Bajarildi: mavzu qayta ochildi.", "Готово: тема снова открыта."}
	case topicDelete:
		return chatText{"Bajarildi: mavzu o'chirildi.", "Готово: тема удалена."}
	case topicHideGeneral:
		return chatText{"Bajarildi: General mavzusi yashirildi.", "Готово: тема General скрыта."}
	}
	return chatText{"Bajarildi: General mavzusi yana ko'rinadi.", "Готово: тема General снова видна."}
}

// topicWords name a topic, and generalWords the General one, as askedFor
// folds a message: "mavzuni yop", "закрой тему", "General'ni yashir".
var (
	topicWords   = []string{"topic", "topik", "топик", "mavzu", "мавзу", "тема", "тему", "темы", "теме", "bolim", "бўлим", "болим", "раздел", "razdel", "vetka", "ветк"}
	generalWords = []string{"general", "генерал", "umumiy", "умумий", "общ", "основн"}
)

// topicActionWords are what a person asking for each action says, besides
// naming a topic.
var topicActionWords = map[string][]string{
	topicCreate:        {"och", "yarat", "qosh", "create", "new", "yangi", "созда", "откр", "добав", "add"},
	topicRename:        append([]string{"nom", "name", "назв"}, changeWords...),
	topicClose:         {"yop", "close", "закр", "ёп", "йоп"},
	topicReopen:        {"och", "open", "откр"},
	topicDelete:        {"ochir", "delete", "удал", "sil", "olib tashla", "ўчир", "учир"},
	topicHideGeneral:   {"yashir", "berkit", "hide", "скр", "спряч", "яшир"},
	topicUnhideGeneral: {"korsat", "qaytar", "och", "unhide", "show", "покаж", "верн", "откр", "кўрсат"},
}

// topicAsked reports whether words, what the person asking wrote
// themselves, ask for action: they name a topic, or General for hiding and
// showing it, and say what to do with it.
func topicAsked(words, action string) bool {
	subject := topicWords
	if action == topicHideGeneral || action == topicUnhideGeneral {
		subject = generalWords
	}
	return says(words, subject) && says(words, topicActionWords[action])
}
