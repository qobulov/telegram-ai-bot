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
	questions, broken := Parse(sample)

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
	questions, broken := Parse("Savol bir?====#ha====yo'q++++Savol ikki?==== tog' ====#daryo ")
	if len(questions) != 2 || broken != 0 || questions[1].Options[questions[1].Correct] != "daryo" || questions[0].Text != "Savol bir?" {
		t.Errorf("questions = %+v, broken %d", questions, broken)
	}
}

func TestParseFindsNothingInOrdinaryText(t *testing.T) {
	if questions, broken := Parse("Bu oddiy matn, unda test yo'q.\n\nIkkinchi xatboshi."); len(questions) != 0 || broken != 0 {
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
	questions, broken := Parse(text)
	t.Logf("%s: %d questions, %d broken", path, len(questions), broken)
	for _, i := range []int{0, 1, len(questions) - 1} {
		if i >= 0 && i < len(questions) {
			t.Logf("#%d %q -> %q of %d", i+1, questions[i].Text, questions[i].Options[questions[i].Correct], len(questions[i].Options))
		}
	}
}
