package stickers

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"  // decodes a GIF sent to be a sticker
	_ "image/jpeg" // decodes a photo
	"image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // decodes a sticker sent to be one of the bot's own
)

const (
	// StaticSide is what a static sticker's longer side must measure; the
	// other may be less.
	StaticSide = 512
	// maxStaticBytes is the largest static sticker Telegram takes.
	maxStaticBytes = 512 << 10
	// maxPixels bounds a picture decoded to be a sticker: a small file can
	// claim to be a huge picture.
	maxPixels = 40_000_000
)

// ErrNotAPicture means the picture cannot be read as one, or is too large
// to be.
var ErrNotAPicture = errors.New("stickers: not a picture that can be read")

// ErrTooBig means the sticker stays above Telegram's 512 KB even with fewer
// colours.
var ErrTooBig = errors.New("stickers: the sticker cannot be made small enough")

// posterize are the low bits of each colour dropped, one more each time
// the PNG is still too big: a photo's noise is what fills the file.
var posterize = []uint{0, 2, 3, 4, 5}

// Static makes a picture, a JPEG, PNG, GIF or WebP, a static sticker as
// Telegram takes it: a PNG whose longer side is 512 pixels, of at most
// 512 KB.
func Static(picture []byte) ([]byte, error) {
	img, err := decode(picture)
	if err != nil {
		return nil, err
	}
	return encode(scaled(img))
}

func decode(picture []byte) (image.Image, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(picture))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width*config.Height > maxPixels {
		return nil, ErrNotAPicture
	}
	img, _, err := image.Decode(bytes.NewReader(picture))
	if err != nil {
		return nil, ErrNotAPicture
	}
	return img, nil
}

// scaled is img resized, larger or smaller, to StaticSide on its longer
// side.
func scaled(img image.Image) *image.NRGBA {
	from := img.Bounds()
	w, h := from.Dx(), from.Dy()
	if w >= h {
		w, h = StaticSide, max(1, (h*StaticSide+w/2)/w)
	} else {
		w, h = max(1, (w*StaticSide+h/2)/h), StaticSide
	}
	sticker := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(sticker, sticker.Bounds(), img, from, draw.Src, nil)
	return sticker
}

// encode is the sticker as a PNG within maxStaticBytes, with fewer colours
// as long as it is not.
func encode(sticker *image.NRGBA) ([]byte, error) {
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	for _, bits := range posterize {
		var out bytes.Buffer
		if err := encoder.Encode(&out, withFewerColours(sticker, bits)); err != nil {
			return nil, err
		}
		if out.Len() <= maxStaticBytes {
			return out.Bytes(), nil
		}
	}
	return nil, ErrTooBig
}

// withFewerColours is a copy of img with the low bits of each colour
// dropped; img itself when there are none to drop. Transparency is kept.
func withFewerColours(img *image.NRGBA, bits uint) *image.NRGBA {
	if bits == 0 {
		return img
	}
	fewer := *img
	fewer.Pix = make([]byte, len(img.Pix))
	mask := byte(0xFF << bits)
	for i, v := range img.Pix {
		if i%4 != 3 {
			v &= mask
		}
		fewer.Pix[i] = v
	}
	return &fewer
}
