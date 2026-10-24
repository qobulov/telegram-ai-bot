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
		Title:    "Pedagog-psixologning asosiy faoliyat turlari",
		Subtitle: "Ta'lim muassasasida psixologik xizmatni tashkillashtirish",
		Author:   "Bajardi: 23-05 guruh talabasi Eshkuziyeva Sabohat",
		Theme:    theme,
		Slides: []Slide{
			{Layout: LayoutPoints, Eyebrow: "Psixologik xizmat missiyasi", Title: "Sog'lom ruhiy muhit va shaxs kamoloti", Icon: "🌱", Items: []Item{
				{Title: "Shaxs rivoji", Text: "O'quvchining qobiliyat va imkoniyatlarini har tomonlama ro'yobga chiqarish"},
				{Title: "Ruhiy salomatlik", Text: "Ta'lim muhitida qulay psixologik xavfsizlikni ta'minlash"},
				{Title: "Hamkorlik uchligi", Text: "O'quvchi, pedagog va ota-onalar o'rtasida uyg'un muloqotni yo'lga qo'yish"},
			}},
			{Layout: LayoutCards, Eyebrow: "Qonunchilik va nizomlar", Title: "Faoliyatning me'yoriy-huquqiy asoslari", Intro: "Pedagog-psixolog ish faoliyati davlat qonunlari va vazirlikning ixtisoslashtirilgan nizomlari asosida tartibga solinadi.", Items: []Item{
				{Title: "«Ta'lim to'g'risida»gi Qonun", Text: "Ta'lim oluvchilarning huquqlari, erkinliklari va ruhiy-jismoniy rivojlanishining davlat kafolatlari."},
				{Title: "Bolalar huquqlari kafolatlari", Text: "Bolalarni zo'ravonlikdan himoya qilish kafolatlari hamda ta'limning davlat talablari."},
				{Title: "Psixologik xizmat nizomi", Text: "Vazirlikning sohaviy nizomlari, lavozim yo'riqnomalari va etika qoidalari."},
			}},
			{Layout: LayoutGrid, Title: "Faoliyatning oltita asosiy turi", Items: []Item{
				{Icon: "🩺", Title: "Psixodiagnostika"}, {Icon: "🛡️", Title: "Psixoprofilaktika"}, {Icon: "🧩", Title: "Psixokorreksiya va rivojlantirish"},
				{Icon: "💬", Title: "Psixologik maslahat"}, {Icon: "🎓", Title: "Psixologik ma'rifat"}, {Icon: "🗂️", Title: "Tashkiliy-metodik ish"},
			}},
			{Layout: LayoutSteps, Eyebrow: "Yo'nalish 03 · Tiklash va o'stirish", Title: "Psixokorreksiya va rivojlantiruvchi jarayonlar", Intro: "Normadan chetlashgan xususiyatlarni to'g'rilash va ichki imkoniyatlarni kengaytirish ketma-ketligi:", Items: []Item{
				{Title: "Maqsadli dastur", Text: "Tashxis xulosalari asosida aniq korreksion reja va mashg'ulot modullarini ishlab chiqish."},
				{Title: "Individual ish", Text: "Muayyan qiyinchilikka ega o'quvchi bilan yolg'iz, maxsus sessiyalar orqali ruhiy to'siqlarni yechish."},
				{Title: "Guruhiy trening", Text: "Tengdoshlar davrasida muloqot, diqqat, hissiy barqarorlik va jamoaviy hamkorlikni rivojlantirish."},
				{Title: "Natijani baholash", Text: "O'tkazilgan mashg'ulotlardan so'ng qayta monitoring qilish va samaradorlikni belgilash."},
			}},
			{Layout: LayoutTable, Eyebrow: "Hujjatlar va tashkiliy ishlar", Title: "Ish rejasi va hujjatlarni yuritish", Columns: [2]string{"Hujjat toifasi", "Hujjatlar va vazifasi"}, Note: "Eslatma: barcha xulosa va individual xaritalar maxfiy saqlanadi.", Items: []Item{
				{Title: "Rejalashtirish", Text: "Yillik istiqbolli reja; choraklik va oylik ish rejalari; xodimlar bilan hamkorlik jadvali."},
				{Title: "Qayd etish", Text: "Ish jurnali: kundalik faoliyat jurnali va konsultatsiyalar daftari."},
				{Title: "Maxsus kartoteka", Text: "O'quvchilarning individual psixologik xaritalari va xulosalari."},
				{Title: "Hisobotlar", Text: "Choraklik va yillik tahliliy hisobotlar; samaradorlikni baholash."},
			}},
			{Layout: LayoutStatement, Eyebrow: "Xulosa", Title: "Asosiy g'oya", Intro: "Aniq tashxis — samarali psixologik yordamning boshlang'ich nuqtasidir.", Items: []Item{{Text: "Amaliy psixologiya asoslari"}}},
			{Layout: LayoutPoints, Title: "Zo'ravonlik & bulling <profilaktika>", Items: []Item{{Title: "Sinflarda", Text: "nizolarni kamaytirish"}, {Text: "Soddalashtirilgan punkt"}}},
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
		for _, name := range []string{"ppt/slides/slide1.xml", "ppt/slides/slide8.xml", "ppt/slides/_rels/slide8.xml.rels"} {
			if _, ok := parts[name]; !ok {
				t.Errorf("%s: no %s", theme, name)
			}
		}
		if _, ok := parts["ppt/slides/slide9.xml"]; ok {
			t.Errorf("%s: a slide too many", theme)
		}
		if !strings.Contains(parts["[Content_Types].xml"], "/ppt/slides/slide8.xml") || strings.Count(parts["ppt/presentation.xml"], "<p:sldId ") != 8 {
			t.Errorf("%s: the slides are not all listed", theme)
		}
		if !strings.Contains(parts["ppt/slides/slide8.xml"], "Zo&apos;ravonlik &amp; bulling &lt;profilaktika&gt;") && !strings.Contains(parts["ppt/slides/slide8.xml"], "Zo'ravonlik &amp; bulling &lt;profilaktika&gt;") {
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

func TestTextShrinksToFit(t *testing.T) {
	short, long := []string{"bir", "ikki"}, []string{strings.Repeat("so'z ", 40), strings.Repeat("gap ", 50)}
	if a, b := fitBlock(short, 4*inch, 3*inch, 22, 12), fitBlock(long, 4*inch, 3*inch, 22, 12); a <= b {
		t.Errorf("sizes %d and %d", a, b)
	}
	if fit("Qisqa sarlavha", 10*inch, 1, 36, 20) != 36 || fit(strings.Repeat("Juda uzun sarlavha ", 8), 10*inch, 2, 36, 20) >= 36 {
		t.Error("titles do not shrink")
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
