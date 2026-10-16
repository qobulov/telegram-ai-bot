package webapi

import (
	"testing"
	"time"
)

func TestLookupPlaceAcceptsEverySpelling(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Toshkent", "Toshkent"},
		{"  toshkent ", "Toshkent"},
		{"Tashkent", "Toshkent"},
		{"Тошкент", "Toshkent"},
		{"Ташкент", "Toshkent"},
		{"Toshkent shahri", "Toshkent"},
		{"Farg'ona", "Farg'ona"},
		{"Fargʻona", "Farg'ona"},
		{"Farg‘ona", "Farg'ona"},
		{"Farg’ona", "Farg'ona"},
		{"Fargona", "Farg'ona"},
		{"Фарғона", "Farg'ona"},
		{"Фергана", "Farg'ona"},
		{"Qo'qon", "Qo'qon"},
		{"Kokand", "Qo'qon"},
		{"Қўқон", "Qo'qon"},
		{"KATTAQO'RG'ON", "Kattaqo'rg'on"},
		{"Самарканд", "Samarqand"},
		{"Bukhara", "Buxoro"},
		{"Xiva", "Xiva"},
		{"Хива", "Xiva"},
		{"Yangiyo‘l", "Yangiyo'l"},
		{"G'ijduvon", "G'ijduvon"},
		{"Moskva", "Moskva"},
		{"г. Москва", "Moskva"},
		{"Sankt Peterburg", "Sankt-Peterburg"},
		{"St. Petersburg", "Sankt-Peterburg"},
		{"Seul", "Seul"},
		{"Nyu York", "Nyu-York"},
		{"Чимкент", "Chimkent"},
		{"Olmaota", "Olmaota"},
		{"Xo'jand", "Xo'jand"},
	}
	for _, test := range tests {
		p, found := lookupPlace(test.input)
		if !found || p.name != test.want {
			t.Errorf("lookupPlace(%q) = %q, %v; want %q", test.input, p.name, found, test.want)
		}
	}
	for _, unknown := range []string{"", "Mexico City", "Toshkentda yomg'ir", "'"} {
		if p, found := lookupPlace(unknown); found {
			t.Errorf("lookupPlace(%q) = %q, want no match", unknown, p.name)
		}
	}
}

func TestPlacesHaveTheirTimeZone(t *testing.T) {
	regions := 0
	keys := map[string]string{}
	for _, p := range places {
		inUzbekistan := p.lat >= 37 && p.lat <= 46 && p.lon >= 55 && p.lon <= 74
		if p.timezone == "" && !inUzbekistan {
			t.Errorf("%s at %v,%v is outside Uzbekistan but has no time zone", p.name, p.lat, p.lon)
		}
		if p.timezone != "" {
			if _, err := time.LoadLocation(p.timezone); err != nil || p.region != "" {
				t.Errorf("%s: time zone %q (%v), region %q", p.name, p.timezone, err, p.region)
			}
		}
		for _, spelling := range append([]string{p.name}, p.aliases...) {
			key := normalizeName(spelling)
			if other, taken := keys[key]; taken && other != p.name {
				t.Errorf("%q names both %s and %s", spelling, other, p.name)
			}
			keys[key] = p.name
		}
		if p.region != "" {
			regions++
			if found, ok := regionPlace(p.region); !ok || found.name != p.name {
				t.Errorf("region %q does not resolve to %s", p.region, p.name)
			}
		}
	}
	if regions != 14 || len(places) < 25 {
		t.Errorf("%d places with %d region centres, want at least 25 and 14", len(places), regions)
	}
}

func TestRegionPlaceIsOnlyForRegionCentres(t *testing.T) {
	if p, found := regionPlace("Tashkent"); !found || p.region != "toshkent" {
		t.Errorf("regionPlace(Tashkent) = %+v, %v", p, found)
	}
	if _, found := regionPlace("Chirchiq"); found {
		t.Error("Chirchiq is not a region centre")
	}
}
