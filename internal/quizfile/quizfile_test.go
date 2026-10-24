package quizfile

import (
	"os"
	"strings"
	"testing"

	"telegram-ai-bot/internal/doctext"
)

const sample = `O'ZBEKISTON RESPUBLIKASI
TOSHKENT DAVLAT IQTISODIYOT UNIVERSITETI
YAKUNIY NAZORAT TEST SAVOLLARI. Mazkur yakuniy nazorat savollari kafedraning yig'ilishida muhokamadan o'tkazilgan va tasdiqlangan, kamchilik chiqqan holatda KPI ustama haqdan 10% ushlab qolishingizga roziman. Kafedra ekspertlar guruhi: Rasulev A. ______ Mamatov A. ______ Abdullayeva M. ______
Ishlab chiqarishning qanday turlari mavjud?
====
#moddiy va nomoddiy
====
barter va Tovar
====
tovarlar va daromadlar
====
oraliq va yakuniy

++++
Quyidagilardan qaysi biri
iqtisodiy hodisa hisoblanadi?
=====
#inqiroz.
====	
taqsimlash.
====
madaniy–maishiy ehtiyoj.

Kishilar hayot kechirishining birlamchi sharti nima deba nomlanadi?
====
#moddiy ehtiyoj.
====
bevosita ehtiyoj.
+++++
Ikki to'g'ri javobli savol?
====
#a
====
#b
++++
Javobsiz savol?
====
a
====
b
++++
Ishlab chiqarishning qanday turlari mavjud?
====
#moddiy va nomoddiy
====
barter va Tovar`

func TestParseReadsTheHEMISFormat(t *testing.T) {
	questions, broken, _ := Parse(sample)

	if len(questions) != 3 || broken != 2 {
		t.Fatalf("questions = %+v, broken = %d; want 3 read, 2 broken", questions, broken)
	}
	first := questions[0]
	if first.Text != "Ishlab chiqarishning qanday turlari mavjud?" || len(first.Options) != 4 || first.Options[first.Correct] != "moddiy va nomoddiy" || first.Options[1] != "barter va Tovar" {
		t.Errorf("first = %+v; the title page must not be in it", first)
	}
	second := questions[1]
	if second.Text != "Quyidagilardan qaysi biri iqtisodiy hodisa hisoblanadi?" || strings.Join(second.Options, "|") != "inqiroz.|taqsimlash.|madaniy–maishiy ehtiyoj." || second.Correct != 0 {
		t.Errorf("second = %+v", second)
	}
	third := questions[2]
	if third.Text != "Kishilar hayot kechirishining birlamchi sharti nima deba nomlanadi?" || third.Options[third.Correct] != "moddiy ehtiyoj." {
		t.Errorf("third = %+v; a question after a left-out ++++ must be found", third)
	}
}

// Text from a PDF keeps the markers but loses its line breaks.
func TestParseReadsMarkersWithinLines(t *testing.T) {
	questions, broken, _ := Parse("Savol bir?====#ha====yo'q++++Savol ikki?==== tog' ====#daryo ")
	if len(questions) != 2 || broken != 0 || questions[1].Options[questions[1].Correct] != "daryo" || questions[0].Text != "Savol bir?" {
		t.Errorf("questions = %+v, broken %d", questions, broken)
	}
}

func TestParseFindsNothingInOrdinaryText(t *testing.T) {
	if questions, broken, _ := Parse("Bu oddiy matn, unda test yo'q.\n\nIkkinchi xatboshi."); len(questions) != 0 || broken != 0 {
		t.Errorf("questions = %+v, broken %d", questions, broken)
	}
}

// QUIZFILE_SAMPLE, when set, is a real test file to read, to check by hand.
func TestSampleFile(t *testing.T) {
	path := os.Getenv("QUIZFILE_SAMPLE")
	if path == "" {
		t.Skip("no QUIZFILE_SAMPLE")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text, err := doctext.Extract(doctext.Kind(path, ""), data)
	if err != nil {
		t.Fatal(err)
	}
	questions, broken, made := Parse(text)
	t.Logf("%s: %d questions, %d broken, options made: %v", path, len(questions), broken, made)
	for i := 0; i < len(questions); i += max(1, len(questions)/12) {
		t.Logf("#%d %q -> %q of %q", i+1, questions[i].Text, questions[i].Options[questions[i].Correct], questions[i].Options)
	}
}

// answersOnly is a test that gives only the right answers, marked with "#"
// or written on the line after the question, as some banks are.
const answersOnly = `TOSHKENT DAVLAT IQTISODIYOT UNIVERSITETI
YAKUNIY TEST SAVOLLARI
+++++
Bir butunni bo'laklarga bo'lib o'rganish –
# Analiz
=====
Parchalab o'rganilgan jarayonlarni umumlashtirish –
# Sintez
===
Umumiylikdan xususiylik sari o'rganish – # Deduksiya
=====
Xususiylikdan umumiylik sari o'rganish –
# induksiya
=====
Ikki javobli savol? # bir # ikki
=====
Material sarfi rejada 20 kg, haqiqatda 25 kg, bahosi 22000 so'm. Rejadagi
xarajat necha so'm?
440 000 so'm
=====
Material sarfi rejada 20 kg, haqiqatda 25 kg, bahosi 22000 so'm. Haqiqiy
xarajat necha so'm?
550 000 so'm
=====
Material sarfining farqini toping.
5 kg
=====
Material xarajati farqi necha so'm?
110 000 so'm
=====
Ushbu koeffitsiyent ta'sirini qaysi usul bilan aniqlash mumkin?
Zanjirli bog'lanish usuli
=====
Ushbu koeffitsiyent ta'sirini qaysi usul bilan aniqlash mumkin?
Mutloq farqlar usuli`

func TestParseMakesOptionsForATestOfAnswers(t *testing.T) {
	questions, broken, made := Parse(answersOnly)
	if !made || len(questions) != 10 || broken != 1 {
		t.Fatalf("made %v, %d questions, broken %d: %+v", made, len(questions), broken, questions)
	}
	byText := map[string]Question{}
	for _, q := range questions {
		if len(q.Options) != 4 || q.Correct != 0 {
			t.Errorf("%q: options %q, correct %d", q.Text, q.Options, q.Correct)
		}
		byText[q.Text] = q
	}
	deduction := byText["Umumiylikdan xususiylik sari o'rganish –"]
	if deduction.Options[0] != "Deduksiya" || strings.Contains(strings.Join(deduction.Options, "|"), "so'm") {
		t.Errorf("a term's options are terms: %q", deduction.Options)
	}
	planned := byText["Material sarfi rejada 20 kg, haqiqatda 25 kg, bahosi 22000 so'm. Rejadagi xarajat necha so'm?"]
	if planned.Options[0] != "440 000 so'm" || strings.Contains(strings.Join(planned.Options, "|"), "Analiz") {
		t.Errorf("a figure's options are figures: %q", planned.Options)
	}
	// The same question asked again may have both answers right, so
	// neither is an option of the other.
	chain := byText["Ushbu koeffitsiyent ta'sirini qaysi usul bilan aniqlash mumkin?"]
	if strings.Contains(strings.Join(chain.Options[1:], "|"), "usuli") {
		t.Errorf("another right answer among the wrong ones: %q", chain.Options)
	}
}

// A test with its options is read as it is, not as a test of answers.
func TestParseKeepsTheOptionsOfAHEMISTest(t *testing.T) {
	if _, _, made := Parse(sample); made {
		t.Error("the HEMIS sample was read as a test of answers")
	}
}
