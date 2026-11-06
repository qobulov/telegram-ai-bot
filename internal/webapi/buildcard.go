package webapi

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// A build card is one picture of a build: a title, then rows of icons,
// each under a heading and over its name. The game's icons are 100 by 100.
const (
	cardPadding  = 24
	cardCell     = 132
	cardIconSize = 96
	cardTitle    = 64
	cardHeading  = 30
	cardLabel    = 40
	cardRowGap   = 14
	cardMaxCells = 6
	titleSize    = 24
	subtitleSize = 15
	headingSize  = 15
	labelSize    = 13
)

var (
	cardBackground = color.RGBA{R: 0x16, G: 0x1b, B: 0x26, A: 0xff}
	cardMissing    = color.RGBA{R: 0x3a, G: 0x40, B: 0x4d, A: 0xff}
	cardText       = color.RGBA{R: 0xf2, G: 0xf4, B: 0xf8, A: 0xff}
	cardMuted      = color.RGBA{R: 0x9a, G: 0xa3, B: 0xb5, A: 0xff}
	cardAccent     = color.RGBA{R: 0xe8, G: 0xc1, B: 0x5c, A: 0xff}
)

type buildCard struct {
	Title    string
	Subtitle string
	Rows     []cardRow
}

type cardRow struct {
	Heading string
	Icons   []cardIcon
}

// cardIcon is one icon and its name; a nil Picture is drawn as a blank
// square, so one icon that would not load leaves the rest.
type cardIcon struct {
	Name    string
	Picture image.Image
}

// cardFaces are the fonts, parsed once.
var cardFaces = sync.OnceValues(func() (map[string]font.Face, error) {
	regular, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}
	bold, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil, err
	}
	faces := map[string]font.Face{}
	for name, spec := range map[string]struct {
		font *opentype.Font
		size float64
	}{"title": {bold, titleSize}, "subtitle": {regular, subtitleSize}, "heading": {bold, headingSize}, "label": {regular, labelSize}} {
		face, err := opentype.NewFace(spec.font, &opentype.FaceOptions{Size: spec.size, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil, err
		}
		faces[name] = face
	}
	return faces, nil
})

// drawBuildCard draws card as a PNG.
func drawBuildCard(card buildCard) ([]byte, error) {
	faces, err := cardFaces()
	if err != nil {
		return nil, err
	}
	cells := 1
	height := cardPadding + cardTitle
	for _, row := range card.Rows {
		cells = max(cells, min(len(row.Icons), cardMaxCells))
		height += cardHeading + cardIconSize + cardLabel + cardRowGap
	}
	width := 2*cardPadding + cells*cardCell
	canvas := image.NewRGBA(image.Rect(0, 0, width, height+cardPadding-cardRowGap))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(cardBackground), image.Point{}, draw.Src)

	writeText(canvas, faces["title"], cardAccent, cardPadding, cardPadding+titleSize, card.Title)
	writeText(canvas, faces["subtitle"], cardMuted, cardPadding, cardPadding+titleSize+subtitleSize+12, card.Subtitle)
	y := cardPadding + cardTitle
	for _, row := range card.Rows {
		writeText(canvas, faces["heading"], cardText, cardPadding, y+headingSize, row.Heading)
		top := y + cardHeading
		for i, icon := range row.Icons[:min(len(row.Icons), cardMaxCells)] {
			left := cardPadding + i*cardCell + (cardCell-cardIconSize)/2
			drawIcon(canvas, image.Rect(left, top, left+cardIconSize, top+cardIconSize), icon.Picture)
			for j, line := range wrapLabel(faces["label"], icon.Name, cardCell-8) {
				textWidth := font.MeasureString(faces["label"], line).Ceil()
				writeText(canvas, faces["label"], cardMuted, cardPadding+i*cardCell+(cardCell-textWidth)/2, top+cardIconSize+labelSize+6+j*(labelSize+3), line)
			}
		}
		y += cardHeading + cardIconSize + cardLabel + cardRowGap
	}
	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// maxIconSide bounds a picture taken for an icon; the game's are 100 wide.
const maxIconSide = 1024

func drawIcon(canvas *image.RGBA, place image.Rectangle, picture image.Image) {
	if picture == nil || picture.Bounds().Empty() || picture.Bounds().Dx() > maxIconSide || picture.Bounds().Dy() > maxIconSide {
		draw.Draw(canvas, place, image.NewUniform(cardMissing), image.Point{}, draw.Src)
		return
	}
	xdraw.CatmullRom.Scale(canvas, place, picture, picture.Bounds(), draw.Over, nil)
}

func writeText(canvas *image.RGBA, face font.Face, c color.Color, x, baseline int, text string) {
	d := font.Drawer{Dst: canvas, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, baseline)}
	d.DrawString(text)
}

// wrapLabel breaks a name into at most two lines of width, ending the
// second with an ellipsis when the name is longer.
func wrapLabel(face font.Face, name string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(name) {
		next := strings.TrimSpace(line + " " + word)
		if line != "" && font.MeasureString(face, next).Ceil() > width {
			lines = append(lines, line)
			line = word
			continue
		}
		line = next
	}
	lines = append(lines, line)
	if len(lines) > 2 {
		lines = append(lines[:1], strings.Join(lines[1:], " "))
	}
	for i, l := range lines {
		for font.MeasureString(face, l).Ceil() > width && len([]rune(l)) > 1 {
			l = string([]rune(l)[:len([]rune(l))-2]) + "…"
		}
		lines[i] = l
	}
	return lines
}
