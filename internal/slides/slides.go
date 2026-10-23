// Package slides writes a presentation as a PowerPoint (.pptx) file: a title
// slide and slides of a title and bullet points, in one of a few color
// themes. It writes the Office Open XML package itself, which PowerPoint,
// Google Slides, Keynote and LibreOffice all open.
package slides

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Deck is a presentation.
type Deck struct {
	Title    string
	Subtitle string
	Slides   []Slide
	// Theme is one of Themes; "" is the first.
	Theme string
}

// Slide is a content slide: a title and its points.
type Slide struct {
	Title   string
	Bullets []string
}

// Limits on a deck, so that every slide fits and the file stays small.
const (
	MaxSlides      = 12
	MaxBullets     = 6
	MaxTitleRunes  = 100
	MaxBulletRunes = 220
)

// palette is a theme's colors, as RGB hex.
type palette struct {
	background, title, text, accent, soft string
}

var palettes = map[string]palette{
	"blue":   {background: "FFFFFF", title: "1F3864", text: "262626", accent: "2E75B6", soft: "DEEBF7"},
	"green":  {background: "FFFFFF", title: "1E4D2B", text: "262626", accent: "2E8B57", soft: "E2F0D9"},
	"purple": {background: "FFFFFF", title: "3B2160", text: "262626", accent: "7030A0", soft: "EDE3F5"},
	"dark":   {background: "1E1E2E", title: "FFFFFF", text: "E6E6E6", accent: "F4B942", soft: "2E2E44"},
}

// Themes are the color themes a deck may have.
var Themes = []string{"blue", "green", "purple", "dark"}

// ErrEmpty reports a deck without a title or slides.
var ErrEmpty = errors.New("slides: a deck needs a title and at least one slide")

// Slide size, 16:9, in EMU (914400 to the inch).
const (
	slideWidth  = 12192000
	slideHeight = 6858000
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

// --- Slides ---

const slideHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">`

const treeHead = `<p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`

func background(color string) string {
	return `<p:bg><p:bgPr><a:solidFill><a:srgbClr val="` + color + `"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>`
}

// rect is a filled rectangle without text.
func rect(id int, x, y, cx, cy int, color string) string {
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Shape %d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr></p:sp>`,
		id, id, x, y, cx, cy, color)
}

// textBox is a box of paragraphs, anchored at the top, middle or bottom
// ("t", "ctr", "b").
func textBox(id int, x, y, cx, cy int, anchor, paragraphs string) string {
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Text %d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr><p:txBody><a:bodyPr wrap="square" lIns="91440" tIns="45720" rIns="91440" bIns="45720" anchor="%s"><a:noAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`,
		id, id, x, y, cx, cy, anchor, paragraphs)
}

// run is a paragraph of one run of text; size is in points. A bullet
// color makes it a bullet point.
func run(text string, size int, color string, bold bool, align, bullet string) string {
	b := "0"
	if bold {
		b = "1"
	}
	props := `<a:pPr algn="` + align + `"><a:spcBef><a:spcPts val="600"/></a:spcBef><a:buNone/></a:pPr>`
	if bullet != "" {
		props = `<a:pPr marL="457200" indent="-342900" algn="l"><a:lnSpc><a:spcPct val="100000"/></a:lnSpc><a:spcBef><a:spcPts val="1200"/></a:spcBef><a:buClr><a:srgbClr val="` + bullet + `"/></a:buClr><a:buFont typeface="Arial"/><a:buChar char="&#8226;"/></a:pPr>`
	}
	return fmt.Sprintf(`<a:p>%s<a:r><a:rPr lang="en-US" sz="%d" b="%s" dirty="0"><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:latin typeface="Arial"/><a:cs typeface="Arial"/></a:rPr><a:t>%s</a:t></a:r></a:p>`,
		props, size*100, b, color, esc(text))
}

func titleSlide(deck Deck, c palette) string {
	var b strings.Builder
	b.WriteString(slideHead + `<p:cSld>` + background(c.background) + treeHead)
	b.WriteString(rect(2, 0, 0, slideWidth, slideHeight, c.soft))
	b.WriteString(rect(3, 0, 0, 228600, slideHeight, c.accent))
	b.WriteString(rect(4, 838200, 3886200, 2286000, 54864, c.accent))
	titleSize := 44
	if utf8.RuneCountInString(deck.Title) > 50 {
		titleSize = 36
	}
	b.WriteString(textBox(5, 838200, 1371600, 10515600, 2438400, "b", run(deck.Title, titleSize, c.title, true, "l", "")))
	if subtitle := strings.TrimSpace(deck.Subtitle); subtitle != "" {
		b.WriteString(textBox(6, 838200, 4114800, 10515600, 1371600, "t", run(subtitle, 22, c.text, false, "l", "")))
	}
	b.WriteString(`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`)
	return b.String()
}

// bulletSize is the point size at which a slide's points fit its box.
func bulletSize(bullets []string) int {
	chars := 0
	for _, bullet := range bullets {
		chars += utf8.RuneCountInString(bullet)
	}
	switch {
	case chars <= 250 && len(bullets) <= 4:
		return 26
	case chars <= 450:
		return 22
	case chars <= 700:
		return 19
	default:
		return 16
	}
}

func contentSlide(s Slide, number, total int, c palette) string {
	var b strings.Builder
	b.WriteString(slideHead + `<p:cSld>` + background(c.background) + treeHead)
	b.WriteString(rect(2, 0, 0, slideWidth, 91440, c.accent))
	titleSize := 34
	if utf8.RuneCountInString(s.Title) > 45 {
		titleSize = 28
	}
	b.WriteString(textBox(3, 609600, 304800, 10972800, 1005840, "b", run(s.Title, titleSize, c.title, true, "l", "")))
	b.WriteString(rect(4, 685800, 1371600, 1371600, 45720, c.accent))
	var points strings.Builder
	size := bulletSize(s.Bullets)
	for _, bullet := range s.Bullets {
		points.WriteString(run(bullet, size, c.text, false, "l", c.accent))
	}
	b.WriteString(textBox(5, 609600, 1600200, 10972800, 4572000, "t", points.String()))
	b.WriteString(textBox(6, 10363200, 6309360, 1219200, 365760, "ctr", run(fmt.Sprintf("%d / %d", number, total), 12, c.accent, false, "r", "")))
	b.WriteString(`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`)
	return b.String()
}

// --- Package parts ---

func contentTypes(slides int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/><Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/><Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/><Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/><Override PartName="/ppt/presProps.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presProps+xml"/><Override PartName="/ppt/viewProps.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.viewProps+xml"/><Override PartName="/ppt/tableStyles.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.tableStyles+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/><Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>`)
	for i := 1; i <= slides; i++ {
		fmt.Fprintf(&b, `<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, i)
	}
	b.WriteString(`</Types>`)
	return b.String()
}

const rootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>`

func coreProps(title string) string {
	now := time.Now().UTC().Format(time.RFC3339)
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dc:title>` + esc(title) + `</dc:title><dc:creator>Maria</dc:creator><dcterms:created xsi:type="dcterms:W3CDTF">` + now + `</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">` + now + `</dcterms:modified></cp:coreProperties>`
}

func appProps(slides int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"><Application>Microsoft Office PowerPoint</Application><PresentationFormat>Widescreen</PresentationFormat><Slides>%d</Slides></Properties>`, slides)
}

func presentation(slides int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presentation xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" saveSubsetFonts="1"><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>`)
	for i := 1; i <= slides; i++ {
		fmt.Fprintf(&b, `<p:sldId id="%d" r:id="rId%d"/>`, 255+i, 10+i)
	}
	fmt.Fprintf(&b, `</p:sldIdLst><p:sldSz cx="%d" cy="%d"/><p:notesSz cx="6858000" cy="9144000"/><p:defaultTextStyle><a:defPPr><a:defRPr lang="en-US"/></a:defPPr></p:defaultTextStyle></p:presentation>`, slideWidth, slideHeight)
	return b.String()
}

func presentationRels(slides int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="theme/theme1.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/presProps" Target="presProps.xml"/><Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/viewProps" Target="viewProps.xml"/><Relationship Id="rId5" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/tableStyles" Target="tableStyles.xml"/>`)
	for i := 1; i <= slides; i++ {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`, 10+i, i)
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

const presProps = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presentationPr xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"/>`

const viewProps = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:viewPr xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:normalViewPr><p:restoredLeft sz="15620"/><p:restoredTop sz="94660"/></p:normalViewPr><p:gridSpacing cx="76200" cy="76200"/></p:viewPr>`

const tableStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<a:tblStyleLst xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" def="{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}"/>`

const slideRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>`

const slideMasterRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="../theme/theme1.xml"/></Relationships>`

const slideLayoutRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/></Relationships>`

const slideMaster = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldMaster xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:bg><p:bgRef idx="1001"><a:schemeClr val="bg1"/></p:bgRef></p:bg>` + treeHead + `</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle><a:lvl1pPr><a:defRPr sz="4400"/></a:lvl1pPr></p:titleStyle><p:bodyStyle><a:lvl1pPr><a:defRPr sz="2400"/></a:lvl1pPr></p:bodyStyle><p:otherStyle><a:lvl1pPr><a:defRPr sz="1800"/></a:lvl1pPr></p:otherStyle></p:txStyles></p:sldMaster>`

const slideLayout = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldLayout xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" type="blank" preserve="1"><p:cSld name="Blank">` + treeHead + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`

const theme = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Maria"><a:themeElements><a:clrScheme name="Maria"><a:dk1><a:srgbClr val="000000"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="1F3864"/></a:dk2><a:lt2><a:srgbClr val="E7E6E6"/></a:lt2><a:accent1><a:srgbClr val="2E75B6"/></a:accent1><a:accent2><a:srgbClr val="ED7D31"/></a:accent2><a:accent3><a:srgbClr val="A5A5A5"/></a:accent3><a:accent4><a:srgbClr val="FFC000"/></a:accent4><a:accent5><a:srgbClr val="5B9BD5"/></a:accent5><a:accent6><a:srgbClr val="70AD47"/></a:accent6><a:hlink><a:srgbClr val="0563C1"/></a:hlink><a:folHlink><a:srgbClr val="954F72"/></a:folHlink></a:clrScheme><a:fontScheme name="Maria"><a:majorFont><a:latin typeface="Arial"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="Arial"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme><a:fmtScheme name="Maria"><a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst><a:lnStyleLst><a:ln w="6350"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="12700"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="19050"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst><a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst><a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst></a:fmtScheme></a:themeElements><a:objectDefaults/><a:extraClrSchemeLst/></a:theme>`
