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

const (
	// mediaTimeout bounds downloading one file from Telegram.
	mediaTimeout = 15 * time.Second
	// transcribeTimeout bounds turning one voice message into text.
	transcribeTimeout = 30 * time.Second
	// maxTranscriptChars cuts very long transcripts.
	maxTranscriptChars = 2000
	// maxPollLabelChars bounds a poll's label: a long poll is named, not
	// copied out.
	maxPollLabelChars = 200
)

// Transcriber turns a voice message into text.
type Transcriber interface {
	Transcribe(ctx context.Context, audio []byte) (string, error)
}

// Media downloads files that were sent to the bot.
type Media interface {
	DownloadFile(ctx context.Context, fileID string, maxBytes int64) ([]byte, error)
}

// media is what the bot can make of a photo, sticker, GIF, voice message,
// video, file, song or poll.
type media struct {
	// label describes it in text, as the model and the stored history see it.
	label string
	// fileID is the file to show the model; empty when there is nothing the
	// model can take in (an animated sticker without a thumbnail, a voice
	// message the provider cannot hear, a file, a song, a poll).
	fileID string
	audio  bool
	// document is the kind of a file the model can read, as documentKind
	// names it; "" for anything else.
	document string
}

// mediaOf describes the media a message carries. Videos are shown by their
// thumbnail, like a GIF; a PDF, Word or text file is given to read; other
// files, songs and polls only by their label.
func (s *Service) mediaOf(msg *telegram.Message) (media, bool) {
	if msg == nil {
		return media{}, false
	}
	switch {
	case len(msg.Photo) > 0:
		return media{label: "<photo>", fileID: s.photoSize(msg.Photo)}, true
	case msg.Sticker != nil:
		return stickerMedia(msg.Sticker), true
	case msg.Animation != nil: // before Document, which Telegram also sets on a GIF
		return media{label: "<GIF>", fileID: thumbnailID(msg.Animation.Thumbnail)}, true
	case msg.Voice != nil:
		return s.voiceMedia(msg.Voice), true
	case msg.Video != nil:
		return media{label: "<video " + clock(msg.Video.Duration) + ">", fileID: thumbnailID(msg.Video.Thumbnail)}, true
	case msg.VideoNote != nil:
		return media{label: "<round video " + clock(msg.VideoNote.Duration) + ">", fileID: thumbnailID(msg.VideoNote.Thumbnail)}, true
	case msg.Document != nil:
		return s.documentMedia(msg.Document), true
	case msg.Audio != nil:
		return media{label: audioLabel(msg.Audio)}, true
	case msg.Poll != nil:
		return media{label: pollLabel(msg.Poll)}, true
	default:
		return media{}, false
	}
}

// clock writes a duration as a player shows it: "0:08", "1:05", "1:02:05".
func clock(seconds int) string {
	seconds = max(seconds, 0)
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

// audioLabel names a song by its performer and title, whichever Telegram
// has: "<audio Yulduz - Bahor>".
func audioLabel(audio *telegram.Audio) string {
	var tags []string
	for _, tag := range []string{audio.Performer, audio.Title} {
		if tag = oneLine(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return labelWith("<audio", strings.Join(tags, " - "))
}

// pollLabel gives a poll's question and options: "<poll: Lunch? / plov /
// manti>", cut to maxPollLabelChars.
func pollLabel(poll *telegram.Poll) string {
	const prefix, suffix = "<poll: ", ">"
	parts := []string{poll.Question}
	for _, option := range poll.Options {
		parts = append(parts, option.Text)
	}
	text := oneLine(strings.Join(parts, " / "))
	return prefix + truncate(text, maxPollLabelChars-len(prefix)-len(suffix)) + suffix
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
		return media{label: labelWith("<sticker", sticker.Emoji), fileID: sticker.FileID}
	}
	return media{label: labelWith("<animated sticker", sticker.Emoji), fileID: thumbnailID(sticker.Thumbnail)}
}

// labelWith closes a label, with a detail when there is one: "<sticker 😂>",
// "<file report.pdf>", "<file>".
func labelWith(kind, detail string) string {
	if detail == "" {
		return kind + ">"
	}
	return kind + " " + detail + ">"
}

func thumbnailID(thumbnail *telegram.PhotoSize) string {
	if thumbnail == nil {
		return ""
	}
	return thumbnail.FileID
}

// canListen reports whether voice messages can be turned into text.
func (s *Service) canListen() bool {
	return s.opts.AudioInput && s.opts.Transcriber != nil
}

// voiceMedia passes a voice message on only when the provider can hear it
// and it is not too long; otherwise the model is told why it was not.
func (s *Service) voiceMedia(voice *telegram.Voice) media {
	label := fmt.Sprintf("<voice message, %d s", voice.Duration)
	switch {
	case !s.canListen():
		return media{label: label + "; you cannot listen to voice messages>"}
	case voice.Duration > s.opts.MaxVoiceSeconds:
		return media{label: label + "; too long to listen to>"}
	default:
		return media{label: label + ">", fileID: voice.FileID, audio: true}
	}
}

// describe returns text with the message's media described in front of it,
// and where it was forwarded from in front of that. A voice message is
// downloaded and transcribed; the transcript is folded to one line without
// square brackets, like anything else a user produced. A message with
// nothing in it may be a member joining or leaving, which is described
// instead.
func (s *Service) describe(ctx context.Context, msg *telegram.Message, text string) string {
	m, ok := s.mediaOf(msg)
	switch {
	case ok:
		if m.audio {
			m.label = s.transcript(ctx, msg, m)
		}
		text = withMediaLabel(m, text)
	case text == "":
		text = memberNote(msg)
	}
	return withForward(msg, text)
}

// withForward puts where a forwarded message came from in front of its text,
// so that the model does not take a forwarded post for the sender's words.
func withForward(msg *telegram.Message, text string) string {
	if msg.ForwardOrigin == nil {
		return text
	}
	name := oneLine(msg.ForwardOrigin.Name())
	if name == "" {
		name = someone
	}
	return strings.TrimSpace("<forwarded from " + name + "> " + text)
}

// memberNote describes members joining or leaving, which Telegram sends as
// messages with nothing in them: "<Ali, Vali joined the group>", "<Ali left
// the group>". It is "" for any other message.
func memberNote(msg *telegram.Message) string {
	switch {
	case len(msg.NewChatMembers) > 0:
		names := make([]string, len(msg.NewChatMembers))
		for i := range msg.NewChatMembers {
			names[i] = plainLabel(&msg.NewChatMembers[i])
		}
		return "<" + strings.Join(names, ", ") + " joined the group>"
	case msg.LeftChatMember != nil:
		return "<" + plainLabel(msg.LeftChatMember) + " left the group>"
	default:
		return ""
	}
}

func (s *Service) transcript(ctx context.Context, msg *telegram.Message, m media) string {
	unheard := strings.TrimSuffix(m.label, ">") + "; it could not be transcribed>"
	downloadCtx, cancel := context.WithTimeout(ctx, mediaTimeout)
	audio, err := s.opts.Media.DownloadFile(downloadCtx, m.fileID, s.opts.MaxMediaBytes)
	cancel()
	if err != nil {
		s.opts.Logger.Warn("voice download failed", "chat_id", msg.Chat.ID, "error", err)
		return unheard
	}
	transcribeCtx, cancel := context.WithTimeout(ctx, transcribeTimeout)
	words, err := s.opts.Transcriber.Transcribe(transcribeCtx, audio)
	cancel()
	if err != nil {
		s.opts.Logger.Warn("transcription failed", "chat_id", msg.Chat.ID, "error", err)
		return unheard
	}
	words = truncate(oneLine(words), maxTranscriptChars)
	if words == "" {
		return strings.TrimSuffix(m.label, ">") + "; no speech>"
	}
	return m.label + ` "` + strings.ReplaceAll(words, `"`, "'") + `"`
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
		if !ok || m.fileID == "" || m.audio {
			continue // voice is given as its transcript, in the text
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
	if m.document != "" {
		return s.documentPart(in, m, data)
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
