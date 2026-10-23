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
// changes to its own membership, which keep its list of groups true, and,
// where it is an admin, changes to other members', which keep its list of
// each group's members true.
var allowedUpdates = []string{"message", "my_chat_member", "chat_member"}

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
	endpoint := c.opts.BaseURL + "/bot" + c.opts.Token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return 0, fmt.Errorf("telegram %s: build request", method)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("telegram %s: %w", method, withoutURL(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, fmt.Errorf("telegram %s: read response: %w", method, err)
	}
	var sent sentMessage
	if err := decode(method, resp.StatusCode, raw, &sent); err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

func uploadForm(u upload) (io.Reader, string, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	fields := map[string]string{"chat_id": strconv.FormatInt(u.chatID, 10)}
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

type sendMessageParams struct {
	ChatID          int64            `json:"chat_id"`
	Text            string           `json:"text"`
	Entities        []messageEntity  `json:"entities,omitempty"`
	ReplyParameters *replyParameters `json:"reply_parameters,omitempty"`
	ReplyMarkup     *inlineKeyboard  `json:"reply_markup,omitempty"`
}

type inlineKeyboard struct {
	InlineKeyboard [][]Button `json:"inline_keyboard"`
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
	if isRich(msg.Text) && len(msg.Buttons) == 0 {
		id, err := c.sendRich(ctx, msg)
		var refused *APIError
		// Telegram answered no, so nothing was sent: the plain message
		// follows. Without an answer the rich one may be in the chat.
		if err == nil || !errors.As(err, &refused) || refused.Code == http.StatusTooManyRequests {
			return id, err
		}
	}
	var firstID int64
	reply := replyTo(msg.ReplyToMessageID)
	var buttons *inlineKeyboard
	if len(msg.Buttons) > 0 {
		buttons = &inlineKeyboard{InlineKeyboard: msg.Buttons}
	}
	for _, chunk := range Split(msg.Text, MessageLimit) {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		text, entities := formatted(chunk)
		id, err := c.sendChunk(ctx, sendMessageParams{ChatID: msg.ChatID, Text: text, Entities: entities, ReplyParameters: reply, ReplyMarkup: buttons})
		if err != nil {
			return firstID, err
		}
		if firstID == 0 {
			firstID = id
		}
		reply, buttons = nil, nil
	}
	return firstID, nil
}

func replyTo(messageID int64) *replyParameters {
	if messageID == 0 {
		return nil
	}
	return &replyParameters{MessageID: messageID, AllowSendingWithoutReply: true}
}

func (c *Client) sendChunk(ctx context.Context, params sendMessageParams) (int64, error) {
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
}

type inputPollOption struct {
	Text string `json:"text"`
}

// SendPoll sends a poll or a quiz and returns the ID of the sent message.
// Polls are anonymous, so the bot never learns or reveals who voted for
// what.
func (c *Client) SendPoll(ctx context.Context, poll OutgoingPoll) (int64, error) {
	params := sendPollParams{
		ChatID:                poll.ChatID,
		Question:              poll.Question,
		Options:               make([]inputPollOption, 0, len(poll.Options)),
		IsAnonymous:           true,
		AllowsMultipleAnswers: poll.Multiple,
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
	var sent sentMessage
	if err := c.call(ctx, "sendPoll", params, &sent); err != nil {
		return 0, err
	}
	return sent.MessageID, nil
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
	ChatID          int64            `json:"chat_id"`
	RichMessage     inputRichMessage `json:"rich_message"`
	ReplyParameters *replyParameters `json:"reply_parameters,omitempty"`
}

type inputRichMessage struct {
	Markdown string `json:"markdown"`
}

// sendRich sends msg's text as a rich message, written in Telegram's rich
// Markdown, whole: a rich message holds far more than an ordinary one.
func (c *Client) sendRich(ctx context.Context, msg Outgoing) (int64, error) {
	var sent sentMessage
	params := sendRichMessageParams{ChatID: msg.ChatID, RichMessage: inputRichMessage{Markdown: msg.Text}, ReplyParameters: replyTo(msg.ReplyToMessageID)}
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
		Type string `json:"type"`
	} `json:"scope"`
}

// SetMyCommands sets the command menu for scope.
func (c *Client) SetMyCommands(ctx context.Context, scope string, commands []BotCommand) error {
	params := setMyCommandsParams{Commands: commands}
	params.Scope.Type = scope
	var ok bool
	return c.call(ctx, "setMyCommands", params, &ok)
}
