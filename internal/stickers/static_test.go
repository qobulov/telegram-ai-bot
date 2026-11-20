package stickers

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"testing"
)

func encoded(t *testing.T, img image.Image) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func pngOf(t *testing.T, sticker []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(sticker))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	return img
}

func filled(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return img
}

// A picture of any size becomes a PNG whose longer side is 512 and whose
// shape is kept.
func TestStaticFitsTheLongerSideTo512(t *testing.T) {
	for _, tc := range []struct{ w, h, wantW, wantH int }{
		{1280, 720, 512, 288},
		{300, 600, 256, 512},
		{128, 128, 512, 512},
	} {
		sticker, err := Static(encoded(t, filled(tc.w, tc.h, color.RGBA{200, 30, 30, 255})))
		if err != nil {
			t.Fatalf("%dx%d: %v", tc.w, tc.h, err)
		}
		if got := pngOf(t, sticker).Bounds().Size(); got != image.Pt(tc.wantW, tc.wantH) {
			t.Errorf("%dx%d became %v, want %dx%d", tc.w, tc.h, got, tc.wantW, tc.wantH)
		}
	}
}

// Noise, which no PNG compresses, still fits Telegram's 512 KB, with fewer
// colours.
func TestStaticKeepsANoisyPhotoWithin512KB(t *testing.T) {
	noise := image.NewRGBA(image.Rect(0, 0, 900, 900))
	random := rand.New(rand.NewPCG(1, 2))
	for i := range noise.Pix {
		noise.Pix[i] = byte(random.UintN(256))
		if i%4 == 3 {
			noise.Pix[i] = 0xFF
		}
	}
	var picture bytes.Buffer
	if err := png.Encode(&picture, noise); err != nil {
		t.Fatal(err)
	}
	sticker, err := Static(picture.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(sticker) > maxStaticBytes {
		t.Errorf("sticker is %d bytes, above %d", len(sticker), maxStaticBytes)
	}
	if got := pngOf(t, sticker).Bounds().Size(); got != image.Pt(512, 512) {
		t.Errorf("size = %v", got)
	}
}

func TestStaticRefusesWhatIsNoPicture(t *testing.T) {
	if _, err := Static([]byte("not a picture")); !errors.Is(err, ErrNotAPicture) {
		t.Errorf("err = %v, want ErrNotAPicture", err)
	}
}
