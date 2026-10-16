package webapi

//go:generate go run ./internal/genplaces -places ${GEONAMES}/places/UZ.txt -alternates ${GEONAMES}/alternates/UZ.txt -out places_uz_gen.go

import (
	"cmp"
	"math"
	"strings"

	"telegram-ai-bot/internal/webapi/internal/placename"
)

// place is where a name points: a town, district or region in Uzbekistan,
// or a city abroad.
type place struct {
	// name is the Latin Uzbek name shown to the model.
	name string
	// timezone is the IANA zone of a city abroad. It is empty in
	// Uzbekistan, which keeps Tashkent time everywhere.
	timezone string
	lat, lon float64
}

// uzbekPlace is a row of the generated table in places_uz_gen.go.
type uzbekPlace struct {
	name       string
	lat, lon   float64
	population int
	// feature is the GeoNames feature code: PPLC, PPLA, ADM1, PPLA2, ADM2,
	// PPLA3 or PPL.
	feature string
	// aliases are the other spellings, separated by "|".
	aliases string
}

// featureRanks order Uzbek places that share a name: the capital, a region
// centre, a region, a district seat, a district, a smaller seat, a town.
var featureRanks = map[string]int{"PPLC": 0, "PPLA": 1, "ADM1": 2, "PPLA2": 3, "ADM2": 4, "PPLA3": 5, "PPL": 6}

// nameIndex finds a table's row by a name as written (placename.Strict:
// apostrophes count) or folded (placename.Key: they do not).
type nameIndex struct{ strict, loose map[string]int }

func newNameIndex() nameIndex {
	return nameIndex{strict: map[string]int{}, loose: map[string]int{}}
}

// add files row under name, unless another row owns the name and
// replaces says to keep it.
func (x nameIndex) add(name string, row int, replaces func(old int) bool) {
	addKey(x.strict, placename.Strict(name), row, replaces)
	addKey(x.loose, placename.Key(name), row, replaces)
}

func addKey(keys map[string]int, key string, row int, replaces func(old int) bool) {
	if old, taken := keys[key]; key != "" && (!taken || replaces(old)) {
		keys[key] = row
	}
}

var (
	uzbekIndex = indexUzbekPlaces()
	worldIndex = indexWorldCities()
)

// indexUzbekPlaces lets the higher-ranked, then the more populous, place
// own a name that several share.
func indexUzbekPlaces() nameIndex {
	index := newNameIndex()
	for row, p := range uzbekPlaces {
		outranks := func(old int) bool {
			other := uzbekPlaces[old]
			return cmp.Or(cmp.Compare(featureRanks[p.feature], featureRanks[other.feature]), cmp.Compare(other.population, p.population)) < 0
		}
		index.add(p.name, row, outranks)
		for _, alias := range strings.Split(p.aliases, "|") {
			index.add(alias, row, outranks)
		}
	}
	return index
}

func indexWorldCities() nameIndex {
	index := newNameIndex()
	for row, city := range worldCities {
		for _, spelling := range append([]string{city.name}, city.aliases...) {
			index.add(spelling, row, func(int) bool { return false })
		}
	}
	return index
}

// lookupPlace finds a place by any spelling: a name as written before one
// that matches only folded, so Bo'ston is the Uzbek town and Boston the
// American city, and at each step Uzbekistan before the rest of the world.
func lookupPlace(name string) (place, bool) {
	strict, loose := placename.Strict(name), placename.Key(name)
	if row, found := uzbekIndex.strict[strict]; found {
		return uzbekPlaces[row].place(), true
	}
	if row, found := worldIndex.strict[strict]; found {
		return worldCities[row].place(), true
	}
	if row, found := uzbekIndex.loose[loose]; found {
		return uzbekPlaces[row].place(), true
	}
	if row, found := worldIndex.loose[loose]; found {
		return worldCities[row].place(), true
	}
	return place{}, false
}

func (p uzbekPlace) place() place { return place{name: p.name, lat: p.lat, lon: p.lon} }

func (c worldCity) place() place {
	return place{name: c.name, timezone: c.timezone, lat: c.lat, lon: c.lon}
}

// region is a region centre that prayer_times knows. The coordinates are
// the ones the Muslim Board's times were checked against.
type region struct {
	key, name string
	lat, lon  float64
}

var regions = []region{
	{key: "toshkent", name: "Toshkent", lat: 41.2995, lon: 69.2401},
	{key: "nukus", name: "Nukus", lat: 42.4586, lon: 59.6058},
	{key: "andijon", name: "Andijon", lat: 40.7834, lon: 72.3507},
	{key: "buxoro", name: "Buxoro", lat: 39.7703, lon: 64.4307},
	{key: "jizzax", name: "Jizzax", lat: 40.1335, lon: 67.8296},
	{key: "qarshi", name: "Qarshi", lat: 38.8606, lon: 65.7891},
	{key: "navoiy", name: "Navoiy", lat: 40.0844, lon: 65.3792},
	{key: "namangan", name: "Namangan", lat: 40.9983, lon: 71.6726},
	{key: "samarqand", name: "Samarqand", lat: 39.6546, lon: 66.9644},
	{key: "guliston", name: "Guliston", lat: 40.4954, lon: 68.7754},
	{key: "termiz", name: "Termiz", lat: 37.2242, lon: 67.2783},
	{key: "fargona", name: "Farg'ona", lat: 40.3842, lon: 71.7843},
	{key: "urganch", name: "Urganch", lat: 41.5518, lon: 60.6314},
	{key: "nurafshon", name: "Nurafshon", lat: 41.0321, lon: 69.3625},
}

// regionReach is how far, in degrees, the place a name finds may lie from
// a region centre and still be it: Tashkent's GeoNames point is 0.04 from
// the one above, Chirchiq 0.18.
const regionReach = 0.1

// regionPlace finds the region centre a name means: the centre by any
// spelling, or its region ("Qashqadaryo" is Qarshi).
func regionPlace(name string) (region, bool) {
	p, found := lookupPlace(name)
	if !found || p.timezone != "" {
		return region{}, false
	}
	for _, r := range regions {
		if math.Hypot(p.lat-r.lat, p.lon-r.lon) < regionReach {
			return r, true
		}
	}
	return region{}, false
}
