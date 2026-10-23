package slides

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleDeck(theme string) Deck {
	return Deck{
		Title:    "O'zbekistonda suv resurslaridan oqilona foydalanish",
		Subtitle: "Talaba: Aliyev Vali, 2-kurs · Toshkent irrigatsiya instituti",
		Theme:    theme,
		Slides: []Slide{
			{Title: "Muammoning dolzarbligi", Bullets: []string{"Orol dengizi 1960-yildan beri 90% ga qisqardi", "Sug'oriladigan yerlarning 50% dan ortig'i sho'rlangan", "Suv tanqisligi har yili 3–5 km³ ga oshmoqda"}},
			{Title: "Tomchilatib sug'orish texnologiyasi va uning afzalliklari qishloq xo'jaligida", Bullets: []string{
				"Suv sarfi an'anaviy usulga nisbatan 40–60% kam",
				"Hosildorlik 20–30% ga oshadi, chunki o'simlik ildiziga kerakli miqdorda suv va o'g'it yetkaziladi",
				"Begona o'tlar kamroq o'sadi, mehnat sarfi kamayadi",
				"Davlat subsidiyasi: xarajatlarning 50% gacha qoplanadi",
				"Qiyalik yerlarda ham samarali ishlaydi",
				"Kamchilik: boshlang'ich investitsiya yuqori (gektariga ~1 500–2 500 $)",
			}},
			{Title: "Xulosa", Bullets: []string{"Zamonaviy texnologiyalar & davlat yordami <muhim>", "Har bir fermer suvni tejashga mas'ul"}},
		},
	}
}

// Every part of the package is well-formed XML, every slide is listed,
// and text is escaped. SLIDES_SAMPLE_DIR, when set, keeps the files, to
// open them in an office suite.
func TestBuildWritesAWellFormedPackage(t *testing.T) {
	for _, theme := range Themes {
		data, err := Build(sampleDeck(theme))
		if err != nil {
			t.Fatal(err)
		}
		if dir := os.Getenv("SLIDES_SAMPLE_DIR"); dir != "" {
			if err := os.WriteFile(filepath.Join(dir, theme+".pptx"), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		parts := map[string]string{}
		for _, f := range archive.File {
			r, _ := f.Open()
			body, _ := io.ReadAll(r)
			r.Close()
			parts[f.Name] = string(body)
			decoder := xml.NewDecoder(bytes.NewReader(body))
			for {
				if _, err := decoder.Token(); err == io.EOF {
					break
				} else if err != nil {
					t.Fatalf("%s: %s is not well-formed: %v", theme, f.Name, err)
				}
			}
		}
		for _, name := range []string{"ppt/slides/slide1.xml", "ppt/slides/slide4.xml", "ppt/slides/_rels/slide4.xml.rels"} {
			if _, ok := parts[name]; !ok {
				t.Errorf("%s: no %s", theme, name)
			}
		}
		if _, ok := parts["ppt/slides/slide5.xml"]; ok {
			t.Errorf("%s: a slide too many", theme)
		}
		if !strings.Contains(parts["[Content_Types].xml"], "/ppt/slides/slide4.xml") || strings.Count(parts["ppt/presentation.xml"], "<p:sldId ") != 4 {
			t.Errorf("%s: the slides are not all listed", theme)
		}
		if !strings.Contains(parts["ppt/slides/slide4.xml"], "Zamonaviy texnologiyalar &amp; davlat yordami &lt;muhim&gt;") {
			t.Errorf("%s: text not escaped", theme)
		}
	}
}

func TestBuildRefusesEmptyAndHugeDecks(t *testing.T) {
	if _, err := Build(Deck{Title: "x"}); err != ErrEmpty {
		t.Errorf("no slides: %v", err)
	}
	if _, err := Build(Deck{Slides: []Slide{{Title: "a"}}}); err != ErrEmpty {
		t.Errorf("no title: %v", err)
	}
	if _, err := Build(Deck{Title: "x", Slides: make([]Slide, MaxSlides+1)}); err == nil {
		t.Error("too many slides were built")
	}
}

func TestBulletsShrinkToFit(t *testing.T) {
	short := []string{"bir", "ikki"}
	long := []string{strings.Repeat("a", 200), strings.Repeat("b", 200), strings.Repeat("c", 200), strings.Repeat("d", 200)}
	if bulletSize(short) <= bulletSize(long) {
		t.Errorf("sizes %d and %d", bulletSize(short), bulletSize(long))
	}
}

func TestFileName(t *testing.T) {
	for title, want := range map[string]string{
		"Suv resurslari: muammo/yechim?": "Suv resurslari muammoyechim.pptx",
		"   ":                            "presentation.pptx",
		strings.Repeat("a", 80):          strings.Repeat("a", 60) + ".pptx",
	} {
		if got := FileName(title); got != want {
			t.Errorf("FileName(%q) = %q, want %q", title, got, want)
		}
	}
}
