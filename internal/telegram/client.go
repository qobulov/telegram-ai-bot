package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPollTimeout = 30 * time.Second
	defaultBaseDelay   = time.Second
	maxSendAttempts    = 3
	maxResponseBytes   = 4 << 20
)

// Options configures a Client. Token and BaseURL are required.
type Options struct {
	Token   string
	BaseURL string
	// HTTPClient must allow requests to last longer than PollTimeout.
	HTTPClient *http.Client
	// PollTimeout is how long Telegram holds a getUpdates request open.
	PollTimeout time.Duration
	// BaseDelay is the first retry delay; later retries back off from it.
	BaseDelay time.Duration
	Logger    *slog.Logger
}

// Client calls the Telegram Bot API. It is safe for concurrent use.
type Client struct {
	opts Options
}

// NewClient returns a Client, filling in defaults for optional settings.
func NewClient(opts Options) *Client {
	if opts.PollTimeout <= 0 {
		opts.PollTimeout = defaultPollTimeout
	}
	if opts.BaseDelay <= 0 {
		opts.BaseDelay = defaultBaseDelay
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: opts.PollTimeout + 10*time.Second}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	return &Client{opts: opts}
}

// allowedUpdates are the update kinds the bot asks Telegram for: messages,
// changes to its own membership, which keep its list of groups true,
// where it is an admin, changes to other members', which keep its list of
// each group's members true, button presses, inline queries, which
// Telegram sends only once inline mode is on in @BotFather, and the check
// before a payment for premium, without which Telegram takes none. Edited
// messages are guarded as new ones are, and join requests are answered
// with a button to press. A Telegram Business account connecting the bot,
// and its chats' messages, are what the bot answers customers from.
var allowedUpdates = []string{"message", "my_chat_member", "chat_member", "callback_query", "inline_query", "pre_checkout_query", "edited_message", "chat_join_request", "business_connection", "business_message", "chat_boost"}

type getUpdatesParams struct {
	Offset         int64    `json:"offset"`
	Timeout        int      `json:"timeout"`
	AllowedUpdates []string `json:"allowed_updates"`
}

// GetUpdates long-polls for updates with an ID of at least offset.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]Update, error) {
	params := getUpdatesParams{
		Offset:         offset,
		Timeout:        timeoutSeconds,
		AllowedUpdates: allowedUpdates,
	}
	var updates []Update
	if err := c.call(ctx, "getUpdates", params, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// GetMe returns the bot's own account. Its ID and username are what
// mentions of the bot and replies to it are matched against.
func (c *Client) GetMe(ctx context.Context) (User, error) {
	var me User
	if err := c.call(ctx, "getMe", struct{}{}, &me); err != nil {
		return User{}, err
	}
	return me, nil
}

type setWebhookParams struct {
	URL            string   `json:"url"`
	SecretToken    string   `json:"secret_token"`
	AllowedUpdates []string `json:"allowed_updates"`
}

// SetWebhook tells Telegram to deliver updates to url instead of holding
// them for getUpdates. Telegram sends secret back in a header with every
// update, which is how the receiver knows a request is genuine. While a
// webhook is set, long polling with the same token does not work.
func (c *Client) SetWebhook(ctx context.Context, url, secret string) error {
	params := setWebhookParams{URL: url, SecretToken: secret, AllowedUpdates: allowedUpdates}
	return c.call(ctx, "setWebhook", params, nil)
}

type webhookInfo struct {
	URL            string   `json:"url"`
	AllowedUpdates []string `json:"allowed_updates"`
}

// EnsureAllowedUpdates sets the webhook again, at the same address, when it
// was set before the bot asked for every kind of update it handles now,
// such as button presses: until then Telegram does not send them. It does
// nothing without a webhook.
func (c *Client) EnsureAllowedUpdates(ctx context.Context, secret string) (changed bool, err error) {
	var info webhookInfo
	if err := c.call(ctx, "getWebhookInfo", struct{}{}, &info); err != nil {
		return false, err
	}
	if info.URL == "" || sameKinds(info.AllowedUpdates, allowedUpdates) {
		return false, nil
	}
	return true, c.SetWebhook(ctx, info.URL, secret)
}

// sameKinds reports whether have asks for exactly the kinds in want, so
// that kinds the bot no longer handles, such as the quizzes' polls, stop
// coming too.
func sameKinds(have, want []string) bool {
	return len(have) == len(want) && !slices.ContainsFunc(want, func(w string) bool { return !slices.Contains(have, w) })
}

type answerCallbackParams struct {
	CallbackQueryID string `json:"callback_query_id"`
	Text            string `json:"text,omitempty"`
	ShowAlert       bool   `json:"show_alert,omitempty"`
}

// AnswerCallbackQuery ends a button press's spinner, showing text, as a
// box to close when alert is set and briefly otherwise.
func (c *Client) AnswerCallbackQuery(ctx context.Context, id, text string, alert bool) error {
	return c.call(ctx, "answerCallbackQuery", answerCallbackParams{CallbackQueryID: id, Text: text, ShowAlert: alert}, nil)
}

type editMessageTextParams struct {
	ChatID    int64  `json:"chat_id"`
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
}

// EditMessageText replaces the text of one of the bot's messages, and with
// it the buttons under it.
func (c *Client) EditMessageText(ctx context.Context, chatID, messageID int64, text string) error {
	return c.call(ctx, "editMessageText", editMessageTextParams{ChatID: chatID, MessageID: messageID, Text: text}, nil)
}

// DeleteWebhook removes the webhook, so that long polling works again.
func (c *Client) DeleteWebhook(ctx context.Context) error {
	return c.call(ctx, "deleteWebhook", struct{}{}, nil)
}

// ErrFileTooLarge means a file is bigger than the caller allowed.
var ErrFileTooLarge = errors.New("telegram: file is too large")

type getFileParams struct {
	FileID string `json:"file_id"`
}

type file struct {
	FileSize int64  `json:"file_size"`
	FilePath string `json:"file_path"`
}

// DownloadFile fetches a file sent to the bot, such as a photo or a voice
// message. Files larger than maxBytes are refused with ErrFileTooLarge,
// before or while downloading.
func (c *Client) DownloadFile(ctx context.Context, fileID string, maxBytes int64) ([]byte, error) {
	var info file
	if err := c.call(ctx, "getFile", getFileParams{FileID: fileID}, &info); err != nil {
		return nil, err
	}
	if info.FileSize > maxBytes {
		return nil, ErrFileTooLarge
	}
	return c.download(ctx, info.FilePath, maxBytes)
}

func (c *Client) download(ctx context.Context, path string, maxBytes int64) ([]byte, error) {
	endpoint := c.opts.BaseURL + "/file/bot" + c.opts.Token + "/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("telegram download: build request")
	}
	resp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram download: %w", withoutURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram download: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("telegram download: %w", withoutURL(err))
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrFileTooLarge
	}
	return data, nil
}

// CaptionLimit is the longest photo caption Telegram accepts.
const CaptionLimit = 1024

// Photo is a picture to send.
type Photo struct {
	ChatID           int64
	ReplyToMessageID int64
	Data             []byte
	Caption          string
	// BusinessConnectionID, when set, sends it on behalf of that
	// connection's business account.
	BusinessConnectionID string
	// Keyboard, in a private chat, puts the keyboard back with the picture,
	// as Outgoing's does.
	Keyboard [][]string
}

// SendPhoto uploads a picture and returns the ID of the sent message. A
// caption longer than Telegram allows is cut.
func (c *Client) SendPhoto(ctx context.Context, photo Photo) (int64, error) {
	fields := map[string]string{}
	if caption := strings.TrimSpace(photo.Caption); caption != "" {
		text, entities := formatted(cutRunes(caption, CaptionLimit))
		fields["caption"] = text
		if len(entities) > 0 {
			encoded, err := json.Marshal(entities)
			if err != nil {
				return 0, fmt.Errorf("telegram sendPhoto: encode: %w", err)
			}
			fields["caption_entities"] = string(encoded)
		}
	}
	fields["business_connection_id"] = photo.BusinessConnectionID // left out when ""
	if len(photo.Keyboard) > 0 {
		encoded, err := json.Marshal(markup(Outgoing{Keyboard: photo.Keyboard}))
		if err != nil {
			return 0, fmt.Errorf("telegram sendPhoto: encode: %w", err)
		}
		fields["reply_markup"] = string(encoded)
	}
	return c.upload(ctx, "sendPhoto", upload{
		chatID: photo.ChatID, replyTo: photo.ReplyToMessageID, fields: fields,
		field: "photo", filename: "image.png", data: photo.Data,
	})
}

// Track is a sound to send as music, shown with its title and performer.
type Track struct {
	ChatID           int64
	ReplyToMessageID int64
	Data             []byte
	Title            string
	Performer        string
}

// SendAudio uploads a track and returns the ID of the sent message. It is
// uploaded rather than given as a link because Telegram names a fetched
// file after its URL and ignores the title and performer.
func (c *Client) SendAudio(ctx context.Context, track Track) (int64, error) {
	name := strings.TrimSpace(track.Performer + " - " + track.Title)
	return c.upload(ctx, "sendAudio", upload{
		chatID: track.ChatID, replyTo: track.ReplyToMessageID,
		fields: map[string]string{"title": track.Title, "performer": track.Performer},
		field:  "audio", filename: strings.Trim(name, "- ") + ".mp3", data: track.Data,
	})
}

// File is a document to upload, such as a report, with an optional
// caption.
type File struct {
	ChatID           int64
	ReplyToMessageID int64
	Data             []byte
	Name             string
	Caption          string
}

// SendFile uploads a document and returns the ID of the sent message.
func (c *Client) SendFile(ctx context.Context, file File) (int64, error) {
	fields := map[string]string{}
	if file.Caption != "" {
		fields["caption"] = file.Caption
	}
	return c.upload(ctx, "sendDocument", upload{
		chatID: file.ChatID, replyTo: file.ReplyToMessageID, fields: fields,
		field: "document", filename: file.Name, data: file.Data,
	})
}

// Clip is a short video to upload: shown round, as a video message, or as
// a silent looping animation.
type Clip struct {
	ChatID           int64
	ReplyToMessageID int64
	Data             []byte
	Round            bool
}

// SendClip uploads a clip and returns the ID of the sent message. A round
// clip must be square: Telegram crops a video message to a circle.
func (c *Client) SendClip(ctx context.Context, clip Clip) (int64, error) {
	u := upload{chatID: clip.ChatID, replyTo: clip.ReplyToMessageID, field: "animation", filename: "clip.mp4", data: clip.Data}
	method := "sendAnimation"
	if clip.Round {
		u.field, method = "video_note", "sendVideoNote"
	}
	return c.upload(ctx, method, u)
}

// upload is one file sent as a multipart form.
type upload struct {
	chatID, replyTo int64
	fields          map[string]string
	field, filename string
	data            []byte
}

func (c *Client) upload(ctx context.Context, method string, u upload) (int64, error) {
	body, contentType, err := uploadForm(u)
	if err != nil {
		return 0, fmt.Errorf("telegram %s: encode: %w", method, err)
	}
	var sent sentMessage
	if err := c.postForm(ctx, method, body, contentType, &sent); err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

// postForm sends a multipart form to method and decodes its result.
func (c *Client) postForm(ctx context.Context, method string, body io.Reader, contentType string, result any) error {
	endpoint := c.opts.BaseURL + "/bot" + c.opts.Token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("telegram %s: build request", method)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, withoutURL(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("telegram %s: read response: %w", method, err)
	}
	return decode(method, resp.StatusCode, raw, result)
}

func uploadForm(u upload) (io.Reader, string, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	// A sticker's file is uploaded for a user, in no chat.
	fields := map[string]string{}
	if u.chatID != 0 {
		fields["chat_id"] = strconv.FormatInt(u.chatID, 10)
	}
	for name, value := range u.fields {
		if value != "" {
			fields[name] = value
		}
	}
	if reply := replyTo(u.replyTo); reply != nil {
		encoded, err := json.Marshal(reply)
		if err != nil {
			return nil, "", err
		}
		fields["reply_parameters"] = string(encoded)
	}
	for name, value := range fields {
		if err := form.WriteField(name, value); err != nil {
			return nil, "", err
		}
	}
	file, err := form.CreateFormFile(u.field, u.filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := file.Write(u.data); err != nil {
		return nil, "", err
	}
	if err := form.Close(); err != nil {
		return nil, "", err
	}
	return &body, form.FormDataContentType(), nil
}

func cutRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}

type chatMemberParams struct {
	ChatID int64 `json:"chat_id"`
	UserID int64 `json:"user_id"`
}

// GetChatMember returns a user's standing in a chat, which tells whether
// they administer it.
func (c *Client) GetChatMember(ctx context.Context, chatID, userID int64) (ChatMember, error) {
	var member ChatMember
	if err := c.call(ctx, "getChatMember", chatMemberParams{ChatID: chatID, UserID: userID}, &member); err != nil {
		return ChatMember{}, err
	}
	return member, nil
}

type channelMemberParams struct {
	ChatID string `json:"chat_id"`
	UserID int64  `json:"user_id"`
}

// ChannelMember returns a user's standing in a channel named by its
// @username or ID, which the bot must administer to ask.
func (c *Client) ChannelMember(ctx context.Context, channel string, userID int64) (ChatMember, error) {
	var member ChatMember
	if err := c.call(ctx, "getChatMember", channelMemberParams{ChatID: channel, UserID: userID}, &member); err != nil {
		return ChatMember{}, err
	}
	return member, nil
}

// GetChat returns a chat's current details, such as a group's title.
func (c *Client) GetChat(ctx context.Context, chatID int64) (Chat, error) {
	var chat Chat
	if err := c.call(ctx, "getChat", chatParams{ChatID: chatID}, &chat); err != nil {
		return Chat{}, err
	}
	return chat, nil
}

type chatParams struct {
	ChatID int64 `json:"chat_id"`
}

// GetChatMemberCount returns how many members a chat has. It fails when the
// bot is no longer in the chat.
func (c *Client) GetChatMemberCount(ctx context.Context, chatID int64) (int, error) {
	var count int
	if err := c.call(ctx, "getChatMemberCount", chatParams{ChatID: chatID}, &count); err != nil {
		return 0, err
	}
	return count, nil
}

// GetChatAdministrators returns a group's creator and administrators. Unlike
// the full member list, which no bot can read, any member may ask for it.
func (c *Client) GetChatAdministrators(ctx context.Context, chatID int64) ([]ChatMember, error) {
	var admins []ChatMember
	if err := c.call(ctx, "getChatAdministrators", chatParams{ChatID: chatID}, &admins); err != nil {
		return nil, err
	}
	return admins, nil
}

type stickerSetParams struct {
	Name string `json:"name"`
}

// GetStickerSet reads a sticker pack by its name, the last part of its
// t.me/addstickers link.
func (c *Client) GetStickerSet(ctx context.Context, name string) (StickerSet, error) {
	var set StickerSet
	if err := c.call(ctx, "getStickerSet", stickerSetParams{Name: name}, &set); err != nil {
		return StickerSet{}, err
	}
	return set, nil
}

type sendStickerParams struct {
	ChatID          int64            `json:"chat_id"`
	Sticker         string           `json:"sticker"`
	ReplyParameters *replyParameters `json:"reply_parameters,omitempty"`
}

// SendSticker sends a sticker Telegram knows by fileID, as a reply to
// replyToMessageID when it is not zero, and returns the sent message's ID.
func (c *Client) SendSticker(ctx context.Context, chatID int64, fileID string, replyToMessageID int64) (int64, error) {
	var sent sentMessage
	params := sendStickerParams{ChatID: chatID, Sticker: fileID, ReplyParameters: replyTo(replyToMessageID)}
	if err := c.call(ctx, "sendSticker", params, &sent); err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

type sendAnimationParams struct {
	ChatID          int64            `json:"chat_id"`
	Animation       string           `json:"animation"`
	ReplyParameters *replyParameters `json:"reply_parameters,omitempty"`
}

// SendAnimation sends a GIF, given by an HTTPS link Telegram fetches or by
// a file ID, as a reply to replyToMessageID when it is not zero, and returns
// the sent message's ID.
func (c *Client) SendAnimation(ctx context.Context, chatID int64, animation string, replyToMessageID int64) (int64, error) {
	var sent sentMessage
	params := sendAnimationParams{ChatID: chatID, Animation: animation, ReplyParameters: replyTo(replyToMessageID)}
	if err := c.call(ctx, "sendAnimation", params, &sent); err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

// Venue is a place to send as a pin on the map, under its name.
type Venue struct {
	ChatID           int64
	ReplyToMessageID int64
	Latitude         float64
	Longitude        float64
	Title            string
	Address          string
}

type sendVenueParams struct {
	ChatID          int64            `json:"chat_id"`
	Latitude        float64          `json:"latitude"`
	Longitude       float64          `json:"longitude"`
	Title           string           `json:"title"`
	Address         string           `json:"address"`
	ReplyParameters *replyParameters `json:"reply_parameters,omitempty"`
}

// SendVenue sends a pin on the map with the place's name and address, and
// returns the sent message's ID.
func (c *Client) SendVenue(ctx context.Context, v Venue) (int64, error) {
	var sent sentMessage
	params := sendVenueParams{ChatID: v.ChatID, Latitude: v.Latitude, Longitude: v.Longitude, Title: v.Title, Address: v.Address, ReplyParameters: replyTo(v.ReplyToMessageID)}
	if err := c.call(ctx, "sendVenue", params, &sent); err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

type sendMessageParams struct {
	BusinessConnectionID string           `json:"business_connection_id,omitempty"`
	ChatID               int64            `json:"chat_id"`
	Text                 string           `json:"text"`
	Entities             []messageEntity  `json:"entities,omitempty"`
	ReplyParameters      *replyParameters `json:"reply_parameters,omitempty"`
	// ReplyMarkup is an *inlineKeyboard or a *replyKeyboard.
	ReplyMarkup     any    `json:"reply_markup,omitempty"`
	MessageEffectID string `json:"message_effect_id,omitempty"`
}

type inlineKeyboard struct {
	InlineKeyboard [][]Button `json:"inline_keyboard"`
}

// replyKeyboard replaces the phone's keyboard with buttons that send their
// text. It stays until another replaces it, shrunk to fit the buttons.
type replyKeyboard struct {
	Keyboard       [][]keyboardButton `json:"keyboard"`
	IsPersistent   bool               `json:"is_persistent"`
	ResizeKeyboard bool               `json:"resize_keyboard"`
}

type keyboardButton struct {
	Text string `json:"text"`
}

// forceReply opens the reply box on the message, as if the user had chosen
// to reply to it, with a hint in the box.
type forceReply struct {
	ForceReply            bool   `json:"force_reply"`
	InputFieldPlaceholder string `json:"input_field_placeholder,omitempty"`
}

// markup is what goes under msg's first message: its buttons, else its
// keyboard, else nothing.
func markup(msg Outgoing) any {
	switch {
	case len(msg.Buttons) > 0:
		return &inlineKeyboard{InlineKeyboard: msg.Buttons}
	case msg.Ask != "":
		return &forceReply{ForceReply: true, InputFieldPlaceholder: msg.Ask}
	case len(msg.Keyboard) > 0:
		rows := make([][]keyboardButton, len(msg.Keyboard))
		for i, row := range msg.Keyboard {
			for _, text := range row {
				rows[i] = append(rows[i], keyboardButton{Text: text})
			}
		}
		return &replyKeyboard{Keyboard: rows, IsPersistent: true, ResizeKeyboard: true}
	}
	return nil
}

type replyParameters struct {
	MessageID int64 `json:"message_id"`
	// AllowSendingWithoutReply sends the message as an ordinary one when the
	// original was deleted in the meantime, instead of failing.
	AllowSendingWithoutReply bool `json:"allow_sending_without_reply"`
}

type sentMessage struct {
	MessageID int64 `json:"message_id"`
}

// SendMessage sends text and returns the ID of the message Telegram
// created. Text with headings, lists, tables or quotations goes as a rich
// message, which Telegram renders as such; when Telegram refuses it, it goes
// as ordinary text. Text longer than Telegram allows is split into several messages;
// only the first is a reply, and its ID is the one returned. Text is sent
// without a parse mode, so that stray Markdown or HTML in a model reply can
// never make the send fail; its bold, code and bullets become entities
// (see formatted).
func (c *Client) SendMessage(ctx context.Context, msg Outgoing) (int64, error) {
	// A rich message carries no buttons, keyboard or effect.
	if isRich(msg.Text) && len(msg.Buttons) == 0 && len(msg.Keyboard) == 0 && msg.Ask == "" && msg.Effect == "" {
		id, err := c.sendRich(ctx, msg)
		var refused *APIError
		// Telegram answered no, so nothing was sent: the plain message
		// follows. Without an answer the rich one may be in the chat.
		if err == nil || !errors.As(err, &refused) || refused.Code == http.StatusTooManyRequests {
			return id, err
		}
	}
	var firstID int64
	reply, buttons, effect := replyTo(msg.ReplyToMessageID), markup(msg), msg.Effect
	for _, chunk := range Split(msg.Text, MessageLimit) {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		text, entities := formatted(chunk)
		id, err := c.sendChunk(ctx, sendMessageParams{BusinessConnectionID: msg.BusinessConnectionID, ChatID: msg.ChatID, Text: text, Entities: entities, ReplyParameters: reply, ReplyMarkup: buttons, MessageEffectID: effect})
		if err != nil {
			return firstID, err
		}
		if firstID == 0 {
			firstID = id
		}
		reply, buttons, effect = nil, nil, ""
	}
	return firstID, nil
}

// sendChunk sends one message, and sends it again without its effect when
// Telegram refuses it with one: an effect is a flourish, which Telegram
// plays only in private chats and only from the list it keeps.
func (c *Client) sendChunk(ctx context.Context, params sendMessageParams) (int64, error) {
	id, err := c.sendWithRetry(ctx, params)
	var refused *APIError
	if params.MessageEffectID != "" && errors.As(err, &refused) && refused.Code == http.StatusBadRequest {
		params.MessageEffectID = ""
		return c.sendWithRetry(ctx, params)
	}
	return id, err
}

func replyTo(messageID int64) *replyParameters {
	if messageID == 0 {
		return nil
	}
	return &replyParameters{MessageID: messageID, AllowSendingWithoutReply: true}
}

func (c *Client) sendWithRetry(ctx context.Context, params sendMessageParams) (int64, error) {
	for attempt := 1; ; attempt++ {
		var sent sentMessage
		err := c.call(ctx, "sendMessage", params, &sent)
		if err == nil || attempt >= maxSendAttempts || !isRateLimited(err) {
			return sent.MessageID, err
		}
		if err := sleep(ctx, c.retryDelay(attempt, err)); err != nil {
			return 0, err
		}
	}
}

type copyMessageParams struct {
	ChatID     int64 `json:"chat_id"`
	FromChatID int64 `json:"from_chat_id"`
	MessageID  int64 `json:"message_id"`
}

// CopyMessage sends a copy of a message, of any kind, to a chat, without the
// "forwarded from" line, and returns the copy's ID.
func (c *Client) CopyMessage(ctx context.Context, chatID, fromChatID, messageID int64) (int64, error) {
	var sent sentMessage
	if err := c.call(ctx, "copyMessage", copyMessageParams{ChatID: chatID, FromChatID: fromChatID, MessageID: messageID}, &sent); err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

type reactionParams struct {
	ChatID    int64          `json:"chat_id"`
	MessageID int64          `json:"message_id"`
	Reaction  []reactionType `json:"reaction"`
}

type reactionType struct {
	Type  string `json:"type"`
	Emoji string `json:"emoji"`
}

// SetMessageReaction replaces the bot's reaction to a message with one
// emoji, which must be one of ReactionEmoji. An empty emoji removes the
// bot's reaction.
func (c *Client) SetMessageReaction(ctx context.Context, chatID, messageID int64, emoji string) error {
	params := reactionParams{ChatID: chatID, MessageID: messageID, Reaction: []reactionType{}}
	if emoji != "" {
		params.Reaction = append(params.Reaction, reactionType{Type: "emoji", Emoji: emoji})
	}
	return c.call(ctx, "setMessageReaction", params, nil)
}

type chatActionParams struct {
	ChatID          int64  `json:"chat_id"`
	MessageThreadID int64  `json:"message_thread_id,omitempty"`
	Action          string `json:"action"`
}

// SendChatAction shows a status such as "typing" in the chat, in the forum
// topic threadID unless it is 0. Telegram clears it after about 5 seconds,
// or as soon as the bot sends a message.
func (c *Client) SendChatAction(ctx context.Context, chatID, threadID int64, action string) error {
	return c.call(ctx, "sendChatAction", chatActionParams{ChatID: chatID, MessageThreadID: threadID, Action: action}, nil)
}

type messageParams struct {
	ChatID    int64 `json:"chat_id"`
	MessageID int64 `json:"message_id"`
}

// DeleteMessage deletes a message. Telegram refuses messages older than 48
// hours, which comes back as an *APIError.
func (c *Client) DeleteMessage(ctx context.Context, chatID, messageID int64) error {
	return c.call(ctx, "deleteMessage", messageParams{ChatID: chatID, MessageID: messageID}, nil)
}

type restrictParams struct {
	ChatID      int64           `json:"chat_id"`
	UserID      int64           `json:"user_id"`
	Permissions ChatPermissions `json:"permissions"`
	// IndependentPermissions makes Telegram apply each permission as given,
	// instead of deriving some from others.
	IndependentPermissions bool  `json:"use_independent_chat_permissions"`
	UntilDate              int64 `json:"until_date,omitempty"`
}

// RestrictChatMember sets what a member of a supergroup may do: mute them
// with NoPermissions, or lift a mute with AllPermissions.
func (c *Client) RestrictChatMember(ctx context.Context, r Restriction) error {
	params := restrictParams{
		ChatID:                 r.ChatID,
		UserID:                 r.UserID,
		Permissions:            r.Permissions,
		IndependentPermissions: true,
	}
	if !r.Until.IsZero() {
		params.UntilDate = r.Until.Unix()
	}
	return c.call(ctx, "restrictChatMember", params, nil)
}

// SetChatPermissions sets what every member of a group may do by default.
func (c *Client) SetChatPermissions(ctx context.Context, chatID int64, permissions ChatPermissions) error {
	params := chatPermissionsParams{ChatID: chatID, Permissions: permissions, IndependentPermissions: true}
	return c.call(ctx, "setChatPermissions", params, nil)
}

type chatPermissionsParams struct {
	ChatID                 int64           `json:"chat_id"`
	Permissions            ChatPermissions `json:"permissions"`
	IndependentPermissions bool            `json:"use_independent_chat_permissions"`
}

// SetChatTitle renames a group.
func (c *Client) SetChatTitle(ctx context.Context, chatID int64, title string) error {
	return c.call(ctx, "setChatTitle", chatTextParams{ChatID: chatID, Title: title}, nil)
}

// SetChatDescription sets a group's description; empty removes it.
func (c *Client) SetChatDescription(ctx context.Context, chatID int64, description string) error {
	return c.call(ctx, "setChatDescription", chatTextParams{ChatID: chatID, Description: description}, nil)
}

type chatTextParams struct {
	ChatID      int64  `json:"chat_id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
}

// SetChatPhoto sets a group's photo from a JPEG.
func (c *Client) SetChatPhoto(ctx context.Context, chatID int64, jpeg []byte) error {
	body, contentType, err := uploadForm(upload{chatID: chatID, field: "photo", filename: "photo.jpg", data: jpeg})
	if err != nil {
		return fmt.Errorf("telegram setChatPhoto: encode: %w", err)
	}
	var done bool
	return c.postForm(ctx, "setChatPhoto", body, contentType, &done)
}

// DeleteChatPhoto removes a group's photo.
func (c *Client) DeleteChatPhoto(ctx context.Context, chatID int64) error {
	return c.call(ctx, "deleteChatPhoto", chatParams{ChatID: chatID}, nil)
}

type stickerSetNameParams struct {
	ChatID         int64  `json:"chat_id"`
	StickerSetName string `json:"sticker_set_name"`
}

// SetChatStickerSet gives a supergroup a sticker pack, by its name, that
// every member may use there. Telegram allows it only where getChat says
// CanSetStickerSet.
func (c *Client) SetChatStickerSet(ctx context.Context, chatID int64, name string) error {
	return c.call(ctx, "setChatStickerSet", stickerSetNameParams{ChatID: chatID, StickerSetName: name}, nil)
}

// DeleteChatStickerSet takes a supergroup's sticker pack away.
func (c *Client) DeleteChatStickerSet(ctx context.Context, chatID int64) error {
	return c.call(ctx, "deleteChatStickerSet", chatParams{ChatID: chatID}, nil)
}

// InviteLink asks for a new invite link to a group.
type InviteLink struct {
	ChatID int64
	// Name is shown to admins in the group's list of links.
	Name string
	// Expires is when the link stops working; zero means never.
	Expires time.Time
	// MemberLimit is how many people may join through it, 1 to 99999; zero
	// means any number.
	MemberLimit int
	// JoinRequest makes each person who opens it ask the admins to approve
	// them. Telegram does not combine it with a MemberLimit.
	JoinRequest bool
}

// CreateChatInviteLink makes a new invite link and returns it.
func (c *Client) CreateChatInviteLink(ctx context.Context, l InviteLink) (string, error) {
	params := inviteLinkParams{ChatID: l.ChatID, Name: l.Name, MemberLimit: l.MemberLimit, JoinRequest: l.JoinRequest}
	if !l.Expires.IsZero() {
		params.ExpireDate = l.Expires.Unix()
	}
	var link struct {
		InviteLink string `json:"invite_link"`
	}
	if err := c.call(ctx, "createChatInviteLink", params, &link); err != nil {
		return "", err
	}
	return link.InviteLink, nil
}

type inviteLinkParams struct {
	ChatID      int64  `json:"chat_id"`
	Name        string `json:"name,omitempty"`
	ExpireDate  int64  `json:"expire_date,omitempty"`
	MemberLimit int    `json:"member_limit,omitempty"`
	JoinRequest bool   `json:"creates_join_request,omitempty"`
}

// EditChatInviteLink gives an invite link the bot made the name and limits
// of l, all of them: Telegram sets each anew, and one left out is none.
func (c *Client) EditChatInviteLink(ctx context.Context, link string, l InviteLink) error {
	params := editInviteLinkParams{ChatID: l.ChatID, InviteLink: link, Name: l.Name, MemberLimit: l.MemberLimit, JoinRequest: l.JoinRequest}
	if !l.Expires.IsZero() {
		params.ExpireDate = l.Expires.Unix()
	}
	return c.call(ctx, "editChatInviteLink", params, nil)
}

// editInviteLinkParams sends every field, zero included, so that an edit
// means exactly what it says whatever Telegram assumes for a missing one.
type editInviteLinkParams struct {
	ChatID      int64  `json:"chat_id"`
	InviteLink  string `json:"invite_link"`
	Name        string `json:"name"`
	ExpireDate  int64  `json:"expire_date"`
	MemberLimit int    `json:"member_limit"`
	JoinRequest bool   `json:"creates_join_request"`
}

// RevokeChatInviteLink stops an invite link the bot made from working.
func (c *Client) RevokeChatInviteLink(ctx context.Context, chatID int64, link string) error {
	return c.call(ctx, "revokeChatInviteLink", revokeLinkParams{ChatID: chatID, InviteLink: link}, nil)
}

type revokeLinkParams struct {
	ChatID     int64  `json:"chat_id"`
	InviteLink string `json:"invite_link"`
}

// UnpinAllChatMessages unpins every pinned message of a group.
func (c *Client) UnpinAllChatMessages(ctx context.Context, chatID int64) error {
	return c.call(ctx, "unpinAllChatMessages", chatParams{ChatID: chatID}, nil)
}

// AdminRights are the rights a member is made an admin with; all false
// takes their adminship away.
type AdminRights struct {
	CanDeleteMessages  bool `json:"can_delete_messages"`
	CanRestrictMembers bool `json:"can_restrict_members"`
	CanPinMessages     bool `json:"can_pin_messages"`
	CanInviteUsers     bool `json:"can_invite_users"`
}

// PromoteChatMember makes a member an admin with rights, or with none, an
// admin a member again. A bot can only give rights it holds, and take them
// only from admins it made.
func (c *Client) PromoteChatMember(ctx context.Context, chatID, userID int64, rights AdminRights) error {
	return c.call(ctx, "promoteChatMember", promoteParams{ChatID: chatID, UserID: userID, AdminRights: rights}, nil)
}

type promoteParams struct {
	ChatID int64 `json:"chat_id"`
	UserID int64 `json:"user_id"`
	AdminRights
}

type memberTagParams struct {
	ChatID int64  `json:"chat_id"`
	UserID int64  `json:"user_id"`
	Tag    string `json:"tag"`
}

// SetChatMemberTag sets the tag shown by a regular member's name, 0 to 16
// characters without emoji; an empty one takes it off. It needs the
// can_manage_tags right.
func (c *Client) SetChatMemberTag(ctx context.Context, chatID, userID int64, tag string) error {
	return c.call(ctx, "setChatMemberTag", memberTagParams{ChatID: chatID, UserID: userID, Tag: tag}, nil)
}

// SetChatAdministratorCustomTitle sets the title shown for an admin the bot
// made.
func (c *Client) SetChatAdministratorCustomTitle(ctx context.Context, chatID, userID int64, title string) error {
	return c.call(ctx, "setChatAdministratorCustomTitle", customTitleParams{ChatID: chatID, UserID: userID, CustomTitle: title}, nil)
}

type customTitleParams struct {
	ChatID      int64  `json:"chat_id"`
	UserID      int64  `json:"user_id"`
	CustomTitle string `json:"custom_title"`
}

// BanChatMember removes a member from a group. In a supergroup they cannot
// rejoin until unbanned; in a basic group an invite link still lets them back.
func (c *Client) BanChatMember(ctx context.Context, chatID, userID int64) error {
	return c.call(ctx, "banChatMember", chatMemberParams{ChatID: chatID, UserID: userID}, nil)
}

type unbanParams struct {
	ChatID int64 `json:"chat_id"`
	UserID int64 `json:"user_id"`
	// OnlyIfBanned keeps the call harmless for someone who is not banned:
	// without it Telegram removes a current member from the group.
	OnlyIfBanned bool `json:"only_if_banned"`
}

// UnbanChatMember lets a banned user join the group again. It does nothing
// to a user who is not banned.
func (c *Client) UnbanChatMember(ctx context.Context, chatID, userID int64) error {
	return c.call(ctx, "unbanChatMember", unbanParams{ChatID: chatID, UserID: userID, OnlyIfBanned: true}, nil)
}

type pinParams struct {
	ChatID              int64 `json:"chat_id"`
	MessageID           int64 `json:"message_id"`
	DisableNotification bool  `json:"disable_notification"`
}

// PinChatMessage pins a message without notifying the members: a pin asked
// for in passing should not ping the whole group.
func (c *Client) PinChatMessage(ctx context.Context, chatID, messageID int64) error {
	return c.call(ctx, "pinChatMessage", pinParams{ChatID: chatID, MessageID: messageID, DisableNotification: true}, nil)
}

// UnpinChatMessage unpins one pinned message.
func (c *Client) UnpinChatMessage(ctx context.Context, chatID, messageID int64) error {
	return c.call(ctx, "unpinChatMessage", messageParams{ChatID: chatID, MessageID: messageID}, nil)
}

type forumTopicParams struct {
	ChatID          int64  `json:"chat_id"`
	MessageThreadID int64  `json:"message_thread_id,omitempty"`
	Name            string `json:"name,omitempty"`
	IconColor       int    `json:"icon_color,omitempty"`
}

type forumTopic struct {
	MessageThreadID int64 `json:"message_thread_id"`
}

// CreateForumTopic opens a topic in a forum supergroup, its icon in one of
// the colours Telegram allows (zero leaves the choice to Telegram), and
// returns its ID: the message_thread_id of the messages in it.
func (c *Client) CreateForumTopic(ctx context.Context, chatID int64, name string, iconColor int) (int64, error) {
	var topic forumTopic
	if err := c.call(ctx, "createForumTopic", forumTopicParams{ChatID: chatID, Name: name, IconColor: iconColor}, &topic); err != nil {
		return 0, err
	}
	return topic.MessageThreadID, nil
}

// EditForumTopic renames a topic. Its icon, not sent, stays as it is.
func (c *Client) EditForumTopic(ctx context.Context, chatID, threadID int64, name string) error {
	return c.call(ctx, "editForumTopic", forumTopicParams{ChatID: chatID, MessageThreadID: threadID, Name: name}, nil)
}

// CloseForumTopic closes a topic: only admins can write in it until it is
// reopened.
func (c *Client) CloseForumTopic(ctx context.Context, chatID, threadID int64) error {
	return c.call(ctx, "closeForumTopic", forumTopicParams{ChatID: chatID, MessageThreadID: threadID}, nil)
}

// ReopenForumTopic opens a closed topic again.
func (c *Client) ReopenForumTopic(ctx context.Context, chatID, threadID int64) error {
	return c.call(ctx, "reopenForumTopic", forumTopicParams{ChatID: chatID, MessageThreadID: threadID}, nil)
}

// DeleteForumTopic deletes a topic with every message in it.
func (c *Client) DeleteForumTopic(ctx context.Context, chatID, threadID int64) error {
	return c.call(ctx, "deleteForumTopic", forumTopicParams{ChatID: chatID, MessageThreadID: threadID}, nil)
}

// HideGeneralForumTopic hides a forum's General topic, which Telegram also
// closes when it is open.
func (c *Client) HideGeneralForumTopic(ctx context.Context, chatID int64) error {
	return c.call(ctx, "hideGeneralForumTopic", chatParams{ChatID: chatID}, nil)
}

// UnhideGeneralForumTopic shows a forum's General topic again.
func (c *Client) UnhideGeneralForumTopic(ctx context.Context, chatID int64) error {
	return c.call(ctx, "unhideGeneralForumTopic", chatParams{ChatID: chatID}, nil)
}

type sendPollParams struct {
	ChatID                int64             `json:"chat_id"`
	Question              string            `json:"question"`
	Options               []inputPollOption `json:"options"`
	IsAnonymous           bool              `json:"is_anonymous"`
	AllowsMultipleAnswers bool              `json:"allows_multiple_answers"`
	Type                  string            `json:"type,omitempty"`
	CorrectOptionID       *int              `json:"correct_option_id,omitempty"`
	Explanation           string            `json:"explanation,omitempty"`
	ReplyParameters       *replyParameters  `json:"reply_parameters,omitempty"`
	OpenPeriod            int               `json:"open_period,omitempty"`
}

type inputPollOption struct {
	Text string `json:"text"`
}

// SendPoll sends a poll or a quiz and returns the ID of the sent message.
// Polls in groups are anonymous, so the bot never learns or reveals who
// voted for what; in a private chat the only voter is the person the bot
// talks to.
func (c *Client) SendPoll(ctx context.Context, poll OutgoingPoll) (int64, error) {
	sent, err := c.sendPoll(ctx, poll)
	return sent.MessageID, err
}

type sentPoll struct {
	MessageID int64 `json:"message_id"`
}

func (c *Client) sendPoll(ctx context.Context, poll OutgoingPoll) (sentPoll, error) {
	params := sendPollParams{
		ChatID:                poll.ChatID,
		Question:              poll.Question,
		Options:               make([]inputPollOption, 0, len(poll.Options)),
		IsAnonymous:           poll.ChatID < 0,
		AllowsMultipleAnswers: poll.Multiple,
		OpenPeriod:            poll.OpenPeriod,
	}
	for _, option := range poll.Options {
		params.Options = append(params.Options, inputPollOption{Text: option})
	}
	if poll.Quiz {
		params.Type, params.CorrectOptionID, params.Explanation = "quiz", &poll.Correct, poll.Explanation
		params.AllowsMultipleAnswers = false
	}
	if poll.ReplyToMessageID != 0 {
		params.ReplyParameters = &replyParameters{MessageID: poll.ReplyToMessageID, AllowSendingWithoutReply: true}
	}
	var sent sentPoll
	if err := c.call(ctx, "sendPoll", params, &sent); err != nil {
		return sentPoll{}, err
	}
	return sent, nil
}

// StopPoll closes a poll the bot sent, by its message's ID, and returns it
// with its final counts.
func (c *Client) StopPoll(ctx context.Context, chatID, messageID int64) (Poll, error) {
	var poll Poll
	if err := c.call(ctx, "stopPoll", messageParams{ChatID: chatID, MessageID: messageID}, &poll); err != nil {
		return Poll{}, err
	}
	return poll, nil
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (c *Client) call(ctx context.Context, method string, params, result any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("telegram %s: encode: %w", method, err)
	}
	endpoint := c.opts.BaseURL + "/bot" + c.opts.Token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram %s: build request", method)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, withoutURL(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("telegram %s: read response: %w", method, err)
	}
	return decode(method, resp.StatusCode, raw, result)
}

func decode(method string, status int, raw []byte, result any) error {
	var parsed apiResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return &APIError{Code: status, Description: "unreadable response from " + method}
	}
	if !parsed.OK {
		return &APIError{
			Code:        parsed.ErrorCode,
			Description: parsed.Description,
			RetryAfter:  time.Duration(parsed.Parameters.RetryAfter) * time.Second,
		}
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(parsed.Result, result); err != nil {
		return fmt.Errorf("telegram %s: decode result: %w", method, err)
	}
	return nil
}

// withoutURL drops the request URL from a transport error. The URL contains
// the bot token, which must never reach logs.
func withoutURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

func isRateLimited(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusTooManyRequests
}

func (c *Client) retryDelay(attempt int, err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	return c.opts.BaseDelay << (attempt - 1)
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type sendRichMessageParams struct {
	BusinessConnectionID string           `json:"business_connection_id,omitempty"`
	ChatID               int64            `json:"chat_id"`
	RichMessage          inputRichMessage `json:"rich_message"`
	ReplyParameters      *replyParameters `json:"reply_parameters,omitempty"`
}

type inputRichMessage struct {
	Markdown string `json:"markdown"`
}

// sendRich sends msg's text as a rich message, written in Telegram's rich
// Markdown, whole: a rich message holds far more than an ordinary one.
func (c *Client) sendRich(ctx context.Context, msg Outgoing) (int64, error) {
	var sent sentMessage
	params := sendRichMessageParams{BusinessConnectionID: msg.BusinessConnectionID, ChatID: msg.ChatID, RichMessage: inputRichMessage{Markdown: msg.Text}, ReplyParameters: replyTo(msg.ReplyToMessageID)}
	if err := c.call(ctx, "sendRichMessage", params, &sent); err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

// BotCommand is a command shown in the menu Telegram opens on "/".
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// Command menu scopes: private chats and any chat without its own list,
// every group's members, and every group's administrators.
const (
	ScopeDefault       = "default"
	ScopeAllGroupChats = "all_group_chats"
	ScopeAllChatAdmins = "all_chat_administrators"
)

type setMyCommandsParams struct {
	Commands []BotCommand `json:"commands"`
	Scope    struct {
		Type   string `json:"type"`
		ChatID int64  `json:"chat_id,omitempty"`
		UserID int64  `json:"user_id,omitempty"`
	} `json:"scope"`
}

// SetMyCommands sets the command menu for scope.
func (c *Client) SetMyCommands(ctx context.Context, scope string, commands []BotCommand) error {
	params := setMyCommandsParams{Commands: commands}
	params.Scope.Type = scope
	var ok bool
	return c.call(ctx, "setMyCommands", params, &ok)
}

// AdministratorRights are the rights the bot asks for when someone makes it
// an admin of a group or a channel, ticked in Telegram's dialog for them to
// confirm. Every field is sent: Telegram needs them all, false included.
type AdministratorRights struct {
	IsAnonymous         bool `json:"is_anonymous"`
	CanManageChat       bool `json:"can_manage_chat"`
	CanDeleteMessages   bool `json:"can_delete_messages"`
	CanManageVideoChats bool `json:"can_manage_video_chats"`
	CanRestrictMembers  bool `json:"can_restrict_members"`
	CanPromoteMembers   bool `json:"can_promote_members"`
	CanChangeInfo       bool `json:"can_change_info"`
	CanInviteUsers      bool `json:"can_invite_users"`
	CanPostStories      bool `json:"can_post_stories"`
	CanEditStories      bool `json:"can_edit_stories"`
	CanDeleteStories    bool `json:"can_delete_stories"`
	CanPostMessages     bool `json:"can_post_messages"`
	CanEditMessages     bool `json:"can_edit_messages"`
	CanPinMessages      bool `json:"can_pin_messages"`
	CanManageTopics     bool `json:"can_manage_topics"`
	CanManageTags       bool `json:"can_manage_tags"`
}

type defaultRightsParams struct {
	Rights      AdministratorRights `json:"rights"`
	ForChannels bool                `json:"for_channels"`
}

// SetMyDefaultAdministratorRights sets the rights the bot asks for when made
// an admin of a group, or with forChannels of a channel.
func (c *Client) SetMyDefaultAdministratorRights(ctx context.Context, rights AdministratorRights, forChannels bool) error {
	var ok bool
	return c.call(ctx, "setMyDefaultAdministratorRights", defaultRightsParams{Rights: rights, ForChannels: forChannels}, &ok)
}

// SetMemberCommands sets the command menu one member sees in one group,
// over every other scope there.
func (c *Client) SetMemberCommands(ctx context.Context, chatID, userID int64, commands []BotCommand) error {
	params := setMyCommandsParams{Commands: commands}
	params.Scope.Type, params.Scope.ChatID, params.Scope.UserID = "chat_member", chatID, userID
	var ok bool
	return c.call(ctx, "setMyCommands", params, &ok)
}
