package webapi

import "testing"

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
	}
	for _, test := range tests {
		p, found := lookupPlace(test.input)
		if !found || p.name != test.want {
			t.Errorf("lookupPlace(%q) = %q, %v; want %q", test.input, p.name, found, test.want)
		}
	}
	for _, unknown := range []string{"", "London", "Toshkentda yomg'ir", "'"} {
		if p, found := lookupPlace(unknown); found {
			t.Errorf("lookupPlace(%q) = %q, want no match", unknown, p.name)
		}
	}
}

func TestPlacesAreInUzbekistan(t *testing.T) {
	regions := 0
	for _, p := range places {
		if p.lat < 37 || p.lat > 46 || p.lon < 55 || p.lon > 74 {
			t.Errorf("%s at %v,%v is outside Uzbekistan", p.name, p.lat, p.lon)
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
