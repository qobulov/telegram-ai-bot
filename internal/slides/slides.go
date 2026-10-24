// Package slides writes a presentation as a PowerPoint (.pptx) file: a title
// slide and content slides laid out as points, cards, steps, a grid of
// tiles, a two-column table or a statement, in one of a few color themes.
// It writes the Office Open XML package itself, which PowerPoint, Google
// Slides, Keynote and LibreOffice all open.
package slides

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Deck is a presentation.
type Deck struct {
	Title    string
	Subtitle string
	// Author is a line at the foot of the title slide, such as who made it.
	Author string
	Slides []Slide
	// Theme is one of Themes; "" is the first.
	Theme string
}

// Slide layouts.
const (
	// LayoutPoints is points down the slide, each a bold term and its
	// explanation; with an Icon, a picture of it on the right.
	LayoutPoints = "points"
	// LayoutCards is two to four cards side by side.
	LayoutCards = "cards"
	// LayoutSteps is cards numbered as steps.
	LayoutSteps = "steps"
	// LayoutGrid is four to six tiles, three to a row.
	LayoutGrid = "grid"
	// LayoutTable is a two-column table: a term and its explanation.
	LayoutTable = "table"
	// LayoutStatement is one statement, large, such as a key idea or a
	// conclusion.
	LayoutStatement = "statement"
)

// Layouts are the layouts a slide may have.
var Layouts = []string{LayoutPoints, LayoutCards, LayoutSteps, LayoutGrid, LayoutTable, LayoutStatement}

// Slide is a content slide.
type Slide struct {
	Layout string
	// Eyebrow is a short label above the title, such as "YO'NALISH 01".
	Eyebrow string
	Title   string
	// Intro is a sentence under the title; for a statement, the statement.
	Intro string
	Items []Item
	// Icon is an emoji shown large beside points.
	Icon string
	// Columns are a table's two headings.
	Columns [2]string
	// Note is a line at the foot of the slide.
	Note string
}

// Item is a point, card, step, tile or table row: a short title and its
// text; either may be empty, not both.
type Item struct {
	Icon  string
	Title string
	Text  string
}

// Limits on a deck, so that every slide fits and the file stays small.
const (
	MaxSlides     = 15
	MaxItems      = 6
	MaxTitleRunes = 100
	MaxItemTitle  = 70
	MaxItemText   = 240
	MaxIntroRunes = 260
)

// palette is a theme's colors, as RGB hex.
type palette struct {
	background, title, text, muted, accent, strong, card, tile, line string
}

var palettes = map[string]palette{
	"classic": {background: "E9EEF3", title: "1E2A44", text: "2B3445", muted: "5B6475", accent: "F2B5A4", strong: "2E5D50", card: "FFFFFF", tile: "F2BBAA", line: "C9D2DB"},
	"navy":    {background: "14213D", title: "FFFFFF", text: "E5E7EB", muted: "A3AEC2", accent: "FCA311", strong: "FCA311", card: "1D2D50", tile: "22335C", line: "33466E"},
	"green":   {background: "EEF4EF", title: "1E3A2F", text: "26332C", muted: "5E6E64", accent: "8FC1A9", strong: "2E6B4F", card: "FFFFFF", tile: "CFE6D8", line: "C6D8CC"},
	"rose":    {background: "FBF1EE", title: "4A1F2B", text: "3A2A2E", muted: "7A5F66", accent: "E8A0A8", strong: "9C3D54", card: "FFFFFF", tile: "F6D5D8", line: "E8CDD1"},
}

// Themes are the color themes a deck may have.
var Themes = []string{"classic", "navy", "green", "rose"}

// ErrEmpty reports a deck without a title or slides.
var ErrEmpty = errors.New("slides: a deck needs a title and at least one slide")

// Sizes in EMU: 914400 to the inch, 12700 to the point. Slides are 16:9.
const (
	inch        = 914400
	point       = 12700
	slideWidth  = 12192000
	slideHeight = 6858000
	margin      = inch * 6 / 10
	contentW    = slideWidth - 2*margin
	bottom      = slideHeight - inch*55/100
)

// Fonts: a serif for titles, as in a printed book, and a sans for text.
const (
	serif = "Georgia"
	sans  = "Arial"
	emoji = "Segoe UI Emoji"
)

// Build writes the deck as a .pptx file.
func Build(deck Deck) ([]byte, error) {
	if strings.TrimSpace(deck.Title) == "" || len(deck.Slides) == 0 {
		return nil, ErrEmpty
	}
	if len(deck.Slides) > MaxSlides {
		return nil, fmt.Errorf("slides: at most %d slides", MaxSlides)
	}
	colors, ok := palettes[deck.Theme]
	if !ok {
		colors = palettes[Themes[0]]
	}
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	total := len(deck.Slides) + 1
	files := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypes(total)},
		{"_rels/.rels", rootRels},
		{"docProps/core.xml", coreProps(deck.Title)},
		{"docProps/app.xml", appProps(total)},
		{"ppt/presentation.xml", presentation(total)},
		{"ppt/_rels/presentation.xml.rels", presentationRels(total)},
		{"ppt/presProps.xml", presProps},
		{"ppt/viewProps.xml", viewProps},
		{"ppt/tableStyles.xml", tableStyles},
		{"ppt/theme/theme1.xml", theme},
		{"ppt/slideMasters/slideMaster1.xml", slideMaster},
		{"ppt/slideMasters/_rels/slideMaster1.xml.rels", slideMasterRels},
		{"ppt/slideLayouts/slideLayout1.xml", slideLayout},
		{"ppt/slideLayouts/_rels/slideLayout1.xml.rels", slideLayoutRels},
		{"ppt/slides/slide1.xml", titleSlide(deck, colors)},
	}
	for i, s := range deck.Slides {
		files = append(files, struct{ name, body string }{fmt.Sprintf("ppt/slides/slide%d.xml", i+2), contentSlide(s, i+2, total, colors)})
	}
	for i := 1; i <= total; i++ {
		files = append(files, struct{ name, body string }{fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", i), slideRels})
	}
	for _, f := range files {
		w, err := archive.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(f.body)); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FileName is a file name for the deck, from its title.
func FileName(title string) string {
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < ' ' {
			return -1
		}
		return r
	}, strings.TrimSpace(title))
	name = strings.Join(strings.Fields(name), " ")
	if utf8.RuneCountInString(name) > 60 {
		name = string([]rune(name)[:60])
	}
	if name == "" {
		name = "presentation"
	}
	return strings.TrimSpace(name) + ".pptx"
}

// esc makes text safe in XML, dropping characters XML cannot hold.
func esc(text string) string {
	text = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || (r >= 0x20 && r != 0xFFFE && r != 0xFFFF) {
			return r
		}
		return -1
	}, text)
	var b strings.Builder
	for _, r := range text {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// --- Text that fits ---

// fit is the largest point size from max down to min at which text fills
// at most lines lines of a box width EMU wide.
func fit(text string, width, lines, max, min int) int {
	for size := max; size > min; size-- {
		if linesFor(text, width, size) <= lines {
			return size
		}
	}
	return min
}

// linesFor is how many lines text takes at size in a box width EMU wide,
// wrapping at words. A character is taken as half the size wide, and
// Cyrillic, which is wider, as 0.55.
func linesFor(text string, width, size int) int {
	charWidth := 0.5
	if strings.IndexFunc(text, func(r rune) bool { return r >= 0x400 && r <= 0x4FF }) >= 0 {
		charWidth = 0.55
	}
	perLine := float64(width) / float64(point) / (float64(size) * charWidth)
	lines, used := 1, 0.0
	for _, word := range strings.Fields(text) {
		n := float64(utf8.RuneCountInString(word))
		switch {
		case used == 0:
			used = n
		case used+1+n <= perLine:
			used += 1 + n
		default:
			lines++
			used = n
		}
		for used > perLine {
			lines++
			used -= perLine
		}
	}
	return lines
}

// fitBlock is the largest size from max down to min at which the given
// paragraphs fit a box width wide and height high.
func fitBlock(paragraphs []string, width, height, max, min int) int {
	for size := max; size > min; size-- {
		total := 0
		for _, p := range paragraphs {
			total += linesFor(p, width, size)
		}
		if float64(total)*float64(size)*1.3*point+float64(len(paragraphs))*float64(size)*0.6*point <= float64(height) {
			return size
		}
	}
	return min
}

// fitEach is the largest size from max down to min at which each of
// texts fits a box of its own, width wide and height high.
func fitEach(texts []string, width, height, max, min int) int {
	size := max
	for _, text := range texts {
		if text != "" {
			size = fitBlock([]string{text}, width, height, size, min)
		}
	}
	return size
}

// textHeight is how high text is at size in a box width wide.
func textHeight(text string, width, size int) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	return linesFor(text, width, size) * size * 13 / 10 * point
}

// --- Shapes ---

const slideHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">`

const treeHead = `<p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`

// canvas collects a slide's shapes, numbering them.
type canvas struct {
	b      strings.Builder
	nextID int
	colors palette
}

func newCanvas(colors palette) *canvas {
	c := &canvas{nextID: 2, colors: colors}
	c.b.WriteString(slideHead + `<p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="` + colors.background + `"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>` + treeHead)
	return c
}

func (c *canvas) id() int {
	c.nextID++
	return c.nextID - 1
}

func (c *canvas) String() string {
	return c.b.String() + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
}

// shape is a filled shape: "rect", "roundRect" or "ellipse"; an empty
// line color draws no outline, and alpha is the fill's opacity in percent.
func (c *canvas) shape(geometry string, x, y, w, h int, fill string, alpha int, line string) {
	fillXML := `<a:noFill/>`
	if fill != "" {
		opacity := ""
		if alpha > 0 && alpha < 100 {
			opacity = fmt.Sprintf(`<a:alpha val="%d"/>`, alpha*1000)
		}
		fillXML = `<a:solidFill><a:srgbClr val="` + fill + `">` + opacity + `</a:srgbClr></a:solidFill>`
	}
	lineXML := `<a:ln><a:noFill/></a:ln>`
	if line != "" {
		lineXML = `<a:ln w="15875"><a:solidFill><a:srgbClr val="` + line + `"/></a:solidFill></a:ln>`
	}
	adjust := `<a:avLst/>`
	if geometry == "roundRect" {
		adjust = `<a:avLst><a:gd name="adj" fmla="val 4000"/></a:avLst>`
	}
	id := c.id()
	fmt.Fprintf(&c.b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Shape %d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="%s">%s</a:prstGeom>%s%s</p:spPr></p:sp>`,
		id, id, x, y, w, h, geometry, adjust, fillXML, lineXML)
}

// run is a run of text in a paragraph.
type run struct {
	text  string
	size  int
	color string
	font  string
	bold  bool
}

func (r run) xml() string {
	bold := "0"
	if r.bold {
		bold = "1"
	}
	return fmt.Sprintf(`<a:r><a:rPr lang="uz-Latn-UZ" sz="%d" b="%s" dirty="0"><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:latin typeface="%s"/><a:ea typeface="%s"/><a:cs typeface="%s"/><a:sym typeface="%s"/></a:rPr><a:t>%s</a:t></a:r>`,
		r.size*100, bold, r.color, r.font, r.font, r.font, r.font, esc(r.text))
}

// paragraph is a paragraph's runs, aligned "l", "ctr" or "r", with space
// before it in points and its line spacing in percent.
type paragraph struct {
	runs        []run
	align       string
	spaceBefore int
	lineSpacing int
}

func (p paragraph) xml() string {
	spacing := p.lineSpacing
	if spacing == 0 {
		spacing = 100
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<a:p><a:pPr algn="%s"><a:lnSpc><a:spcPct val="%d"/></a:lnSpc><a:spcBef><a:spcPts val="%d"/></a:spcBef><a:buNone/></a:pPr>`,
		p.align, spacing*1000, p.spaceBefore*100)
	for _, r := range p.runs {
		b.WriteString(r.xml())
	}
	b.WriteString(`</a:p>`)
	return b.String()
}

// text is a box of paragraphs, anchored "t", "ctr" or "b".
func (c *canvas) text(x, y, w, h int, anchor string, paragraphs ...paragraph) {
	id := c.id()
	fmt.Fprintf(&c.b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Text %d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr><p:txBody><a:bodyPr wrap="square" lIns="0" tIns="0" rIns="0" bIns="0" anchor="%s"><a:noAutofit/></a:bodyPr><a:lstStyle/>`,
		id, id, x, y, w, h, anchor)
	for _, p := range paragraphs {
		c.b.WriteString(p.xml())
	}
	c.b.WriteString(`</p:txBody></p:sp>`)
}

// line is one paragraph of one run.
func line(text string, size int, color, font string, bold bool, align string) paragraph {
	return paragraph{runs: []run{{text: text, size: size, color: color, font: font, bold: bold}}, align: align}
}

// --- Slides ---

func titleSlide(deck Deck, c palette) string {
	cv := newCanvas(c)
	// The right side is a still life of overlapping circles in the
	// theme's colors, where Gamma would put a picture.
	cv.shape("ellipse", inch*86/10, inch*8/10, inch*45/10, inch*45/10, c.accent, 70, "")
	cv.shape("ellipse", inch*98/10, inch*30/10, inch*34/10, inch*34/10, c.strong, 55, "")
	cv.shape("ellipse", inch*80/10, inch*42/10, inch*22/10, inch*22/10, c.tile, 85, "")
	cv.shape("ellipse", inch*112/10, inch*5/10, inch*12/10, inch*12/10, c.title, 25, "")
	panelW := inch * 82 / 10
	cv.shape("roundRect", margin, margin, panelW, slideHeight-2*margin, c.card, 80, "")
	inner := panelW - inch*8/10
	x := margin + inch*4/10
	titleSize := fit(deck.Title, inner, 4, 54, 30)
	cv.text(x, margin+inch*4/10, inner, inch*36/10, "b", line(deck.Title, titleSize, c.title, serif, false, "l"))
	y := margin + inch*41/10
	if subtitle := strings.TrimSpace(deck.Subtitle); subtitle != "" {
		cv.text(x, y, inner, inch*12/10, "t", line(subtitle, fit(subtitle, inner, 3, 22, 14), c.text, sans, false, "l"))
	}
	if author := strings.TrimSpace(deck.Author); author != "" {
		cv.text(x, slideHeight-margin-inch*8/10, inner, inch*5/10, "b", line(author, fit(author, inner, 2, 16, 11), c.muted, sans, false, "l"))
	}
	return cv.String()
}

// header draws the eyebrow, title and intro of a content slide over width
// from x, and returns where the content below them starts.
func header(cv *canvas, s Slide, x, width int) int {
	c := cv.colors
	y := inch * 5 / 10
	if eyebrow := strings.TrimSpace(s.Eyebrow); eyebrow != "" {
		cv.text(x, y, width, inch*35/100, "t", line(strings.ToUpper(eyebrow), 12, c.muted, sans, true, "l"))
		y += inch * 4 / 10
	}
	titleSize := fit(s.Title, width, 2, 36, 24)
	titleH := linesFor(s.Title, width, titleSize) * titleSize * 13 / 10 * point
	cv.text(x, y, width, titleH, "t", line(s.Title, titleSize, c.title, serif, false, "l"))
	y += titleH + inch*2/10
	if intro := strings.TrimSpace(s.Intro); intro != "" && s.Layout != LayoutStatement {
		size := fit(intro, width, 2, 18, 14)
		h := linesFor(intro, width, size) * size * 14 / 10 * point
		cv.text(x, y, width, h, "t", line(intro, size, c.text, sans, false, "l"))
		y += h + inch*25/100
	}
	return y
}

func contentSlide(s Slide, number, total int, c palette) string {
	cv := newCanvas(c)
	switch s.Layout {
	case LayoutCards, LayoutSteps:
		cards(cv, s)
	case LayoutGrid:
		grid(cv, s)
	case LayoutTable:
		table(cv, s)
	case LayoutStatement:
		statement(cv, s)
	default:
		points(cv, s)
	}
	if note := strings.TrimSpace(s.Note); note != "" {
		width := contentW - inch*12/10
		cv.text(margin, bottom-inch/10, width, inch*4/10, "b", line(note, fit(note, width, 1, 13, 10), c.muted, sans, false, "l"))
	}
	cv.text(slideWidth-margin-inch, bottom-inch/10, inch, inch*4/10, "b", line(fmt.Sprintf("%d / %d", number, total), 11, c.muted, sans, false, "r"))
	return cv.String()
}

// points are a dot and a bold term and its text each, down the left; an
// icon fills the right.
func points(cv *canvas, s Slide) {
	c := cv.colors
	width := contentW
	if s.Icon != "" {
		width = contentW * 64 / 100
		size := inch * 36 / 10
		x, y := slideWidth-margin-size, (slideHeight-size)/2
		cv.shape("ellipse", x, y, size, size, c.accent, 45, "")
		cv.text(x, y, size, size, "ctr", line(s.Icon, 120, c.title, emoji, false, "ctr"))
	}
	top := header(cv, s, margin, width)
	if len(s.Items) == 0 {
		return
	}
	textX := margin + inch*55/100
	textW := width - inch*55/100
	slot := (bottom - inch*4/10 - top) / len(s.Items)
	var texts []string
	for _, item := range s.Items {
		texts = append(texts, strings.TrimSpace(item.Title+" "+item.Text))
	}
	size := fitBlock(texts, textW, slot*len(s.Items), 22, 13)
	for i, item := range s.Items {
		y := top + i*slot
		dot := size * point * 9 / 10
		cv.shape("ellipse", margin, y+size*point*15/100, dot, dot, c.accent, 0, "")
		var runs []run
		if item.Title != "" {
			title := item.Title
			if item.Text != "" && !strings.HasSuffix(title, ":") {
				title += ":"
			}
			runs = append(runs, run{text: title + " ", size: size, color: c.title, font: sans, bold: true})
		}
		if item.Text != "" {
			runs = append(runs, run{text: item.Text, size: size, color: c.text, font: sans})
		}
		cv.text(textX, y, textW, slot, "t", paragraph{runs: runs, align: "l", lineSpacing: 110})
	}
}

// cards are two to four cards side by side, as high as the fullest needs;
// steps number them.
func cards(cv *canvas, s Slide) {
	c := cv.colors
	top := header(cv, s, margin, contentW)
	n := len(s.Items)
	if n == 0 {
		return
	}
	gap := inch * 3 / 10
	w := (contentW - (n-1)*gap) / n
	room := bottom - inch*4/10 - top
	pad := inch * 3 / 10
	inner := w - 2*pad
	lead := 0 // the number and rule of a step, or an icon
	if s.Layout == LayoutSteps {
		lead = inch * 75 / 100
	} else {
		for _, item := range s.Items {
			if item.Icon != "" {
				lead = inch * 75 / 100
			}
		}
	}
	var titles, texts []string
	for _, item := range s.Items {
		titles, texts = append(titles, item.Title), append(texts, item.Text)
	}
	titleSize := fitEach(titles, inner, inch*9/10, 22, 15)
	titleRoom := 0
	for _, title := range titles {
		titleRoom = max(titleRoom, textHeight(title, inner, titleSize))
	}
	if titleRoom > 0 {
		titleRoom += inch * 15 / 100
	}
	textSize := fitEach(texts, inner, room-2*pad-lead-titleRoom, 18, 12)
	textRoom := 0
	for _, text := range texts {
		textRoom = max(textRoom, textHeight(text, inner, textSize)*11/10)
	}
	h := min(room, 2*pad+lead+titleRoom+textRoom+inch/10)
	for i, item := range s.Items {
		x := margin + i*(w+gap)
		cv.shape("roundRect", x, top, w, h, c.card, 0, c.strong)
		y := top + pad
		if s.Layout == LayoutSteps {
			cv.text(x+pad, y, inner, inch*4/10, "t", line(fmt.Sprintf("%02d", i+1), 20, c.strong, serif, true, "l"))
			cv.shape("rect", x+pad, y+inch*5/10, inner, point*2, c.accent, 0, "")
		} else if item.Icon != "" {
			cv.text(x+pad, y, inner, inch*6/10, "t", line(item.Icon, 30, c.title, emoji, false, "l"))
		}
		y += lead
		if item.Title != "" {
			cv.text(x+pad, y, inner, titleRoom, "t", line(item.Title, titleSize, c.title, sans, false, "l"))
		}
		y += titleRoom
		if item.Text != "" {
			cv.text(x+pad, y, inner, top+h-pad-y, "t", paragraph{runs: []run{{text: item.Text, size: textSize, color: c.text, font: sans}}, align: "l", lineSpacing: 110})
		}
	}
}

// grid is four to six tiles, three to a row, or two for four, as high as
// the fullest needs.
func grid(cv *canvas, s Slide) {
	c := cv.colors
	top := header(cv, s, margin, contentW)
	n := len(s.Items)
	if n == 0 {
		return
	}
	columns := 3
	if n == 4 || n <= 2 {
		columns = 2
	}
	rows := (n + columns - 1) / columns
	gap := inch * 3 / 10
	w := (contentW - (columns-1)*gap) / columns
	room := (bottom - inch*4/10 - top - (rows-1)*gap) / rows
	pad := inch * 3 / 10
	inner := w - 2*pad
	lead := 0
	for _, item := range s.Items {
		if item.Icon != "" {
			lead = inch * 75 / 100
		}
	}
	var all []string
	for _, item := range s.Items {
		all = append(all, strings.TrimSpace(item.Title+" "+item.Text))
	}
	size := fitEach(all, inner, room-2*pad-lead, 20, 12)
	need := 0
	for _, item := range s.Items {
		need = max(need, textHeight(item.Title, inner, size+2)+textHeight(item.Text, inner, size)*11/10)
	}
	h := min(room, max(2*pad+lead+need+inch/10, inch*15/10))
	for i, item := range s.Items {
		x := margin + (i%columns)*(w+gap)
		y := top + (i/columns)*(h+gap)
		cv.shape("rect", x, y, w, h, c.tile, 0, "")
		ty := y + pad
		if item.Icon != "" {
			cv.text(x+pad, ty, inner, inch*6/10, "t", line(item.Icon, 30, c.title, emoji, false, "l"))
			ty += lead
		}
		var paragraphs []paragraph
		if item.Title != "" {
			paragraphs = append(paragraphs, line(item.Title, size+2, c.title, sans, item.Text != "", "l"))
		}
		if item.Text != "" {
			p := line(item.Text, size, c.text, sans, false, "l")
			p.spaceBefore = 4
			paragraphs = append(paragraphs, p)
		}
		cv.text(x+pad, ty, inner, y+h-pad-ty, "t", paragraphs...)
	}
}

// table is a two-column table: headings, then a row for each item, its
// title bold on the left and its text on the right, a rule between rows.
func table(cv *canvas, s Slide) {
	c := cv.colors
	top := header(cv, s, margin, contentW)
	leftW := contentW * 26 / 100
	rightX := margin + leftW + inch*3/10
	rightW := contentW - leftW - inch*3/10
	pad := inch / 10
	if s.Columns[0] != "" || s.Columns[1] != "" {
		cv.text(margin+pad, top, leftW, inch*4/10, "t", line(s.Columns[0], 16, c.title, sans, true, "l"))
		cv.text(rightX, top, rightW, inch*4/10, "t", line(s.Columns[1], 16, c.title, sans, true, "l"))
		top += inch * 45 / 100
	}
	if len(s.Items) == 0 {
		return
	}
	var texts []string
	for _, item := range s.Items {
		texts = append(texts, item.Text)
	}
	slot := (bottom - inch*5/10 - top) / len(s.Items)
	size := fitBlock(texts, rightW, slot*len(s.Items)-len(s.Items)*inch/5, 18, 12)
	for i, item := range s.Items {
		y := top + i*slot
		cv.shape("rect", margin, y, contentW, point, c.line, 0, "")
		cv.text(margin+pad, y+inch/8, leftW-pad, slot-inch/8, "t", line(item.Title, size, c.title, sans, true, "l"))
		cv.text(rightX, y+inch/8, rightW, slot-inch/8, "t", paragraph{runs: []run{{text: item.Text, size: size, color: c.text, font: sans}}, align: "l", lineSpacing: 110})
	}
}

// statement is one statement, large, under a rule, with a line under it
// from the first item, such as its source.
func statement(cv *canvas, s Slide) {
	c := cv.colors
	header(cv, s, margin, contentW)
	text := strings.TrimSpace(s.Intro)
	if text == "" {
		return
	}
	width := contentW * 85 / 100
	x := (slideWidth - width) / 2
	size := fit(text, width, 4, 36, 22)
	h := linesFor(text, width, size) * size * 14 / 10 * point
	y := (slideHeight-h)/2 + inch*4/10
	cv.shape("rect", x, y-inch*35/100, inch*12/10, point*3, c.accent, 0, "")
	cv.text(x, y, width, h, "t", paragraph{runs: []run{{text: text, size: size, color: c.title, font: serif}}, align: "l", lineSpacing: 115})
	if len(s.Items) > 0 {
		by := strings.TrimSpace(s.Items[0].Title + " " + s.Items[0].Text)
		cv.text(x, y+h+inch*2/10, width, inch*5/10, "t", line("— "+by, 16, c.muted, sans, false, "l"))
	}
}
