package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/telegram"
)

// mediaTimeout bounds downloading one file from Telegram.
const mediaTimeout = 15 * time.Second

// Media downloads files that were sent to the bot.
type Media interface {
	DownloadFile(ctx context.Context, fileID string, maxBytes int64) ([]byte, error)
}

// media is what the bot can make of a photo, sticker, GIF or voice message.
type media struct {
	// label describes it in text, as the model and the stored history see it.
	label string
	// fileID is the file to show the model; empty when there is nothing the
	// model can take in (an animated sticker without a thumbnail, a voice
	// message the provider cannot hear).
	fileID string
	audio  bool
}

// mediaOf describes the media a message carries.
func (s *Service) mediaOf(msg *telegram.Message) (media, bool) {
	if msg == nil {
		return media{}, false
	}
	switch {
	case len(msg.Photo) > 0:
		return media{label: "<photo>", fileID: s.photoSize(msg.Photo)}, true
	case msg.Sticker != nil:
		return stickerMedia(msg.Sticker), true
	case msg.Animation != nil:
		return media{label: "<GIF>", fileID: thumbnailID(msg.Animation.Thumbnail)}, true
	case msg.Voice != nil:
		return s.voiceMedia(msg.Voice), true
	default:
		return media{}, false
	}
}

// photoSize picks the largest size that fits the download limit. Telegram
// lists sizes smallest first; a size with unknown file size is assumed to fit.
func (s *Service) photoSize(sizes []telegram.PhotoSize) string {
	chosen := sizes[0].FileID
	for _, size := range sizes {
		if size.FileSize <= s.opts.MaxMediaBytes {
			chosen = size.FileID
		}
	}
	return chosen
}

// stickerMedia shows a static sticker itself. Animated and video stickers
// are not images, so their first-frame thumbnail is shown instead; the emoji
// says what the sticker means either way.
func stickerMedia(sticker *telegram.Sticker) media {
	if !sticker.IsAnimated && !sticker.IsVideo {
		return media{label: labelWithEmoji("<sticker", sticker.Emoji), fileID: sticker.FileID}
	}
	return media{label: labelWithEmoji("<animated sticker", sticker.Emoji), fileID: thumbnailID(sticker.Thumbnail)}
}

func labelWithEmoji(kind, emoji string) string {
	if emoji == "" {
		return kind + ">"
	}
	return kind + " " + emoji + ">"
}

func thumbnailID(thumbnail *telegram.PhotoSize) string {
	if thumbnail == nil {
		return ""
	}
	return thumbnail.FileID
}

// voiceMedia passes a voice message on only when the provider can hear it
// and it is not too long; otherwise the model is told why it was not.
func (s *Service) voiceMedia(voice *telegram.Voice) media {
	label := fmt.Sprintf("<voice message, %d s", voice.Duration)
	switch {
	case !s.opts.AudioInput:
		return media{label: label + "; you cannot listen to voice messages>"}
	case voice.Duration > s.opts.MaxVoiceSeconds:
		return media{label: label + "; too long to listen to>"}
	default:
		return media{label: label + ">", fileID: voice.FileID, audio: true}
	}
}

// withMediaLabel puts the description of a message's media in front of its
// text: "<photo> look at this". Labels use angle brackets because square
// brackets are reserved for the bot's own markers and are replaced in
// anything a user wrote. A user can type a label, but that attaches nothing.
func withMediaLabel(m media, text string) string {
	return strings.TrimSpace(m.label + " " + text)
}

// attachments downloads what the model should see: the media of the message
// itself and of the message it replies to. A file that cannot be downloaded
// is left out; its label is still in the text, so the model knows something
// was there.
func (s *Service) attachments(ctx context.Context, in incoming) []ai.ContentPart {
	var parts []ai.ContentPart
	for _, msg := range []*telegram.Message{in.msg, in.msg.ReplyToMessage} {
		m, ok := s.mediaOf(msg)
		if !ok || m.fileID == "" {
			continue
		}
		if part, ok := s.download(ctx, in, m); ok {
			parts = append(parts, part)
		}
	}
	return parts
}

func (s *Service) download(ctx context.Context, in incoming, m media) (ai.ContentPart, bool) {
	ctx, cancel := context.WithTimeout(ctx, mediaTimeout)
	defer cancel()
	data, err := s.opts.Media.DownloadFile(ctx, m.fileID, s.opts.MaxMediaBytes)
	if err != nil {
		s.opts.Logger.Warn("media download failed", "chat_id", in.scope.ChatID, "update_id", in.updateID, "error", err)
		return ai.ContentPart{}, false
	}
	if m.audio {
		return ai.AudioPart("ogg", data), true
	}
	// Telegram serves photos as JPEG and stickers as WebP; the bytes say
	// which, so the declared type does not have to be trusted.
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		s.opts.Logger.Warn("downloaded media is not an image", "chat_id", in.scope.ChatID, "type", mimeType)
		return ai.ContentPart{}, false
	}
	return ai.ImagePart(mimeType, data), true
}
