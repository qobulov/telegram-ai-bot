package main

import (
	"slices"
	"testing"
)

func TestSameNameKeepsSpellingsAndDropsOtherNames(t *testing.T) {
	tests := []struct {
		alias   string
		current []string
		same    bool
	}{
		// The place's own name, written the Russian or English way.
		{"Kokand", []string{"Qo'qon"}, true},
		{"Dzhizak", []string{"Jizzax"}, true},
		{"Kungrad", []string{"Qo'ng'irot"}, true},
		{"Denau", []string{"Denov"}, true},
		{"Bagdad", []string{"Bag'dod"}, true},
		{"Риштан", []string{"Rishton"}, true},
		{"Gorod Kungrad", []string{"Qo'ng'irot Shahri"}, true},
		{"Kurgantepa", []string{"Qo'rg'ontepa"}, true},
		// Soviet names that are other cities today, and names of other
		// places.
		{"Kuybyshev", []string{"Rishton"}, false},
		{"Ul’yanovsk", []string{"Mug‘lon Shahar", "Mug'lon"}, false},
		{"Sverdlovsk", []string{"Jondor"}, false},
		{"Stalino", []string{"Shahrixon"}, false},
		{"Zheleznodorozhnyy", []string{"Qo'ng'irot"}, false},
		{"Kyzyl-Tepa", []string{"Qo'rg'ontepa"}, false},
		{"Kurgan Tyube", []string{"Qo'rg'ontepa"}, false},
		{"Naryn", []string{"Xaqqulobod", "Haqqulobod"}, false},
		{"Almazar", []string{"Zafar"}, false},
		{"Shakhimardan", []string{"G'ozg'on"}, false},
		{"Шелек", []string{"Chelak"}, false},
		{"Naannoo Andijan", []string{"Andijon"}, false},
	}
	for _, test := range tests {
		if got := sameName(test.alias, test.current); got != test.same {
			t.Errorf("sameName(%q, %q) = %v, want %v", test.alias, test.current, got, test.same)
		}
	}
}

// A Russian adjective gives the noun that ends as the Uzbek name does.
func TestNamesReadRussianAdjectives(t *testing.T) {
	tests := []struct {
		name, alternate string
		want, not       string
	}{
		{"Forish Tumani", "Farishskiy Rayon", "Farish", "Farisha"},
		{"Zangiota Tumani", "Zangiatinskiy Rayon", "Zangiata", "Zangiatin"},
		{"Kegeyli Tumani", "Kegeylinskiy Rayon", "Kegeyli", "Kegeylin"},
		{"Amudaryo Tumani", "Amudar’inskiy Rayon", "Amudar'ya", "Amudar'i"},
		{"Yunusobod Tumani", "Kirovskiy Rayon", "", "Kirov"},
	}
	for _, test := range tests {
		f := &feature{name: test.name, ascii: test.name, alternates: []alternate{{name: test.alternate}}}
		f.current = currentNames(f)
		got := names(f)
		if test.want != "" && !slices.Contains(got, test.want) {
			t.Errorf("names(%s) = %q, want %q among them", test.name, got, test.want)
		}
		if slices.Contains(got, test.not) {
			t.Errorf("names(%s) = %q, want no %q", test.name, got, test.not)
		}
	}
}
