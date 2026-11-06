package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"strconv"
	"strings"
)

// BusinessConnection is a Telegram Business account that connected the bot
// as its chatbot, in the account's settings: whose account it is, what the
// bot may do there, and whether the connection still holds.
type BusinessConnection struct {
	ID string `json:"id"`
	// User is the account's owner, who made the connection.
	User User `json:"user"`
	// UserChatID is the bot's private chat with that owner.
	UserChatID int64 `json:"user_chat_id"`
	// Date is when the connection was made, in Unix seconds.
	Date      int64              `json:"date"`
	Rights    *BusinessBotRights `json:"rights"`
	IsEnabled bool               `json:"is_enabled"`
}

// CanReply reports a connection the bot may answer through: one still
// enabled, whose owner let the bot reply in the chats that had incoming
// messages in the last 24 hours.
func (c BusinessConnection) CanReply() bool {
	return c.IsEnabled && c.Rights != nil && c.Rights.CanReply
}

// CanManageStories reports a connection the bot may post stories through.
func (c BusinessConnection) CanManageStories() bool {
	return c.IsEnabled && c.Rights != nil && c.Rights.CanManageStories
}

// BusinessBotRights are what the owner of a business account let the bot
// do there. Only the rights the bot uses are kept.
type BusinessBotRights struct {
	CanReply         bool `json:"can_reply"`
	CanReadMessages  bool `json:"can_read_messages"`
	CanManageStories bool `json:"can_manage_stories"`
}

type businessConnectionParams struct {
	BusinessConnectionID string `json:"business_connection_id"`
}

// GetBusinessConnection asks Telegram about a connection, for a message
// that came through one the bot has no record of.
func (c *Client) GetBusinessConnection(ctx context.Context, id string) (BusinessConnection, error) {
	var connection BusinessConnection
	if err := c.call(ctx, "getBusinessConnection", businessConnectionParams{BusinessConnectionID: id}, &connection); err != nil {
		return BusinessConnection{}, err
	}
	return connection, nil
}

// Story sizes and periods Telegram accepts from a bot.
const (
	StoryWidth  = 1080
	StoryHeight = 1920
	// StoryCaptionLimit is the longest story caption, in characters.
	StoryCaptionLimit = 2048
	// StoryMaxBytes is the largest picture a story may be.
	StoryMaxBytes = 10 << 20
)

// StoryPeriods are the periods, in seconds, after which a story may go to
// the archive: 6, 12, 24 or 48 hours.
var StoryPeriods = []int{6 * 3600, 12 * 3600, 24 * 3600, 48 * 3600}

// Story is a picture to post as a business account's story.
type Story struct {
	BusinessConnectionID string
	// Photo must be StoryWidth by StoryHeight and at most StoryMaxBytes;
	// Telegram takes a story's picture only as a new upload.
	Photo   []byte
	Caption string
	// ActivePeriod is one of StoryPeriods.
	ActivePeriod int
}

type inputStoryContent struct {
	Type  string `json:"type"`
	Photo string `json:"photo"`
}

type postedStory struct {
	ID int64 `json:"id"`
}

// PostStory posts a story on behalf of the connection's business account,
// which needs the can_manage_stories right, and returns its ID.
func (c *Client) PostStory(ctx context.Context, story Story) (int64, error) {
	body, contentType, err := storyForm(story)
	if err != nil {
		return 0, fmt.Errorf("telegram postStory: encode: %w", err)
	}
	var posted postedStory
	if err := c.postForm(ctx, "postStory", body, contentType, &posted); err != nil {
		return 0, err
	}
	return posted.ID, nil
}

// storyForm is the story as a multipart form: the content names the
// picture's part, "attach://story".
func storyForm(story Story) (*bytes.Buffer, string, error) {
	content, err := json.Marshal(inputStoryContent{Type: "photo", Photo: "attach://story"})
	if err != nil {
		return nil, "", err
	}
	fields := map[string]string{
		"business_connection_id": story.BusinessConnectionID,
		"content":                string(content),
		"active_period":          strconv.Itoa(story.ActivePeriod),
	}
	if caption := strings.TrimSpace(story.Caption); caption != "" {
		text, entities := formatted(cutRunes(caption, StoryCaptionLimit))
		fields["caption"] = text
		if len(entities) > 0 {
			encoded, err := json.Marshal(entities)
			if err != nil {
				return nil, "", err
			}
			fields["caption_entities"] = string(encoded)
		}
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := form.WriteField(name, value); err != nil {
			return nil, "", err
		}
	}
	part, err := form.CreateFormFile("story", "story.jpg")
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(story.Photo); err != nil {
		return nil, "", err
	}
	if err := form.Close(); err != nil {
		return nil, "", err
	}
	return &body, form.FormDataContentType(), nil
}

// Checklist limits Telegram sets.
const (
	ChecklistTitleLimit = 255
	ChecklistTaskLimit  = 100
	MaxChecklistTasks   = 30
)

// Checklist is a checklist to send, or to put in place of one sent, on
// behalf of a business account: a title and its tasks, numbered from 1 in
// order. Both the account and the person it is sent to may tick tasks off
// and add more.
type Checklist struct {
	BusinessConnectionID string
	ChatID               int64
	Title                string
	Tasks                []string
}

type inputChecklist struct {
	Title                    string               `json:"title"`
	Tasks                    []inputChecklistTask `json:"tasks"`
	OthersCanAddTasks        bool                 `json:"others_can_add_tasks"`
	OthersCanMarkTasksAsDone bool                 `json:"others_can_mark_tasks_as_done"`
}

type inputChecklistTask struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

func (c Checklist) input() inputChecklist {
	in := inputChecklist{Title: c.Title, OthersCanAddTasks: true, OthersCanMarkTasksAsDone: true}
	for i, task := range c.Tasks {
		in.Tasks = append(in.Tasks, inputChecklistTask{ID: i + 1, Text: task})
	}
	return in
}

type sendChecklistParams struct {
	BusinessConnectionID string         `json:"business_connection_id"`
	ChatID               int64          `json:"chat_id"`
	Checklist            inputChecklist `json:"checklist"`
}

// SendChecklist sends a checklist into a chat of the business account and
// returns the sent message's ID. Bots may send checklists only on behalf
// of a business account.
func (c *Client) SendChecklist(ctx context.Context, checklist Checklist) (int64, error) {
	var sent sentMessage
	params := sendChecklistParams{BusinessConnectionID: checklist.BusinessConnectionID, ChatID: checklist.ChatID, Checklist: checklist.input()}
	if err := c.call(ctx, "sendChecklist", params, &sent); err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

type editChecklistParams struct {
	BusinessConnectionID string         `json:"business_connection_id"`
	ChatID               int64          `json:"chat_id"`
	MessageID            int64          `json:"message_id"`
	Checklist            inputChecklist `json:"checklist"`
}

// EditMessageChecklist puts checklist in place of the one in message
// messageID. Its tasks are numbered by place, as SendChecklist numbers
// them, so a task that keeps its place keeps its ID.
func (c *Client) EditMessageChecklist(ctx context.Context, messageID int64, checklist Checklist) error {
	params := editChecklistParams{BusinessConnectionID: checklist.BusinessConnectionID, ChatID: checklist.ChatID, MessageID: messageID, Checklist: checklist.input()}
	return c.call(ctx, "editMessageChecklist", params, nil)
}
