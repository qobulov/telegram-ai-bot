package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // decodes a GIF sent to be the bot's photo
	"image/jpeg"
	_ "image/png" // decodes a PNG sent to be the bot's photo
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/telegram"
)

const (
	botProfileTimeout = 30 * time.Second
	// avatarPrompt asks the model what the bot's own photo shows.
	avatarPrompt = "This is your own profile picture on Telegram. In one or two short sentences, in English, say plainly what it shows: who or what, the colours and the style."
)

// BotAccount is the bot's own Telegram profile.
type BotAccount interface {
	GetBotProfile(ctx context.Context, language string) (telegram.BotProfile, error)
	SetBotName(ctx context.Context, name, language string) error
	SetBotBio(ctx context.Context, bio, language string) error
	SetBotDescription(ctx context.Context, description, language string) error
	SetBotPhoto(ctx context.Context, jpeg []byte) error
	RemoveBotPhoto(ctx context.Context) error
	// Photo is the bot's current profile photo and its unique ID; ok is
	// false when it has none.
	Photo(ctx context.Context) (data []byte, id string, ok bool, err error)
}

// Eyes is a model that can look at a picture.
type Eyes interface {
	Complete(ctx context.Context, req ai.Request) (ai.Message, error)
}

// BotProfileOptions configures the bot_profile tool.
type BotProfileOptions struct {
	Account BotAccount
	// Eyes describe the profile photo; nil leaves it undescribed.
	Eyes Eyes
}

// botProfileLanguages are the languages a profile text may be set for: ""
// for everyone without their own, then the bot's three.
var botProfileLanguages = []string{"", "uz", "ru", "en"}

type botProfileArgs struct {
	Action   string `json:"action"`
	Text     string `json:"text"`
	Language string `json:"language"`
}

// BotProfileTool shows anyone the bot's own Telegram profile, what its
// photo shows included, and lets an owner change it: its name, bio,
// description and photo, for everyone or for one language.
func BotProfileTool(opts BotProfileOptions) Tool {
	avatars := &avatarDescriptions{eyes: opts.Eyes}
	return Tool{
		Name: "bot_profile",
		Description: "Your own Telegram profile. show: your name, bio, description and what your profile photo shows; use it when asked about them. " +
			"Owners only, when an owner asks: name, bio (120 chars), description (512) set text, for a language or all; photo makes the picture in the message yours; remove_photo.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["show", "name", "bio", "description", "photo", "remove_photo"]},
				"text": {"type": "string", "description": "The new text, exactly as the owner gave it; empty removes that language's own."},
				"language": {"type": "string", "enum": ["", "uz", "ru", "en"], "description": "Only for people whose Telegram is in this language; empty for everyone."}
			},
			"required": ["action"],
			"additionalProperties": false
		}`),
		Timeout: botProfileTimeout,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return runBotProfile(ctx, opts.Account, avatars, raw)
		},
	}
}

func runBotProfile(ctx context.Context, account BotAccount, avatars *avatarDescriptions, raw json.RawMessage) (any, error) {
	var args botProfileArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	if args.Action == "show" {
		return showBotProfile(ctx, account, avatars)
	}
	caller, ok := CallerFrom(ctx)
	if !ok || !caller.Owner {
		return nil, &ArgumentError{Message: "only your owners may change your profile; say so"}
	}
	text := strings.TrimSpace(args.Text)
	var err error
	switch args.Action {
	case "name":
		err = setProfileText(ctx, account.SetBotName, text, args.Language, telegram.MaxBotName)
	case "bio":
		err = setProfileText(ctx, account.SetBotBio, text, args.Language, telegram.MaxBotBio)
	case "description":
		err = setProfileText(ctx, account.SetBotDescription, text, args.Language, telegram.MaxBotDescription)
	case "photo":
		err = setBotPhoto(ctx, account, caller.Picture)
	case "remove_photo":
		err = account.RemoveBotPhoto(ctx)
	default:
		return nil, &ArgumentError{Message: "action must be show, name, bio, description, photo or remove_photo"}
	}
	if err != nil {
		return nil, refused(err)
	}
	result := map[string]any{"done": args.Action}
	if args.Action == "name" {
		result["note"] = "Telegram shows the new name; you still answer to the names you were called by before, which the owners change in the settings."
	}
	return result, nil
}

func showBotProfile(ctx context.Context, account BotAccount, avatars *avatarDescriptions) (any, error) {
	profiles := map[string]telegram.BotProfile{}
	for _, language := range botProfileLanguages {
		profile, err := account.GetBotProfile(ctx, language)
		if err != nil {
			return nil, refused(err)
		}
		key := language
		if key == "" {
			key = "default"
		}
		profiles[key] = profile
	}
	return map[string]any{"profile": profiles, "photo": avatars.of(ctx, account)}, nil
}

// avatarDescriptions are what the bot's profile photos show, by photo, so
// that each is looked at once.
type avatarDescriptions struct {
	eyes Eyes
	mu   sync.Mutex
	seen map[string]string
}

// of is what the bot's current photo shows: "none" without one, and
// without a description when it cannot be looked at.
func (a *avatarDescriptions) of(ctx context.Context, account BotAccount) map[string]any {
	data, id, ok, err := account.Photo(ctx)
	switch {
	case err != nil:
		return map[string]any{"error": "the photo could not be fetched"}
	case !ok:
		return map[string]any{"has": false}
	}
	photo := map[string]any{"has": true}
	if shows := a.describe(ctx, id, data); shows != "" {
		photo["shows"] = shows
	}
	return photo
}

func (a *avatarDescriptions) describe(ctx context.Context, id string, data []byte) string {
	a.mu.Lock()
	shows, known := a.seen[id]
	a.mu.Unlock()
	if known || a.eyes == nil {
		return shows
	}
	reply, err := a.eyes.Complete(ctx, ai.Request{Messages: []ai.Message{{
		Role: ai.RoleUser, Content: avatarPrompt, Parts: []ai.ContentPart{ai.ImagePart("image/jpeg", data)},
	}}})
	if err != nil {
		return ""
	}
	shows = strings.TrimSpace(reply.Content)
	a.mu.Lock()
	if a.seen == nil {
		a.seen = map[string]string{}
	}
	a.seen[id] = shows
	a.mu.Unlock()
	return shows
}

func setProfileText(ctx context.Context, set func(context.Context, string, string) error, text, language string, limit int) error {
	if utf8.RuneCountInString(text) > limit {
		return &ArgumentError{Message: fmt.Sprintf("text must be at most %d characters", limit)}
	}
	if text == "" && language == "" {
		return &ArgumentError{Message: "give the text; only one language's own text may be removed"}
	}
	return set(ctx, text, language)
}

func setBotPhoto(ctx context.Context, account BotAccount, picture []byte) error {
	if len(picture) == 0 {
		return &ArgumentError{Message: "there is no picture in the message: ask the owner to send the photo with the request, or reply to one"}
	}
	photo, err := asJPEG(picture)
	if err != nil {
		return err
	}
	return account.SetBotPhoto(ctx, photo)
}

// asJPEG is a picture as a JPEG, which Telegram takes for a profile photo.
func asJPEG(picture []byte) ([]byte, error) {
	if bytes.HasPrefix(picture, []byte{0xFF, 0xD8, 0xFF}) {
		return picture, nil
	}
	img, _, err := image.Decode(bytes.NewReader(picture))
	if err != nil {
		return nil, &ArgumentError{Message: "this picture cannot be used: ask for it as a photo, not a sticker or a file"}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 92}); err != nil {
		return nil, internal(err)
	}
	return out.Bytes(), nil
}

// refused gives Telegram's reason to the owner who asked, such as how long
// until the name may change again; anything else stays in the logs.
func refused(err error) error {
	var apiErr *telegram.APIError
	if errors.As(err, &apiErr) {
		message := "Telegram refused: " + apiErr.Description
		if apiErr.RetryAfter > 0 {
			message += fmt.Sprintf(" (try again in %s)", apiErr.RetryAfter.Round(time.Minute))
		}
		return &ArgumentError{Message: message}
	}
	var argErr *ArgumentError
	if errors.As(err, &argErr) {
		return err
	}
	return internal(err)
}
