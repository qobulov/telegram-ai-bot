package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateImageReturnsThePicture(t *testing.T) {
	var path, key string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, key = r.URL.Path, r.Header.Get("x-goog-api-key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"a cat"},{"inlineData":{"mimeType":"image/png","data":"UE5HREFUQQ=="}}]}}]}`)
	}))
	defer server.Close()
	images := NewImageClient(ImageOptions{BaseURL: server.URL, APIKey: "k", Model: "img-model"})

	image, err := images.Generate(context.Background(), "a cat in sunglasses")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if string(image.Data) != "PNGDATA" || image.MimeType != "image/png" {
		t.Errorf("image = %q %q", image.MimeType, image.Data)
	}
	if path != "/models/img-model:generateContent" || key != "k" {
		t.Errorf("path = %q, key = %q", path, key)
	}
	modalities := body["generationConfig"].(map[string]any)["responseModalities"].([]any)
	if len(modalities) == 0 || modalities[0] != "IMAGE" {
		t.Errorf("responseModalities = %v", modalities)
	}
	text := body["contents"].([]any)[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"]
	if !strings.Contains(text.(string), "a cat in sunglasses") {
		t.Errorf("prompt = %v", text)
	}
}

func TestGenerateImageReportsQuotaErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"error":{"code":429,"message":"You exceeded your current quota"}}`)
	}))
	defer server.Close()
	images := NewImageClient(ImageOptions{BaseURL: server.URL, APIKey: "k", Model: "m"})

	_, err := images.Generate(context.Background(), "x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 429 {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerateImageWithoutAPictureIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"I cannot draw that."}]}}]}`)
	}))
	defer server.Close()
	images := NewImageClient(ImageOptions{BaseURL: server.URL, APIKey: "k", Model: "m"})

	_, err := images.Generate(context.Background(), "x")
	if !errors.Is(err, ErrNoImage) || !strings.Contains(err.Error(), "I cannot draw that.") {
		t.Fatalf("err = %v, want ErrNoImage with the model's explanation", err)
	}
}
