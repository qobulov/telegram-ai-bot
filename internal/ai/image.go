package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxImageResponseBytes bounds a generated image response.
const maxImageResponseBytes = 20 << 20

// ErrNoImage means the model answered without a picture, usually refusing.
var ErrNoImage = errors.New("ai: no image in the response")

// ImageOptions configures an ImageClient: Gemini's native API base URL
// (".../v1beta"), the key, and an image model.
type ImageOptions struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// ImageClient draws pictures with a Gemini image model.
type ImageClient struct {
	opts ImageOptions
}

// Image is a generated picture.
type Image struct {
	Data     []byte
	MimeType string
}

// NewImageClient returns an ImageClient.
func NewImageClient(opts ImageOptions) *ImageClient {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{}
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	return &ImageClient{opts: opts}
}

type imageRequest struct {
	Contents         []imageContent `json:"contents"`
	GenerationConfig struct {
		ResponseModalities []string `json:"responseModalities"`
	} `json:"generationConfig"`
}

type imageContent struct {
	Parts []imagePart `json:"parts"`
}

type imagePart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inlineData,omitempty"`
}

type inlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type imageResponse struct {
	Candidates []struct {
		Content imageContent `json:"content"`
	} `json:"candidates"`
}

// Generate draws a picture from the prompt. It is not retried: a picture
// costs money, and the caller decides whether to try again.
func (c *ImageClient) Generate(ctx context.Context, prompt string) (Image, error) {
	var req imageRequest
	req.Contents = []imageContent{{Parts: []imagePart{{Text: prompt}}}}
	req.GenerationConfig.ResponseModalities = []string{"IMAGE", "TEXT"}
	body, err := json.Marshal(req)
	if err != nil {
		return Image{}, err
	}
	endpoint := c.opts.BaseURL + "/models/" + c.opts.Model + ":generateContent"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Image{}, fmt.Errorf("build image request: %w", err)
	}
	httpReq.Header.Set("x-goog-api-key", c.opts.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.opts.HTTPClient.Do(httpReq)
	if err != nil {
		return Image{}, fmt.Errorf("image request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageResponseBytes))
	if err != nil {
		return Image{}, fmt.Errorf("read image response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Image{}, statusError(resp, raw)
	}
	return decodeImage(raw)
}

func decodeImage(raw []byte) (Image, error) {
	var parsed imageResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Image{}, fmt.Errorf("decode image response: %w", err)
	}
	var said []string
	for _, candidate := range parsed.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil {
				data, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
				if err != nil {
					return Image{}, fmt.Errorf("decode image data: %w", err)
				}
				return Image{Data: data, MimeType: part.InlineData.MimeType}, nil
			}
			said = append(said, part.Text)
		}
	}
	return Image{}, fmt.Errorf("%w: %s", ErrNoImage, truncate(strings.TrimSpace(strings.Join(said, " "))))
}
