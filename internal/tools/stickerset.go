package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"telegram-ai-bot/internal/stickers"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	// stickerSetTimeout allows for drawing the sticker first, as
	// generate_image does; the turn's own deadline still applies.
	stickerSetTimeout = imageTimeout
	// stickersPerDay is how many stickers each owner adds a day, owners
	// though they are: Telegram paces additions to a set, and a static set
	// holds 120.
	stickersPerDay = 20
	// learnTimeout bounds the model's look at a new sticker, and
	// learnMargin is the time it leaves the tool to answer. A sticker not
	// looked at now is looked at with the next one added.
	learnTimeout = 15 * time.Second
	learnMargin  = 3 * time.Second
	// ownSetPrefix starts the set's name, which Telegram requires to end in
	// "_by_" and the bot's username.
	ownSetPrefix = "maria"
	// stickerStyle asks the image model for a picture Drawn can cut out.
	stickerStyle = "\n\nDraw it as a single die-cut sticker: one subject, centred, bold clean outline, flat bright colours, on a plain solid white background with nothing else on it."
)

const (
	stickerSetOwnersOnly = "only my owners can change my sticker set"
	stickerNoPicture     = "there is no picture in the message: ask the owner to send the photo with the request or reply to one, or give a prompt to draw the sticker"
	stickerNoDrawing     = "drawing is turned off here, so the sticker cannot be drawn; ask for a photo instead"
	stickerUnreadable    = "this picture cannot be read; ask for it as a photo"
	stickerTooDetailed   = "this picture is too detailed to fit Telegram's 512 KB for a sticker; try another"
	stickerNotReplied    = "reply to the sticker to take out of the set, with the request"
	stickerNoSet         = "there is no sticker set yet: it is made with the first sticker added"
	stickerTitleLength   = "title must be 1 to 64 characters"
	stickerAddedNote     = "The new sticker is in the chat. Say in one short line, in their language, that it was added, with the set's link."
	stickerUnshownNote   = "Added, but it could not be shown here. Say so in one short line, with the set's link."
	stickerRemovedNote   = "Taken out of the set; Telegram may show it in the set for a few more minutes. Say so in one short line."
)

// StickerSetAPI makes and changes the bot's own sticker set on Telegram.
type StickerSetAPI interface {
	UploadStickerFile(ctx context.Context, userID int64, png []byte) (string, error)
	CreateNewStickerSet(ctx context.Context, userID int64, name, title string, first telegram.InputSticker) error
	AddStickerToSet(ctx context.Context, userID int64, name string, sticker telegram.InputSticker) error
	DeleteStickerFromSet(ctx context.Context, fileID string) error
	SetStickerSetTitle(ctx context.Context, name, title string) error
	GetStickerSet(ctx context.Context, name string) (telegram.StickerSet, error)
	SendSticker(ctx context.Context, chatID int64, fileID string, replyToMessageID int64) (int64, error)
}

// StickerSetStore remembers whose the set is, counts the stickers added and
// forgets the ones taken out.
type StickerSetStore interface {
	OwnStickerSetOwner(ctx context.Context, name string) (int64, error)
	SaveOwnStickerSet(ctx context.Context, name string, ownerID int64) error
	ReserveStickerUpload(ctx context.Context, userID, chatID int64, since time.Time, limit int) (int64, bool, error)
	ReleaseStickerUpload(ctx context.Context, id int64) error
	ForgetSticker(ctx context.Context, fileUniqueID string) error
}

// StickerLearner has the model look at a set's new stickers, so that the
// bot sends them too, as it does an imported pack's.
type StickerLearner interface {
	Import(ctx context.Context, name string) (stickers.Report, error)
}

// StickerSetOptions configures the sticker_set tool.
type StickerSetOptions struct {
	Telegram StickerSetAPI
	Store    StickerSetStore
	// Learner, when set, makes new stickers ones the bot sends.
	Learner StickerLearner
	// Drawer, when set, draws a sticker from a prompt.
	Drawer ImageGenerator
	// BotUsername ends the set's name.
	BotUsername string
	// Title is the set's title when it is made.
	Title    string
	Location *time.Location
	Now      func() time.Time
}

// OwnStickerSetName is the name of the bot's own set, which its link ends
// in.
func OwnStickerSetName(botUsername string) string {
	return ownSetPrefix + "_by_" + botUsername
}

type stickerSetArgs struct {
	Action string   `json:"action"`
	Emoji  []string `json:"emoji"`
	Prompt string   `json:"prompt"`
	Title  string   `json:"title"`
}

// StickerSetTool lets the bot's owners fill a sticker set of the bot's own
// from the chat: a photo they send or reply to, or a picture the bot draws,
// becomes a sticker in it, and a sticker of it they reply to is taken out.
// The set belongs on Telegram to the owner who made it, and every later
// sticker is added as them.
func StickerSetTool(opts StickerSetOptions) Tool {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	opts.Title = cutTitle(opts.Title)
	set := ownStickerSet{StickerSetOptions: opts, name: OwnStickerSetName(opts.BotUsername)}
	return Tool{
		Name: "sticker_set",
		Description: "Owners only: your own Telegram sticker set. " +
			"add: makes the picture in the owner's message, or in the one they replied to, a sticker in the set, or, with prompt, draws the sticker first; give 1 to 3 emoji for its mood. The new sticker is sent to the chat. " +
			"remove: takes out the sticker of the set the owner replied to. title: renames the set. link: the set's link, title and size. " +
			"Use it only when an owner asks (\"stiker qil\", \"stikerpakka qo'sh\", \"стикер сделай\"); never say a sticker was added unless the tool says so.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["add", "remove", "title", "link"]},
				"emoji": {"type": "array", "items": {"type": "string"}, "maxItems": 20, "description": "For add: the emoji the sticker stands for, each on its own, such as [\"😂\"]."},
				"prompt": {"type": "string", "description": "For add, only when the owner asks you to draw the sticker: what to draw, in English, with details. Leave it out to use the picture in the message."},
				"title": {"type": "string", "description": "For title: the set's new title, at most 64 characters."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		OwnerOnly: true,
		Timeout:   stickerSetTimeout,
		Run:       set.run,
	}
}

type ownStickerSet struct {
	StickerSetOptions
	name string
}

func (s ownStickerSet) run(ctx context.Context, raw json.RawMessage) (any, error) {
	var args stickerSetArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	// OwnerOnly already keeps others out; the set is checked in code too.
	caller, ok := CallerFrom(ctx)
	if !ok || !caller.Human || !caller.Owner || caller.ChatID == 0 {
		return nil, &DeniedError{Message: stickerSetOwnersOnly}
	}
	switch args.Action {
	case "add":
		return s.add(ctx, caller, args)
	case "remove":
		return s.remove(ctx, caller)
	case "title":
		return s.retitle(ctx, args.Title)
	case "link":
		return s.show(ctx)
	}
	return nil, &ArgumentError{Message: "action must be add, remove, title or link"}
}

func (s ownStickerSet) link() string {
	return "https://t.me/addstickers/" + s.name
}

// add makes a sticker and puts it in the set, within the owner's stickers
// for the day; one that is not added does not count.
func (s ownStickerSet) add(ctx context.Context, caller Caller, args stickerSetArgs) (any, error) {
	emoji, err := stickerEmoji(args.Emoji)
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(args.Prompt)
	switch {
	case utf8.RuneCountInString(prompt) > maxImagePrompt:
		return nil, &ArgumentError{Message: fmt.Sprintf("prompt must be at most %d characters", maxImagePrompt)}
	case prompt == "" && len(caller.Picture) == 0:
		return nil, &ArgumentError{Message: stickerNoPicture}
	case prompt != "" && s.Drawer == nil:
		return nil, &DeniedError{Message: stickerNoDrawing}
	}
	// Owners have no limit on pictures, but they do on stickers: the
	// limit is Telegram's pace, not a cost.
	limited := Caller{UserID: caller.UserID, ChatID: caller.ChatID}
	reservation, err := reserveToday(ctx, limited, daily{limit: stickersPerDay, location: s.Location, now: s.Now, what: "stickers"}, s.Store.ReserveStickerUpload)
	if err != nil {
		return nil, err
	}
	if err := s.put(ctx, caller, prompt, emoji); err != nil {
		_ = s.Store.ReleaseStickerUpload(context.WithoutCancel(ctx), reservation)
		return nil, err
	}
	return s.sendNewest(ctx, caller), nil
}

// put makes the sticker and adds it to the set, as the set's owner, or
// makes the set with it, as the asking owner's.
func (s ownStickerSet) put(ctx context.Context, caller Caller, prompt string, emoji []string) error {
	png, err := s.picture(ctx, caller.Picture, prompt)
	if err != nil {
		return err
	}
	owner, err := s.Store.OwnStickerSetOwner(ctx, s.name)
	made := err == nil
	switch {
	case errors.Is(err, storage.ErrNoStickerSet):
		owner = caller.UserID
	case err != nil:
		return internal(err)
	}
	fileID, err := s.Telegram.UploadStickerFile(ctx, owner, png)
	if err != nil {
		return refused(err)
	}
	sticker := telegram.InputSticker{FileID: fileID, Emoji: emoji}
	if made {
		return refused(s.Telegram.AddStickerToSet(ctx, owner, s.name, sticker))
	}
	return refused(s.create(ctx, owner, sticker))
}

// create makes the set with its first sticker and remembers whose it is. A
// set Telegram has already, which the database lost, is added to instead:
// it is most likely this owner's.
func (s ownStickerSet) create(ctx context.Context, owner int64, sticker telegram.InputSticker) error {
	err := s.Telegram.CreateNewStickerSet(ctx, owner, s.name, s.Title, sticker)
	if apiErr, ok := telegramRefusal(err); ok && strings.Contains(apiErr.Description, "occupied") {
		err = s.Telegram.AddStickerToSet(ctx, owner, s.name, sticker)
	}
	if err != nil {
		return err
	}
	// Were it not saved, the next sticker would find the set taken and be
	// added to it the same way.
	_ = s.Store.SaveOwnStickerSet(context.WithoutCancel(ctx), s.name, owner)
	return nil
}

// picture is the sticker's PNG: the caller's picture, or one drawn from
// prompt with its white background cut away.
func (s ownStickerSet) picture(ctx context.Context, picture []byte, prompt string) ([]byte, error) {
	if prompt == "" {
		return stickerPicture(stickers.Static(picture))
	}
	drawn, err := s.Drawer.Generate(ctx, prompt+stickerStyle)
	if err != nil {
		return nil, drawingError(err)
	}
	return stickerPicture(stickers.Drawn(drawn.Data))
}

func stickerPicture(png []byte, err error) ([]byte, error) {
	switch {
	case errors.Is(err, stickers.ErrNotAPicture):
		return nil, &ArgumentError{Message: stickerUnreadable}
	case errors.Is(err, stickers.ErrTooBig):
		return nil, &ArgumentError{Message: stickerTooDetailed}
	case err != nil:
		return nil, internal(err)
	}
	return png, nil
}

// sendNewest sends the set's last sticker, the one just added, in reply to
// the owner, and ends the turn with the set's link: the sticker is the
// answer. Then the model looks at it, so that the bot sends it too. A
// sticker that cannot be shown is added all the same.
func (s ownStickerSet) sendNewest(ctx context.Context, caller Caller) any {
	result := map[string]any{"done": "add", "link": s.link(), "note": stickerUnshownNote}
	set, err := s.Telegram.GetStickerSet(ctx, s.name)
	if err != nil || len(set.Stickers) == 0 {
		return result
	}
	result["stickers"] = len(set.Stickers)
	newest := set.Stickers[len(set.Stickers)-1]
	if _, err := s.Telegram.SendSticker(ctx, caller.ChatID, newest.FileID, caller.MessageID); err == nil {
		result["note"] = stickerAddedNote
		if sink, ok := SinkFrom(ctx); ok {
			sink.Finish(fmt.Sprintf("Stikerlarimga qo'shdim, endi %d ta: %s", len(set.Stickers), s.link()))
		}
	}
	s.learn(ctx)
	return result
}

// learn has the model look at the set's stickers not looked at yet, within
// what time the tool has left.
func (s ownStickerSet) learn(ctx context.Context) {
	if s.Learner == nil {
		return
	}
	budget := learnTimeout
	if deadline, ok := ctx.Deadline(); ok {
		budget = min(budget, time.Until(deadline)-learnMargin)
	}
	if budget <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	// The importer logs a sticker it could not look at; it is looked at
	// with the next one added.
	_, _ = s.Learner.Import(ctx, s.name)
}

// remove takes out of the set the sticker the owner replied to, which the
// bot then never sends again.
func (s ownStickerSet) remove(ctx context.Context, caller Caller) (any, error) {
	var sticker *telegram.Sticker
	if caller.Target != nil {
		sticker = caller.Target.Sticker
	}
	switch {
	case sticker == nil:
		return nil, &ArgumentError{Message: stickerNotReplied}
	case sticker.SetName != s.name:
		return nil, &DeniedError{Message: "that sticker is not from my set, " + s.link() + ", so it cannot be taken out"}
	}
	if err := s.Store.ForgetSticker(ctx, sticker.FileUniqueID); err != nil {
		return nil, internal(err)
	}
	if err := s.Telegram.DeleteStickerFromSet(ctx, sticker.FileID); err != nil {
		return nil, refused(err)
	}
	return map[string]any{"done": "remove", "note": stickerRemovedNote}, nil
}

// retitle renames the set, once it is made.
func (s ownStickerSet) retitle(ctx context.Context, title string) (any, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > telegram.MaxStickerSetTitle {
		return nil, &ArgumentError{Message: stickerTitleLength}
	}
	if _, err := s.Store.OwnStickerSetOwner(ctx, s.name); errors.Is(err, storage.ErrNoStickerSet) {
		return nil, &DeniedError{Message: stickerNoSet}
	} else if err != nil {
		return nil, internal(err)
	}
	if err := s.Telegram.SetStickerSetTitle(ctx, s.name, title); err != nil {
		return nil, refused(err)
	}
	return map[string]any{"done": "title", "title": title, "link": s.link()}, nil
}

// show is the set's link, title and size, as Telegram has them.
func (s ownStickerSet) show(ctx context.Context) (any, error) {
	set, err := s.Telegram.GetStickerSet(ctx, s.name)
	if _, ok := telegramRefusal(err); ok {
		return nil, &DeniedError{Message: stickerNoSet}
	}
	if err != nil {
		return nil, internal(err)
	}
	return map[string]any{"link": s.link(), "title": set.Title, "stickers": len(set.Stickers)}, nil
}

// stickerEmoji are the emoji the model gave, each checked to be one, once
// each: Telegram takes 1 to 20.
func stickerEmoji(given []string) ([]string, error) {
	var emoji []string
	for _, e := range given {
		e = strings.TrimSpace(e)
		if !isEmoji(e) {
			return nil, &ArgumentError{Message: fmt.Sprintf("%q is not one emoji; give each emoji on its own, such as [\"😂\", \"🔥\"]", e)}
		}
		if !slices.Contains(emoji, e) {
			emoji = append(emoji, e)
		}
	}
	if len(emoji) == 0 || len(emoji) > telegram.MaxStickerEmoji {
		return nil, &ArgumentError{Message: fmt.Sprintf("emoji: give 1 to %d, such as [\"😂\"]", telegram.MaxStickerEmoji)}
	}
	return emoji, nil
}

// maxEmojiRunes is room for the longest emoji, a family joined by
// zero-width joiners with skin tones.
const maxEmojiRunes = 16

// isEmoji reports something that can be an emoji: symbols, with the
// joiners, variation selectors and skin tones that build one, and no
// letter, digit or space.
func isEmoji(s string) bool {
	if s == "" || utf8.RuneCountInString(s) > maxEmojiRunes {
		return false
	}
	symbol := false
	for _, r := range s {
		switch {
		case unicode.Is(unicode.So, r):
			symbol = true
		case r == '‍', unicode.In(r, unicode.Sk, unicode.Mn, unicode.Me):
		default:
			return false
		}
	}
	return symbol
}

// cutTitle is the set's first title, within what Telegram takes.
func cutTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "Maria"
	}
	if runes := []rune(title); len(runes) > telegram.MaxStickerSetTitle {
		return string(runes[:telegram.MaxStickerSetTitle])
	}
	return title
}
