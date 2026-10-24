package telegram

import (
	"bytes"
	"context"
	"mime/multipart"
)

// Limits Telegram sets on the bot's own profile, in characters.
const (
	MaxBotName        = 64
	MaxBotBio         = 120
	MaxBotDescription = 512
)

// BotProfile is the bot's own profile in one language: its name, its bio
// (the short description on its profile page and with shared links) and
// its description (shown in an empty chat with it).
type BotProfile struct {
	Name, Bio, Description string
}

type profileTextParams struct {
	Name             string `json:"name,omitempty"`
	ShortDescription string `json:"short_description,omitempty"`
	Description      string `json:"description,omitempty"`
	LanguageCode     string `json:"language_code,omitempty"`
}

// GetBotProfile returns the bot's profile for language, such as "uz", or
// the one for every language without its own when language is "".
func (c *Client) GetBotProfile(ctx context.Context, language string) (BotProfile, error) {
	params := profileTextParams{LanguageCode: language}
	var name struct {
		Name string `json:"name"`
	}
	var bio struct {
		ShortDescription string `json:"short_description"`
	}
	var description struct {
		Description string `json:"description"`
	}
	if err := c.call(ctx, "getMyName", params, &name); err != nil {
		return BotProfile{}, err
	}
	if err := c.call(ctx, "getMyShortDescription", params, &bio); err != nil {
		return BotProfile{}, err
	}
	if err := c.call(ctx, "getMyDescription", params, &description); err != nil {
		return BotProfile{}, err
	}
	return BotProfile{Name: name.Name, Bio: bio.ShortDescription, Description: description.Description}, nil
}

// SetBotName sets the bot's name for language, or for every language
// without its own when language is "". An empty name removes the one for
// that language.
func (c *Client) SetBotName(ctx context.Context, name, language string) error {
	var ok bool
	return c.call(ctx, "setMyName", profileTextParams{Name: name, LanguageCode: language}, &ok)
}

// SetBotBio sets the short description on the bot's profile page.
func (c *Client) SetBotBio(ctx context.Context, bio, language string) error {
	var ok bool
	return c.call(ctx, "setMyShortDescription", profileTextParams{ShortDescription: bio, LanguageCode: language}, &ok)
}

// SetBotDescription sets the text shown in an empty chat with the bot.
func (c *Client) SetBotDescription(ctx context.Context, description, language string) error {
	var ok bool
	return c.call(ctx, "setMyDescription", profileTextParams{Description: description, LanguageCode: language}, &ok)
}

// SetBotPhoto uploads a JPEG as the bot's profile photo.
func (c *Client) SetBotPhoto(ctx context.Context, jpeg []byte) error {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("photo", `{"type":"static","photo":"attach://avatar"}`); err != nil {
		return err
	}
	file, err := form.CreateFormFile("avatar", "avatar.jpg")
	if err != nil {
		return err
	}
	if _, err := file.Write(jpeg); err != nil {
		return err
	}
	if err := form.Close(); err != nil {
		return err
	}
	var ok bool
	return c.postForm(ctx, "setMyProfilePhoto", &body, form.FormDataContentType(), &ok)
}

// RemoveBotPhoto removes the bot's profile photo.
func (c *Client) RemoveBotPhoto(ctx context.Context) error {
	var ok bool
	return c.call(ctx, "removeMyProfilePhoto", struct{}{}, &ok)
}

// profilePhotoSide is the size a profile photo is downloaded in, near
// enough to see it and small enough to be quick.
const profilePhotoSide = 640

// ProfilePhoto downloads a user's current profile photo, the bot's own
// included, in the size nearest profilePhotoSide, with its unique ID. ok is
// false when they have none.
func (c *Client) ProfilePhoto(ctx context.Context, userID, maxBytes int64) (data []byte, id string, ok bool, err error) {
	var photos struct {
		Photos [][]PhotoSize `json:"photos"`
	}
	params := struct {
		UserID int64 `json:"user_id"`
		Limit  int   `json:"limit"`
	}{userID, 1}
	if err := c.call(ctx, "getUserProfilePhotos", params, &photos); err != nil {
		return nil, "", false, err
	}
	if len(photos.Photos) == 0 || len(photos.Photos[0]) == 0 {
		return nil, "", false, nil
	}
	best := photos.Photos[0][0]
	for _, size := range photos.Photos[0] {
		if distance(size.Width, profilePhotoSide) < distance(best.Width, profilePhotoSide) {
			best = size
		}
	}
	data, err = c.DownloadFile(ctx, best.FileID, maxBytes)
	if err != nil {
		return nil, "", false, err
	}
	return data, best.FileUniqueID, true, nil
}

func distance(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
