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
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/telegram"
)

const botProfileTimeout = 30 * time.Second

// BotAccount is the bot's own Telegram profile.
type BotAccount interface {
	GetBotProfile(ctx context.Context, language string) (telegram.BotProfile, error)
	SetBotName(ctx context.Context, name, language string) error
	SetBotBio(ctx context.Context, bio, language string) error
	SetBotDescription(ctx context.Context, description, language string) error
	SetBotPhoto(ctx context.Context, jpeg []byte) error
	RemoveBotPhoto(ctx context.Context) error
}

// botProfileLanguages are the languages a profile text may be set for: ""
// for everyone without their own, then the bot's three.
var botProfileLanguages = []string{"", "uz", "ru", "en"}

type botProfileArgs struct {
	Action   string `json:"action"`
	Text     string `json:"text"`
	Language string `json:"language"`
}

// BotProfileTool lets an owner change the bot's own Telegram profile: its
// name, bio, description and photo, for everyone or for one language.
func BotProfileTool(account BotAccount) Tool {
	return Tool{
		Name: "bot_profile",
		Description: "Owners only. Your own Telegram profile. show: name, bio and description in each language. " +
			"name, bio (profile page, 120 chars), description (empty chat, 512): set text, for language or for all. " +
			"photo: make the picture in the message your photo. remove_photo. Only when an owner asks.",
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
		OwnerOnly: true,
		Timeout:   botProfileTimeout,
		Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
			return runBotProfile(ctx, account, raw)
		},
	}
}

func runBotProfile(ctx context.Context, account BotAccount, raw json.RawMessage) (any, error) {
	var args botProfileArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	caller, ok := CallerFrom(ctx)
	if !ok || !caller.Owner {
		return nil, errors.New("only the bot's owners may change its profile")
	}
	text := strings.TrimSpace(args.Text)
	var err error
	switch args.Action {
	case "show":
		return showBotProfile(ctx, account)
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

func showBotProfile(ctx context.Context, account BotAccount) (any, error) {
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
	return profiles, nil
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
