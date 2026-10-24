// Package doctext takes the text out of the files people send: Word
// documents old (.doc) and new (.docx), PDFs and plain text.
package doctext

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"github.com/richardlehane/mscfb"
	"golang.org/x/text/encoding/charmap"
)

// maxText bounds the text taken from one file, so that a small file cannot
// unpack into gigabytes.
const maxText = 20 << 20

// ErrUnknown reports a file of a kind this package cannot read.
var ErrUnknown = errors.New("doctext: not a .doc, .docx, .pdf or text file")

// Kind is how a file is read, by its name: "doc", "docx", "pdf", "text",
// or "" for a file this package cannot read.
func Kind(name, mimeType string) string {
	switch ext := strings.ToLower(path.Ext(name)); {
	case ext == ".pdf" || mimeType == "application/pdf":
		return "pdf"
	case ext == ".docx":
		return "docx"
	case ext == ".doc" || mimeType == "application/msword":
		return "doc"
	case ext == ".txt" || ext == ".md" || ext == ".csv" || ext == ".json" || ext == ".log" || ext == ".srt" || strings.HasPrefix(mimeType, "text/"):
		return "text"
	}
	return ""
}

// Extract is the text of a file of the given kind, a paragraph a line.
func Extract(kind string, data []byte) (string, error) {
	switch kind {
	case "doc":
		return Doc(data)
	case "docx":
		return Docx(data)
	case "pdf":
		return PDF(data)
	case "text":
		if bytes.IndexByte(data, 0) >= 0 {
			return "", errors.New("doctext: a binary file, not text")
		}
		if !utf8.Valid(data) {
			// Old Uzbek and Russian files are often in Windows-1251.
			decoded, err := charmap.Windows1251.NewDecoder().Bytes(data)
			if err != nil {
				return "", fmt.Errorf("doctext: not UTF-8 or Windows-1251 text")
			}
			return string(decoded), nil
		}
		return string(data), nil
	}
	return "", ErrUnknown
}

// --- .docx ---

// Docx is the text of a .docx file, a paragraph a line.
func Docx(data []byte) (string, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("doctext: not a .docx: %w", err)
	}
	for _, f := range archive.File {
		if f.Name != "word/document.xml" {
			continue
		}
		body, err := f.Open()
		if err != nil {
			return "", err
		}
		defer body.Close()
		return wordXMLText(io.LimitReader(body, maxText))
	}
	return "", errors.New("doctext: a .docx without word/document.xml")
}

// wordXMLText takes the text runs (w:t) out of WordprocessingML, ending a
// line at each paragraph and keeping tabs and breaks.
func wordXMLText(r io.Reader) (string, error) {
	var b strings.Builder
	decoder := xml.NewDecoder(r)
	inText := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("doctext: .docx: %w", err)
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				b.WriteByte('\t')
			case "br":
				b.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
	}
	return strings.TrimSpace(b.String()), nil
}

// --- .doc (Word 97-2003) ---

// Doc is the text of a Word 97-2003 file. Its text lies in the WordDocument
// stream in pieces, which the piece table in the table stream lists, each
// either 8-bit Windows-1252 or UTF-16.
func Doc(data []byte) (string, error) {
	streams, err := docStreams(data)
	if err != nil {
		return "", err
	}
	word := streams["WordDocument"]
	if len(word) < 0x1AA || binary.LittleEndian.Uint16(word) != 0xA5EC {
		return "", errors.New("doctext: not a Word document")
	}
	table := streams["0Table"]
	if binary.LittleEndian.Uint16(word[0x0A:])&0x0200 != 0 {
		table = streams["1Table"]
	}
	fcClx, lcbClx := binary.LittleEndian.Uint32(word[0x1A2:]), binary.LittleEndian.Uint32(word[0x1A6:])
	if uint64(fcClx)+uint64(lcbClx) > uint64(len(table)) || lcbClx == 0 {
		return "", errors.New("doctext: a Word document without a piece table")
	}
	pieces, err := pieceTable(table[fcClx : fcClx+lcbClx])
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, p := range pieces {
		if err := p.write(&b, word); err != nil {
			return "", err
		}
		if b.Len() > maxText {
			break
		}
	}
	return docClean(b.String()), nil
}

// docStreams reads the streams of a Compound File Binary, by name.
func docStreams(data []byte) (map[string][]byte, error) {
	reader, err := mscfb.New(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("doctext: not a .doc: %w", err)
	}
	streams := map[string][]byte{}
	for entry, err := reader.Next(); err == nil; entry, err = reader.Next() {
		switch entry.Name {
		case "WordDocument", "0Table", "1Table":
			body, err := io.ReadAll(io.LimitReader(entry, maxText))
			if err != nil {
				return nil, fmt.Errorf("doctext: .doc stream %s: %w", entry.Name, err)
			}
			streams[entry.Name] = body
		}
	}
	return streams, nil
}

// piece is a run of a .doc's text: chars characters from offset, 8-bit
// when compressed and UTF-16 otherwise.
type piece struct {
	offset     uint32
	chars      uint32
	compressed bool
}

// pieceTable reads the Clx: property runs to skip (0x01), then the piece
// table (0x02): n+1 character positions and n piece descriptors.
func pieceTable(clx []byte) ([]piece, error) {
	for len(clx) > 0 && clx[0] == 0x01 {
		if len(clx) < 3 {
			return nil, errors.New("doctext: a broken .doc piece table")
		}
		size := int(binary.LittleEndian.Uint16(clx[1:]))
		if 3+size > len(clx) {
			return nil, errors.New("doctext: a broken .doc piece table")
		}
		clx = clx[3+size:]
	}
	if len(clx) < 5 || clx[0] != 0x02 {
		return nil, errors.New("doctext: a .doc without a piece table")
	}
	size := int(binary.LittleEndian.Uint32(clx[1:]))
	plc := clx[5:]
	if size > len(plc) || size < 4 || (size-4)%12 != 0 {
		return nil, errors.New("doctext: a broken .doc piece table")
	}
	n := (size - 4) / 12
	pieces := make([]piece, 0, n)
	for i := range n {
		start, end := binary.LittleEndian.Uint32(plc[4*i:]), binary.LittleEndian.Uint32(plc[4*(i+1):])
		if end < start {
			return nil, errors.New("doctext: a broken .doc piece table")
		}
		descriptor := plc[4*(n+1)+8*i:]
		fc := binary.LittleEndian.Uint32(descriptor[2:])
		p := piece{offset: fc & 0x3FFFFFFF, chars: end - start, compressed: fc&0x40000000 != 0}
		if p.compressed {
			p.offset /= 2
		}
		pieces = append(pieces, p)
	}
	return pieces, nil
}

func (p piece) write(b *strings.Builder, word []byte) error {
	size := uint64(p.chars)
	if !p.compressed {
		size *= 2
	}
	if uint64(p.offset)+size > uint64(len(word)) {
		return errors.New("doctext: a .doc piece past the end of its text")
	}
	raw := word[p.offset : uint64(p.offset)+size]
	if p.compressed {
		text, err := charmap.Windows1252.NewDecoder().Bytes(raw)
		if err != nil {
			return err
		}
		b.Write(text)
		return nil
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[2*i:])
	}
	b.WriteString(string(utf16.Decode(units)))
	return nil
}

// docClean turns Word's control characters into text: paragraph and cell
// marks end a line, a field keeps its result and loses its code.
func docClean(text string) string {
	var b strings.Builder
	inFieldCode := 0
	for _, r := range text {
		switch r {
		case 0x13: // field begins: its code follows
			inFieldCode++
		case 0x14: // field separator: its result follows
			if inFieldCode > 0 {
				inFieldCode--
			}
		case 0x15: // field ends
		case '\r', 0x07, 0x0B, 0x0C:
			if inFieldCode == 0 {
				b.WriteByte('\n')
			}
		default:
			if inFieldCode == 0 && (r >= ' ' || r == '\t') {
				b.WriteRune(r)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// --- PDF ---

// PDF is the text of a PDF, row by row as the pages lay it out.
func PDF(data []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("doctext: not a PDF: %w", err)
	}
	var b strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		rows, err := page.GetTextByRow()
		if err != nil {
			return "", fmt.Errorf("doctext: PDF page %d: %w", i, err)
		}
		for _, row := range rows {
			for _, word := range row.Content {
				b.WriteString(word.S)
			}
			b.WriteByte('\n')
		}
		if b.Len() > maxText {
			break
		}
	}
	return strings.TrimSpace(b.String()), nil
}
