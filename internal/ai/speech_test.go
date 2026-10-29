package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSpeakAsksForTheVoiceAndFormat(t *testing.T) {
	var path, auth string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = io.WriteString(w, "OggS\x00opus-bytes")
	}))
	defer server.Close()
	speech := NewSpeechClient(SpeechOptions{BaseURL: server.URL + "/v1/", APIKey: "k", Model: "tts-model", Voice: "nova", Format: "opus"})

	got, err := speech.Speak(context.Background(), "Salom!")
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if got.Format != "ogg" || string(got.Data) != "OggS\x00opus-bytes" {
		t.Errorf("speech = %s %q", got.Format, got.Data)
	}
	if path != "/v1/audio/speech" || auth != "Bearer k" {
		t.Errorf("path = %q, auth = %q", path, auth)
	}
	want := map[string]any{"model": "tts-model", "input": "Salom!", "voice": "nova", "response_format": "opus"}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("%s = %v, want %v", k, body[k], v)
		}
	}
}

// Sound Telegram would not play as a voice message is refused, whatever
// was asked for, so that the text goes instead.
func TestSpeakRefusesWhatIsNoVoice(t *testing.T) {
	for name, reply := range map[string]string{
		"wav":  "RIFF\x24\x00\x00\x00WAVEfmt ",
		"json": `{"error":"no such model"}`,
		"none": "",
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, reply)
			}))
			defer server.Close()
			speech := NewSpeechClient(SpeechOptions{BaseURL: server.URL, APIKey: "k", Model: "m"})

			if _, err := speech.Speak(context.Background(), "x"); !errors.Is(err, ErrNotVoice) {
				t.Errorf("err = %v, want ErrNotVoice", err)
			}
		})
	}
}

func TestSpeakReportsProviderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":404,"message":"Not found"}}`)
	}))
	defer server.Close()
	speech := NewSpeechClient(SpeechOptions{BaseURL: server.URL, APIKey: "k", Model: "m"})

	_, err := speech.Speak(context.Background(), "x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestVoiceFormat(t *testing.T) {
	cases := map[string]string{
		"OggS\x00\x02":                 "ogg",
		"ID3\x04\x00":                  "mp3",
		"\xFF\xFB\x90\x00":             "mp3",
		"\x00\x00\x00\x20ftypM4A \x00": "m4a",
		"RIFF\x24\x00\x00\x00WAVEfmt ": "",
		"<html>":                       "",
		"\xFF":                         "",
	}
	for audio, want := range cases {
		if got := voiceFormat([]byte(audio)); got != want {
			t.Errorf("voiceFormat(%q) = %q, want %q", audio, got, want)
		}
	}
}
