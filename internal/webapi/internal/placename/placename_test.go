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
		{name: "Khiwa Shahri", want: "xiva", alike: []string{"Xiva", "Хива", "Khiva"}},
		{name: "Ёшлик", want: "yoshlik", alike: []string{"Yoshlik"}},
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
