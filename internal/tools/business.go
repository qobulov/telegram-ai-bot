package tools

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // decodes a sticker sent to be a story

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	// storyTimeout covers drawing the story's picture and uploading it.
	storyTimeout = 30 * time.Second
	// defaultStoryHours is how long a story stays up when the owner did not
	// say.
	defaultStoryHours = 24
	// maxBusinessChats is how many of the latest customer chats are listed,
	// and searched for the one the owner named.
	maxBusinessChats = 30
)

// Refusals of the business tools, in words the model can be given.
const (
	businessOwnersOnly = "only my owners can have me do this, and only in their private chat with me"
	businessNotLinked  = "no business account of yours is connected to me: in Telegram, Settings → Telegram Business → Chatbots, add me and choose the chats I may answer"
	storyNoRight       = "your business account has not let me manage stories: in Telegram Business → Chatbots, open my settings and turn on managing stories"
	storyNoPicture     = "there is no picture for the story: send the photo with the request, reply to one, or have me draw or make one in this same message"
	storyBadPicture    = "this picture cannot be used for a story: send it as a photo"
	replyNoRight       = "your business account has not let me reply to messages: in Telegram Business → Chatbots, open my settings and turn on replying"
	checklistNoChat    = "say whose chat it is: the customer's name, @username or chat ID, from the chats action"
	checklistNoneSent  = "I have sent no checklist into that chat yet: send one first"
)

// BusinessAccounts reads the owners' business accounts and the chats and
// checklists the bot knows there.
type BusinessAccounts interface {
	OwnerBusinessConnection(ctx context.Context, userID int64) (storage.BusinessConnection, error)
	BusinessChats(ctx context.Context, connectionID string, limit int) ([]storage.BusinessChat, error)
	SaveBusinessChecklist(ctx context.Context, c storage.BusinessChecklist) error
	LastBusinessChecklist(ctx context.Context, connectionID string, chatID int64) (storage.BusinessChecklist, error)
}

// StoryPoster posts stories on behalf of a business account.
type StoryPoster interface {
	PostStory(ctx context.Context, story telegram.Story) (int64, error)
}

// ChecklistSender sends and changes checklists on behalf of a business
// account.
type ChecklistSender interface {
	SendChecklist(ctx context.Context, checklist telegram.Checklist) (int64, error)
	EditMessageChecklist(ctx context.Context, messageID int64, checklist telegram.Checklist) error
}

// StoryOptions configures the post_story tool. Every field is required.
type StoryOptions struct {
	Accounts BusinessAccounts
	Poster   StoryPoster
}

type storyArgs struct {
	Caption string `json:"caption"`
	Hours   int    `json:"hours"`
}

// StoryTool lets an owner have the bot post a picture as a story of their
// business account: the one they sent or replied to, or one drawn or made
// in the same turn. Only an owner, in their private chat with the bot, and
// only to their own account: the account is the one Telegram says the
// caller connected, never one the model names.
func StoryTool(opts StoryOptions) Tool {
	return Tool{
		Name: "post_story",
		Description: "Post a picture as a story on the owner's Telegram Business account, which they connected to you. " +
			"Only when the owner asks for a story in words. The picture is the one drawn or made in this same turn " +
			"(call generate_image or make_meme first), or else the photo the owner sent with the request or replied to. " +
			"Optional caption; hours is how long it stays up: 6, 12, 24 (default) or 48.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"caption": {"type": "string", "description": "Text shown on the story, in the owner's language; empty for none."},
				"hours": {"type": "integer", "enum": [6, 12, 24, 48], "description": "How long the story stays up, in hours."}
			},
			"additionalProperties": false
		}`),
		OwnerOnly: true,
		Timeout:   storyTimeout,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return postStory(ctx, opts, raw)
		},
	}
}

func postStory(ctx context.Context, opts StoryOptions, raw json.RawMessage) (any, error) {
	var args storyArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	hours := args.Hours
	if hours == 0 {
		hours = defaultStoryHours
	}
	if !slices.Contains(telegram.StoryPeriods, hours*3600) {
		return nil, &ArgumentError{Message: "hours must be 6, 12, 24 or 48"}
	}
	if utf8.RuneCountInString(args.Caption) > telegram.StoryCaptionLimit {
		return nil, &ArgumentError{Message: fmt.Sprintf("caption is at most %d characters", telegram.StoryCaptionLimit)}
	}
	caller, connection, err := ownersConnection(ctx, opts.Accounts)
	if err != nil {
		return nil, err
	}
	if !connection.CanManageStories {
		return nil, &DeniedError{Message: storyNoRight}
	}
	picture := storySource(ctx, caller)
	if len(picture) == 0 {
		return nil, &ArgumentError{Message: storyNoPicture}
	}
	photo, err := storyPicture(picture)
	if err != nil {
		return nil, err
	}
	id, err := opts.Poster.PostStory(ctx, telegram.Story{
		BusinessConnectionID: connection.ID, Photo: photo, Caption: strings.TrimSpace(args.Caption), ActivePeriod: hours * 3600,
	})
	if err != nil {
		return nil, refused(err)
	}
	return map[string]any{"posted": true, "story_id": id, "hours": hours, "note": "Posted on the owner's business account. Tell them so in one short line."}, nil
}

// ownersConnection is the caller's own business connection, for an owner
// in their private chat with the bot.
func ownersConnection(ctx context.Context, accounts BusinessAccounts) (Caller, storage.BusinessConnection, error) {
	caller, ok := CallerFrom(ctx)
	if !ok || !caller.Owner || caller.Business || caller.ChatID != caller.UserID {
		return Caller{}, storage.BusinessConnection{}, &DeniedError{Message: businessOwnersOnly}
	}
	connection, err := accounts.OwnerBusinessConnection(ctx, caller.UserID)
	switch {
	case errors.Is(err, storage.ErrUnknownBusinessConnection):
		return Caller{}, storage.BusinessConnection{}, &ArgumentError{Message: businessNotLinked}
	case err != nil:
		return Caller{}, storage.BusinessConnection{}, internal(err)
	}
	return caller, connection, nil
}

// storySource is the picture to post: the last one drawn or made this
// turn, or else the one the owner sent or replied to.
func storySource(ctx context.Context, caller Caller) []byte {
	if sink, ok := SinkFrom(ctx); ok {
		if drawing, ok := sink.LastDrawing(); ok {
			return drawing.Data
		}
	}
	return caller.Picture
}

// storyPicture fits a picture to a story, which Telegram takes only at
// telegram.StoryWidth by telegram.StoryHeight: the whole picture in the
// middle, on itself blown up to cover the story and darkened, so that
// nothing of it is cut off and there are no bare bars.
func storyPicture(picture []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(picture))
	if err != nil {
		return nil, &ArgumentError{Message: storyBadPicture}
	}
	story := image.NewRGBA(image.Rect(0, 0, telegram.StoryWidth, telegram.StoryHeight))
	xdraw.ApproxBiLinear.Scale(story, story.Bounds(), src, coverRect(src.Bounds(), telegram.StoryWidth, telegram.StoryHeight), xdraw.Src, nil)
	xdraw.Draw(story, story.Bounds(), image.NewUniform(color.RGBA{A: 150}), image.Point{}, xdraw.Over)
	xdraw.CatmullRom.Scale(story, fitRect(src.Bounds(), telegram.StoryWidth, telegram.StoryHeight), src, src.Bounds(), xdraw.Over, nil)
	for _, quality := range []int{90, 75} {
		var out bytes.Buffer
		if err := jpeg.Encode(&out, story, &jpeg.Options{Quality: quality}); err != nil {
			return nil, internal(err)
		}
		if out.Len() <= telegram.StoryMaxBytes {
			return out.Bytes(), nil
		}
	}
	return nil, &ArgumentError{Message: storyBadPicture}
}

// coverRect is the middle part of src with the proportions of w by h: what
// shows when src is blown up to cover w by h.
func coverRect(src image.Rectangle, w, h int) image.Rectangle {
	sw, sh := src.Dx(), src.Dy()
	if sw*h > sh*w { // wider than the story: cut the sides
		cw := sh * w / h
		x := src.Min.X + (sw-cw)/2
		return image.Rect(x, src.Min.Y, x+cw, src.Max.Y)
	}
	ch := sw * h / w
	y := src.Min.Y + (sh-ch)/2
	return image.Rect(src.Min.X, y, src.Max.X, y+ch)
}

// fitRect is where src goes, whole and as large as it fits, in the middle
// of w by h.
func fitRect(src image.Rectangle, w, h int) image.Rectangle {
	sw, sh := src.Dx(), src.Dy()
	fw, fh := w, sh*w/sw
	if fh > h {
		fw, fh = sw*h/sh, h
	}
	x, y := (w-fw)/2, (h-fh)/2
	return image.Rect(x, y, x+fw, y+fh)
}

// ChecklistOptions configures the business_checklist tool. Every field is
// required.
type ChecklistOptions struct {
	Accounts BusinessAccounts
	Sender   ChecklistSender
	Location *time.Location
}

const (
	checklistActionChats = "chats"
	checklistActionSend  = "send"
	checklistActionEdit  = "edit"
)

type checklistArgs struct {
	Action string   `json:"action"`
	Chat   string   `json:"chat"`
	Title  string   `json:"title"`
	Tasks  []string `json:"tasks"`
}

// ChecklistTool lets an owner have the bot send a checklist, on behalf of
// their business account, into one of its customer chats the bot has seen,
// or change the last one it sent there. Telegram lets bots send checklists
// only on behalf of a business account, and not tick their tasks: the
// owner and the customer tick them in the chat.
func ChecklistTool(opts ChecklistOptions) Tool {
	return Tool{
		Name: "business_checklist",
		Description: "Send a checklist, a title and tasks the owner and the customer can tick off, into a customer's chat of the owner's Telegram Business account, on the owner's behalf. " +
			"Only when the owner asks. action chats lists the customer chats you have seen there; send sends a new checklist to chat; " +
			"edit replaces the title and tasks of the last checklist you sent to chat (give the whole new list of tasks; you cannot tick tasks, the owner and customer do). " +
			"chat is the customer's name, @username or chat ID.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["chats", "send", "edit"]},
				"chat": {"type": "string", "description": "The customer: their name, @username or chat ID, as the chats action lists them."},
				"title": {"type": "string", "description": "The checklist's title, up to 255 characters; for edit, empty keeps it."},
				"tasks": {"type": "array", "items": {"type": "string"}, "description": "1 to 30 tasks, each up to 100 characters, in order."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		OwnerOnly: true,
		Timeout:   15 * time.Second,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return runChecklist(ctx, opts, raw)
		},
	}
}

func runChecklist(ctx context.Context, opts ChecklistOptions, raw json.RawMessage) (any, error) {
	var args checklistArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	_, connection, err := ownersConnection(ctx, opts.Accounts)
	if err != nil {
		return nil, err
	}
	chats, err := opts.Accounts.BusinessChats(ctx, connection.ID, maxBusinessChats)
	if err != nil {
		return nil, internal(err)
	}
	switch args.Action {
	case checklistActionChats:
		return listBusinessChats(chats, opts.Location), nil
	case checklistActionSend, checklistActionEdit:
	default:
		return nil, &ArgumentError{Message: "action must be chats, send or edit"}
	}
	if !connection.CanReply {
		return nil, &DeniedError{Message: replyNoRight}
	}
	chat, err := findBusinessChat(chats, args.Chat)
	if err != nil {
		return nil, err
	}
	if args.Action == checklistActionSend {
		return sendChecklist(ctx, opts, connection, chat, args)
	}
	return editChecklist(ctx, opts, connection, chat, args)
}

func sendChecklist(ctx context.Context, opts ChecklistOptions, connection storage.BusinessConnection, chat storage.BusinessChat, args checklistArgs) (any, error) {
	title, tasks, err := checklistContent(args.Title, args.Tasks)
	if err != nil {
		return nil, err
	}
	if title == "" {
		return nil, &ArgumentError{Message: "a checklist needs a title"}
	}
	checklist := telegram.Checklist{BusinessConnectionID: connection.ID, ChatID: chat.ChatID, Title: title, Tasks: tasks}
	id, err := opts.Sender.SendChecklist(ctx, checklist)
	if err != nil {
		return nil, refused(err)
	}
	saveChecklist(ctx, opts, checklist, id)
	return map[string]any{"sent": true, "to": businessChatName(chat), "tasks": len(tasks), "note": "Sent on the owner's behalf. Tell them so in one short line."}, nil
}

func editChecklist(ctx context.Context, opts ChecklistOptions, connection storage.BusinessConnection, chat storage.BusinessChat, args checklistArgs) (any, error) {
	last, err := opts.Accounts.LastBusinessChecklist(ctx, connection.ID, chat.ChatID)
	switch {
	case errors.Is(err, storage.ErrNoChecklist):
		return nil, &ArgumentError{Message: checklistNoneSent}
	case err != nil:
		return nil, internal(err)
	}
	title, tasks, err := checklistContent(args.Title, args.Tasks)
	if err != nil {
		return nil, err
	}
	checklist := telegram.Checklist{BusinessConnectionID: connection.ID, ChatID: chat.ChatID, Title: cmp.Or(title, last.Title), Tasks: tasks}
	if err := opts.Sender.EditMessageChecklist(ctx, last.MessageID, checklist); err != nil {
		return nil, refused(err)
	}
	saveChecklist(ctx, opts, checklist, last.MessageID)
	return map[string]any{"changed": true, "to": businessChatName(chat), "tasks": len(tasks), "note": "Changed. Tell the owner so in one short line."}, nil
}

// saveChecklist keeps what was sent, for a later edit. The checklist is in
// the chat whether or not this works, so a failure is not the tool's.
func saveChecklist(ctx context.Context, opts ChecklistOptions, checklist telegram.Checklist, messageID int64) {
	_ = opts.Accounts.SaveBusinessChecklist(context.WithoutCancel(ctx), storage.BusinessChecklist{
		ConnectionID: checklist.BusinessConnectionID, ChatID: checklist.ChatID, MessageID: messageID,
		Title: checklist.Title, Tasks: checklist.Tasks,
	})
}

// checklistContent checks a title and tasks against Telegram's limits.
func checklistContent(title string, tasks []string) (string, []string, error) {
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) > telegram.ChecklistTitleLimit {
		return "", nil, &ArgumentError{Message: fmt.Sprintf("the title is at most %d characters", telegram.ChecklistTitleLimit)}
	}
	var kept []string
	for _, task := range tasks {
		task = strings.TrimSpace(task)
		if task == "" {
			continue
		}
		if utf8.RuneCountInString(task) > telegram.ChecklistTaskLimit {
			return "", nil, &ArgumentError{Message: fmt.Sprintf("each task is at most %d characters", telegram.ChecklistTaskLimit)}
		}
		kept = append(kept, task)
	}
	if len(kept) == 0 || len(kept) > telegram.MaxChecklistTasks {
		return "", nil, &ArgumentError{Message: fmt.Sprintf("a checklist has 1 to %d tasks", telegram.MaxChecklistTasks)}
	}
	return title, kept, nil
}

type listedBusinessChat struct {
	ChatID   int64  `json:"chat_id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	LastSeen string `json:"last_message"`
}

func listBusinessChats(chats []storage.BusinessChat, location *time.Location) map[string]any {
	listed := make([]listedBusinessChat, len(chats))
	for i, c := range chats {
		listed[i] = listedBusinessChat{ChatID: c.ChatID, Name: c.FirstName, Username: c.Username, LastSeen: c.LastMessageAt.In(location).Format("2 Jan 15:04")}
	}
	return map[string]any{"chats": listed, "note": "The customer chats of the owner's business account you have seen, the latest first. A checklist can go only where the customer wrote in the last 24 hours."}
}

// findBusinessChat is the one chat among chats that wanted names: its ID,
// the customer's @username, or their first name.
func findBusinessChat(chats []storage.BusinessChat, wanted string) (storage.BusinessChat, error) {
	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		return storage.BusinessChat{}, &ArgumentError{Message: checklistNoChat}
	}
	id, notID := strconv.ParseInt(wanted, 10, 64)
	username, isUsername := strings.CutPrefix(wanted, "@")
	var found []storage.BusinessChat
	for _, c := range chats {
		switch {
		case notID == nil && c.ChatID == id,
			isUsername && strings.EqualFold(c.Username, username),
			!isUsername && notID != nil && strings.EqualFold(c.FirstName, wanted):
			found = append(found, c)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return storage.BusinessChat{}, &ArgumentError{Message: "no customer chat I have seen is " + wanted + "; list them with the chats action"}
	}
	return storage.BusinessChat{}, &ArgumentError{Message: "several customers are called " + wanted + "; give the chat ID from the chats action"}
}

func businessChatName(c storage.BusinessChat) string {
	if c.Username != "" {
		return c.FirstName + " @" + c.Username
	}
	return c.FirstName
}
