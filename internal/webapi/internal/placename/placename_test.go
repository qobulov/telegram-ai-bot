package placename

import "testing"

func TestKeyFoldsTheWaysANameIsWritten(t *testing.T) {
	tests := []struct {
		name  string
		want  string
		alike []string
	}{
		{name: "Farg'ona", want: "fargona", alike: []string{"Fargʻona", "Farg‘ona", "Farg’ona", "Farg`ona", "Fargʼona", "FARGONA", "Farghona", "Фарғона"}},
		{name: "Qo'ng'irot", want: "kongirot", alike: []string{"Qo‘ng‘irot Shahri", "Қўнғирот", "Kongirot"}},
		{name: "Toshkent", want: "toshkent", alike: []string{"  toshkent ", "Toshkent shahri", "Тошкент", "Toshkent viloyati", "г. Тошкент"}},
		{name: "Tashkent", want: "tashkent", alike: []string{"Ташкент", "Tashkent City", "Tashkent Region"}},
		{name: "Bo'stonliq", want: "bostonlik", alike: []string{"Бўстонлиқ тумани", "Bo‘stonliq District", "Bostonliq rayoni"}},
		// ŭ is how GeoNames romanizes ў; it folds to u, as in its ASCII names.
		{name: "Bŭstonliq Tumani", want: "bustonlik", alike: []string{"Bustonliq"}},
		{name: "Akhangaran", want: "axangaran", alike: []string{"Ахангаран"}},
		{name: "Besharyk", want: "besharyk", alike: []string{"Бешарык"}},
		{name: "Sankt-Peterburg", want: "sanktpeterburg", alike: []string{"Санкт-Петербург", "Sankt Peterburg"}},
		{name: "Nyu-York", want: "nyuyork", alike: []string{"Нью-Йорк", "Nyu York"}},
		{name: "Jidda", want: "jidda", alike: []string{"Джидда", "Djidda", "Dzhidda"}},
		{name: "İstanbul", want: "istanbul", alike: []string{"Istanbul", "ISTANBUL"}},
		{name: "Khiwa Shahri", want: "xiva", alike: []string{"Xiva", "Хива", "Khiva", "Hiva"}},
		{name: "Ёшлик", want: "yoshlik", alike: []string{"Yoshlik"}},
		// h and x are one letter to members, and Russian writes both х.
		{name: "Hazorasp", want: "xazorasp", alike: []string{"Xazorasp", "Khazorasp", "Ҳазорасп", "Хазорасп"}},
		{name: "Sayhunobod", want: "sayxunobod", alike: []string{"Sayxunobod", "Saykhunobod"}},
		{name: "Andijan", want: "andijan", alike: []string{"Andizhan", "Andidzhan", "Андижан"}},
		{name: "Shahrisabz", want: "shaxrisabz", alike: []string{"Shakhrisabz", "Шаҳрисабз", "Шахрисабз"}},
	}
	for _, test := range tests {
		if got := Key(test.name); got != test.want {
			t.Errorf("Key(%q) = %q, want %q", test.name, got, test.want)
		}
		for _, other := range test.alike {
			if got := Key(other); got != Key(test.name) {
				t.Errorf("Key(%q) = %q, want %q like %q", other, got, Key(test.name), test.name)
			}
		}
	}
}

func TestKeyKeepsDifferentNamesApart(t *testing.T) {
	for _, pair := range [][2]string{{"Olot", "Alat"}, {"Mexico", "New Mexico"}, {"York", "New York"}, {"Paris", "Parij"}} {
		if Key(pair[0]) == Key(pair[1]) {
			t.Errorf("Key(%q) = Key(%q) = %q", pair[0], pair[1], Key(pair[0]))
		}
	}
	for _, empty := range []string{"", "'", " - ", "shahri", "г."} {
		if got := Key(empty); got != "" {
			t.Errorf("Key(%q) = %q, want empty", empty, got)
		}
	}
}

func TestStrictKeepsApostrophesAndLetters(t *testing.T) {
	tests := []struct{ a, b string }{
		{"Bo'ston", "Bo‘ston"},
		{"Bo'ston", "Boʻston"},
		{"Bo'ston", "Бўстон"},
		{"Kogon Shahri", "kogon"},
		{"Mexico City", "Mexico"},
		{"Tashkent", "Ташкент"},
	}
	for _, test := range tests {
		if Strict(test.a) != Strict(test.b) {
			t.Errorf("Strict(%q) = %q, Strict(%q) = %q; want equal", test.a, Strict(test.a), test.b, Strict(test.b))
		}
	}
	different := []struct{ a, b string }{
		{"Bo'ston", "Boston"},
		{"So'x", "Sox"},
		{"Qamashi", "Kamashi"},
		{"Danghara", "Dangara"},
	}
	for _, test := range different {
		if Strict(test.a) == Strict(test.b) {
			t.Errorf("Strict(%q) = Strict(%q) = %q", test.a, test.b, Strict(test.a))
		}
		if Key(test.a) != Key(test.b) {
			t.Errorf("Key(%q) = %q, Key(%q) = %q; want equal", test.a, Key(test.a), test.b, Key(test.b))
		}
	}
}

func TestKindOf(t *testing.T) {
	tests := []struct {
		name  string
		kind  Kind
		uzbek bool
	}{
		{"Forish tumani", District, true},
		{"Бўстон тумани", District, true},
		{"Qashqadaryo viloyati", Region, true},
		{"Boston shahri", Town, true},
		{"Toshkent sh.", Town, true},
		{"Келес шахри", Town, true},
		{"Qo'rg'ontepa qishlog'i", Town, true},
		{"г. Москва", Town, false},
		{"Mexico City", Town, false},
		{"Китабский район", District, false},
		{"Ташкентская область", Region, false},
		{"Toshkent", AnyKind, false},
	}
	for _, test := range tests {
		if kind, uzbek := KindOf(test.name); kind != test.kind || uzbek != test.uzbek {
			t.Errorf("KindOf(%q) = %v, %v; want %v, %v", test.name, kind, uzbek, test.kind, test.uzbek)
		}
	}
}

func TestTrimDropsUzbekAndRussianPlaceWords(t *testing.T) {
	for name, want := range map[string]string{
		"Keles shahri":        "Keles",
		"Toshkent sh.":        "Toshkent",
		"Ташкент шахар":       "Ташкент",
		"г. Ош":               "Ош",
		"Bo'ritepa qishlog'i": "Bo'ritepa",
		"Mexico City":         "Mexico City",
		"Kuwait City":         "Kuwait City",
		"Toshkent":            "Toshkent",
		"shahri":              "",
	} {
		if got := Trim(name); got != want {
			t.Errorf("Trim(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestNounsReadRussianAdjectives(t *testing.T) {
	tests := []struct {
		name string
		noun string
	}{
		{"Китабский район", "kitab"},
		{"Бостанлыкский район", "bostanlyk"},
		{"Ферганская область", "fergana"},
		{"Хорезмская область", "xorezm"},
		{"Кашкадарьинская область", "kashkadarya"},
		{"Зангиатинский район", "zangiata"},
		{"Кегейлинский район", "kegeyli"},
		{"Farishskiy Rayon", "farish"},
	}
	for _, test := range tests {
		nouns := Nouns(test.name)
		found := false
		for _, noun := range nouns {
			found = found || noun == test.noun
		}
		if !found {
			t.Errorf("Nouns(%q) = %q, want %q among them", test.name, nouns, test.noun)
		}
	}
	for _, name := range []string{"Toshkent", "Forish tumani", "Китабский", "Toshkent shahri"} {
		if nouns := Nouns(name); nouns != nil {
			t.Errorf("Nouns(%q) = %q, want none", name, nouns)
		}
	}
}

func TestCyrillicSpellsAsRussianDoes(t *testing.T) {
	for latin, want := range map[string]string{
		"Venetsiya":       "венеция",
		"Xyuston":         "хьюстон",
		"Qarag'anda":      "караганда",
		"Edinburg":        "эдинбург",
		"Nijniy Novgorod": "нижний новгород",
		"Zaporojye":       "запорожье",
		"Buenos-Ayres":    "буэнос-айрес",
		"Bo'ritepa":       "буритепа",
		"Shiroz":          "широз",
	} {
		if got := Cyrillic(latin); got != want {
			t.Errorf("Cyrillic(%q) = %q, want %q", latin, got, want)
		}
	}
}

func TestLatinSpellsAsUzbekDoes(t *testing.T) {
	for cyrillic, want := range map[string]string{
		"Дамашқ":    "Damashq",
		"Кўлоб":     "Ko‘lob",
		"Қарағанди": "Qarag‘andi",
		"Джидда":    "Jidda",
		"Тошкент":   "Toshkent",
		"Moskva":    "Moskva",
	} {
		if got := Latin(cyrillic); got != want {
			t.Errorf("Latin(%q) = %q, want %q", cyrillic, got, want)
		}
	}
}

func TestWords(t *testing.T) {
	words := Words("Ansan-si")
	if len(words) != 2 || words[0] != "ansan" || words[1] != "si" {
		t.Errorf("Words(Ansan-si) = %q", words)
	}
	if words := Words("Kogon Shahri"); len(words) != 1 || words[0] != "kogon" {
		t.Errorf("Words(Kogon Shahri) = %q", words)
	}
}
