package telegram

import (
	"context"
	"strings"
)

// InlineQuery is someone typing "@bot words" in any chat: the bot answers
// with results they may pick, which their app then posts as their own
// message. The bot learns nothing of the chat but its type.
type InlineQuery struct {
	ID    string `json:"id"`
	From  *User  `json:"from"`
	Query string `json:"query"`
	// ChatType is the kind of chat it was typed in, such as "private" or
	// "supergroup"; "sender" for the chat with the bot itself.
	ChatType string `json:"chat_type"`
}

// InlineArticle is one result of an inline answer: a title and a line
// under it, and the text posted when it is picked.
type InlineArticle struct {
	// ID tells the results of one answer apart, 1 to 64 bytes.
	ID          string
	Title       string
	Description string
	Text        string
}

// InlineAnswer answers an inline query.
type InlineAnswer struct {
	QueryID  string
	Articles []InlineArticle
	// CacheSeconds is how long Telegram may show these results again for
	// the same words without asking; 0 asks every time.
	CacheSeconds int
}

type answerInlineParams struct {
	InlineQueryID string                `json:"inline_query_id"`
	Results       []inlineArticleResult `json:"results"`
	// CacheTime is always sent, 0 too: left out, Telegram keeps results
	// for 300 seconds.
	CacheTime int `json:"cache_time"`
	// IsPersonal keeps one person's results from being shown to another
	// who types the same words: an answer counts against its asker's quota
	// and may speak to them.
	IsPersonal bool `json:"is_personal"`
}

type inlineArticleResult struct {
	Type                string              `json:"type"`
	ID                  string              `json:"id"`
	Title               string              `json:"title"`
	Description         string              `json:"description,omitempty"`
	InputMessageContent inputMessageContent `json:"input_message_content"`
}

type inputMessageContent struct {
	MessageText string          `json:"message_text"`
	Entities    []messageEntity `json:"entities,omitempty"`
}

// inlineDescriptionRunes is how much of an article's text is shown under
// its title.
const inlineDescriptionRunes = 100

// AnswerInlineQuery sends the results of an inline query, always personal.
// Each article's text is formatted as SendMessage formats a message, and
// cut to one message: an article posts exactly one. Telegram refuses an
// answer to a query older than about ten seconds.
func (c *Client) AnswerInlineQuery(ctx context.Context, answer InlineAnswer) error {
	params := answerInlineParams{
		InlineQueryID: answer.QueryID,
		Results:       make([]inlineArticleResult, 0, len(answer.Articles)),
		CacheTime:     answer.CacheSeconds,
		IsPersonal:    true,
	}
	for _, article := range answer.Articles {
		text, entities := formatted(firstChunk(article.Text))
		description := article.Description
		if description == "" {
			description = strings.Join(strings.Fields(text), " ")
		}
		params.Results = append(params.Results, inlineArticleResult{
			Type:                "article",
			ID:                  article.ID,
			Title:               article.Title,
			Description:         cutRunes(description, inlineDescriptionRunes),
			InputMessageContent: inputMessageContent{MessageText: text, Entities: entities},
		})
	}
	return c.call(ctx, "answerInlineQuery", params, nil)
}

// firstChunk is as much of text as one message holds.
func firstChunk(text string) string {
	if chunks := Split(strings.TrimSpace(text), MessageLimit); len(chunks) > 0 {
		return chunks[0]
	}
	return ""
}
