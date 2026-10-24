package doctext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

// The fixtures are one small test, written by LibreOffice as Word 97,
// Word 2007 and PDF.
func TestExtractReadsEveryFormat(t *testing.T) {
	for _, name := range []string{"sample.doc", "sample.docx", "sample.pdf"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		text, err := Extract(Kind(name, ""), data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, want := range []string{"Iqtisodiyot nima o‘rganadi?", "#resurslardan foydalanishni", "++++", "#narx oshsa talab kamayadi"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: %q lacks %q", name, text, want)
			}
		}
	}
}

func TestKind(t *testing.T) {
	for name, want := range map[string]string{"a.DOC": "doc", "a.docx": "docx", "a.pdf": "pdf", "a.txt": "text", "a.zip": "", "a": ""} {
		if got := Kind(name, ""); got != want {
			t.Errorf("Kind(%q) = %q, want %q", name, got, want)
		}
	}
	if Kind("noname", "application/msword") != "doc" || Kind("noname", "application/pdf") != "pdf" {
		t.Error("the MIME type was not used")
	}
}

// Old Uzbek and Russian text files are often in Windows-1251; a binary
// file is no text.
func TestTextFiles(t *testing.T) {
	cyrillic, _ := charmap.Windows1251.NewEncoder().String("Тест саволлари")
	if text, err := Extract("text", []byte(cyrillic)); err != nil || text != "Тест саволлари" {
		t.Errorf("Windows-1251 = %q, %v", text, err)
	}
	if _, err := Extract("text", []byte{0xff, 0xfe, 0x00, 0x41}); err == nil {
		t.Error("a binary file was taken for text")
	}
}

func TestBrokenFilesAreErrors(t *testing.T) {
	for kind, data := range map[string][]byte{"doc": []byte("not a doc"), "docx": []byte("PK not a zip"), "pdf": []byte("%PDF-1.4 broken"), "zip": []byte("PK")} {
		if _, err := Extract(kind, data); err == nil {
			t.Errorf("%s: a broken file was read", kind)
		}
	}
}
