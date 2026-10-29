package service

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/telegram"
)

// Speaker reads an answer aloud.
type Speaker interface {
	Speak(ctx context.Context, text string) (ai.Speech, error)
}

// VoiceSender sends a voice message.
type VoiceSender interface {
	SendVoice(ctx context.Context, voice telegram.VoiceNote) (int64, error)
}

const (
	// maxSpokenRunes is the longest answer read aloud, about a minute of
	// speech: speech is paid by the character, and a longer answer is
	// easier read than heard. A longer one goes as text.
	maxSpokenRunes = 800
	// speechTimeout bounds reading an answer aloud. It comes after the
	// turn, so it is kept short: on serverless hosting the whole update has
	// a minute.
	speechTimeout = 15 * time.Second
)

// voiceNote tells the model, right before the message, that its answer will
// be heard, not read.
const voiceNote = "[They asked you to answer by voice: your answer is read aloud and sent as a voice message. Write only the words to say, as you would say them: no Markdown, lists, links, emoji or markers, and under 600 characters. Don't mention that it is a voice message.]"

var (
	// voiceWords name a voice answer, in Uzbek, Russian and English, and
	// answerWords asking for one: "ovozli javob ber", "ovoz bilan ayt",
	// "голосом ответь", "send a voice message". Either may come first, a
	// word or two apart. "ovozli xabarni eshit", about a voice message
	// someone sent, asks for none, nor does "голосование", a vote.
	voiceWords  = `(?:ovozli|ovoz bilan|ovozda|ovozing bilan|ovozingiz bilan|ovozingda|голосом|голосов(?:ое|ым|ой|ую|уху|ухой)|voice|by voice)`
	answerWords = `(?:javob|ayt|gapir|jo'nat|yubor|tashla|o'qi|ответ|скаж|отправ|пришл|запиш|reply|answer|say|send|tell|read)`
	between     = `\P{L}+(?:\p{L}+\P{L}+){0,2}?`
	voiceAsk    = regexp.MustCompile(`(?i)(?:^|\P{L})(?:` + voiceWords + between + answerWords + `|` + answerWords + `\p{L}*` + between + voiceWords + `)|озвуч`)
)

// wantsVoice reports words that ask to be answered by voice.
func wantsVoice(words string) bool {
	return voiceAsk.MatchString(strings.Map(oneApostrophe, strings.ToLower(words)))
}

// canSpeak reports whether answers can be sent as voice messages.
func (s *Service) canSpeak() bool {
	return s.opts.Speaker != nil && s.opts.VoiceSender != nil
}

// sendSpoken reads text aloud and sends it as a voice message, and returns
// the sent message's ID, or 0 when it could not be: the text is too long,
// speech failed or Telegram refused it, and the caller then sends the text
// itself.
func (s *Service) sendSpoken(in incoming, text string) int64 {
	if utf8.RuneCountInString(text) > maxSpokenRunes {
		s.opts.Logger.Info("answer too long to read aloud; sending text", "chat_id", in.msg.Chat.ID)
		return 0
	}
	s.showRecording(in.msg)
	ctx, cancel := context.WithTimeout(context.Background(), speechTimeout)
	speech, err := s.opts.Speaker.Speak(ctx, text)
	cancel()
	if err != nil {
		s.opts.Logger.Warn("could not read the answer aloud; sending text", "chat_id", in.msg.Chat.ID, "error", err)
		return 0
	}
	ctx, cancel = context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	id, err := s.opts.VoiceSender.SendVoice(ctx, telegram.VoiceNote{
		ChatID: in.msg.Chat.ID, ReplyToMessageID: in.replyTarget(), Data: speech.Data, Format: speech.Format,
	})
	if err != nil {
		s.opts.Logger.Warn("could not send a voice answer; sending text", "chat_id", in.msg.Chat.ID, "error", err)
		return 0
	}
	return id
}

// showRecording shows "recording voice" while the answer is read aloud. It
// is a gesture, like typing.
func (s *Service) showRecording(msg *telegram.Message) {
	if s.opts.ChatActor == nil {
		return
	}
	threadID := int64(0)
	if msg.IsTopicMessage {
		threadID = msg.MessageThreadID
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if err := s.opts.ChatActor.SendChatAction(ctx, msg.Chat.ID, threadID, "record_voice"); err != nil {
		s.opts.Logger.Debug("recording indicator failed", "chat_id", msg.Chat.ID, "error", err)
	}
}
