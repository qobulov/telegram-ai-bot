package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxSpeechBytes bounds a spoken answer: a minute of Opus is under half a
// megabyte, and Telegram takes voice messages of up to 50 MB.
const maxSpeechBytes = 10 << 20

// ErrNotVoice means the provider answered with sound Telegram does not play
// as a voice message, such as WAV, or with no sound at all.
var ErrNotVoice = errors.New("ai: the speech is not OGG/Opus, MP3 or M4A")

// SpeechOptions configures a SpeechClient: an OpenAI-compatible API base
// URL, its key, a text-to-speech model, the voice and the format asked for,
// "opus" or "mp3".
type SpeechOptions struct {
	BaseURL    string
	APIKey     string
	Model      string
	Voice      string
	Format     string
	HTTPClient *http.Client
}

// SpeechClient reads text aloud through the OpenAI-compatible /audio/speech
// endpoint, which OpenAI and OpenRouter have. Gemini's OpenAI-compatible
// endpoint has none: its own speech models give WAV, which Telegram does
// not play as a voice message.
type SpeechClient struct {
	opts SpeechOptions
}

// Speech is text read aloud, in a format Telegram plays as a voice message:
// Format is "ogg", "mp3" or "m4a".
type Speech struct {
	Data   []byte
	Format string
}

// NewSpeechClient returns a SpeechClient.
func NewSpeechClient(opts SpeechOptions) *SpeechClient {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{}
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	return &SpeechClient{opts: opts}
}

type speechRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

// Speak reads text aloud. It is not retried: speech costs money, and the
// caller sends the text instead when it fails.
func (c *SpeechClient) Speak(ctx context.Context, text string) (Speech, error) {
	body, err := json.Marshal(speechRequest{Model: c.opts.Model, Input: text, Voice: c.opts.Voice, ResponseFormat: c.opts.Format})
	if err != nil {
		return Speech{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.opts.BaseURL+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return Speech{}, fmt.Errorf("build speech request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.opts.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		return Speech{}, fmt.Errorf("speech request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSpeechBytes+1))
	if err != nil {
		return Speech{}, fmt.Errorf("read speech response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Speech{}, statusError(resp, raw)
	}
	if len(raw) > maxSpeechBytes {
		return Speech{}, errors.New("ai: the speech is too long")
	}
	format := voiceFormat(raw)
	if format == "" {
		return Speech{}, ErrNotVoice
	}
	return Speech{Data: raw, Format: format}, nil
}

// voiceFormat names the format of audio from its first bytes, when it is
// one Telegram plays as a voice message, and is "" otherwise: a provider
// may ignore the format asked for, or answer an error with status 200.
func voiceFormat(audio []byte) string {
	switch {
	case bytes.HasPrefix(audio, []byte("OggS")):
		return "ogg"
	case bytes.HasPrefix(audio, []byte("ID3")), len(audio) > 1 && audio[0] == 0xFF && audio[1]&0xE0 == 0xE0:
		return "mp3"
	case len(audio) > 8 && string(audio[4:8]) == "ftyp":
		return "m4a"
	}
	return ""
}
