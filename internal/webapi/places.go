package webapi

//go:generate go run ./internal/genplaces -places ${GEONAMES}/places/UZ.txt -alternates ${GEONAMES}/alternates/UZ.txt -out places_uz_gen.go

import (
	"cmp"
	"math"
	"slices"
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
	// province is GeoNames' code of the region: 13 is the city of
	// Tashkent, 14 the region around it.
	province int
	kinds    placeKind
	// aliases are the other spellings, separated by "|".
	aliases string
}

// placeKind says what a row is to a member who writes "tumani" or
// "viloyati": a town that is a district seat named like its district is
// both.
type placeKind int

const (
	townKind placeKind = 1 << iota
	districtKind
	regionKind
)

const (
	tashkentCity   = 13
	tashkentRegion = 14
)

// featureRanks order Uzbek places that share a name: the capital, a region
// centre, a district seat, a region, a district, a smaller seat, a town.
// A region ranks after a seat of its name: Sirdaryo is the town, Sirdaryo
// viloyati the region.
var featureRanks = map[string]int{"PPLC": 0, "PPLA": 1, "PPLA2": 2, "ADM1": 3, "ADM2": 4, "PPLA3": 5, "PPL": 6}

// rank is a row's place in featureRanks. Tashkent's districts are parts of
// the capital and rank with it: Uchtepa is the district of Tashkent, not
// the village in Jizzax.
func (p uzbekPlace) rank() int {
	if p.feature == "ADM2" && p.province == tashkentCity {
		return featureRanks["PPLC"]
	}
	return featureRanks[p.feature]
}

// nameIndex finds the rows of a table by a name as written
// (placename.Strict: apostrophes count) or folded (placename.Key: they do
// not).
type nameIndex struct{ strict, loose map[string][]int }

func newNameIndex() nameIndex {
	return nameIndex{strict: map[string][]int{}, loose: map[string][]int{}}
}

func (x nameIndex) add(name string, row int) {
	addKey(x.strict, placename.Strict(name), row)
	addKey(x.loose, placename.Key(name), row)
}

func addKey(keys map[string][]int, key string, row int) {
	if key != "" && !slices.Contains(keys[key], row) {
		keys[key] = append(keys[key], row)
	}
}

var (
	uzbekIndex = indexUzbekPlaces()
	worldIndex = indexWorldCities()
)

func indexUzbekPlaces() nameIndex {
	index := newNameIndex()
	for row, p := range uzbekPlaces {
		index.add(p.name, row)
		for _, alias := range strings.Split(p.aliases, "|") {
			index.add(alias, row)
		}
	}
	return index
}

func indexWorldCities() nameIndex {
	index := newNameIndex()
	for row, city := range worldCities {
		for _, spelling := range append([]string{city.name}, city.aliases...) {
			index.add(spelling, row)
		}
	}
	return index
}

// lookupPlace finds a place by any spelling: a name as written before one
// that matches only folded, so Bo'ston is the Uzbek town and Boston the
// American city, and at each step Uzbekistan before the rest of the world.
// An Uzbek place word puts the folded Uzbek names first ("Boston tumani"
// is Bo'ston) and picks a district, region or town among Uzbek places of
// one name. A Russian adjective ("Китабский район") is read as its noun.
func lookupPlace(name string) (place, bool) {
	kind, uzbekWord := placename.KindOf(name)
	for _, spelling := range append([]string{name}, placename.Nouns(name)...) {
		strict, loose := placename.Strict(spelling), placename.Key(spelling)
		uzbekStrict, inUzbekStrict := bestUzbekPlace(uzbekIndex.strict[strict], kind, loose)
		uzbekLoose, inUzbekLoose := bestUzbekPlace(uzbekIndex.loose[loose], kind, loose)
		worldStrict, worldLoose := worldIndex.strict[strict], worldIndex.loose[loose]
		switch {
		case inUzbekStrict:
			return uzbekPlaces[uzbekStrict].place(), true
		case inUzbekLoose && uzbekWord:
			return uzbekPlaces[uzbekLoose].place(), true
		case len(worldStrict) > 0:
			return worldCities[worldStrict[0]].place(), true
		case inUzbekLoose:
			return uzbekPlaces[uzbekLoose].place(), true
		case len(worldLoose) > 0:
			return worldCities[worldLoose[0]].place(), true
		}
	}
	return place{}, false
}

// uzbekCandidates are the rows a name can mean, as written or folded.
func uzbekCandidates(name string) []int {
	var rows []int
	for _, spelling := range append([]string{name}, placename.Nouns(name)...) {
		for _, row := range slices.Concat(uzbekIndex.strict[placename.Strict(spelling)], uzbekIndex.loose[placename.Key(spelling)]) {
			if !slices.Contains(rows, row) {
				rows = append(rows, row)
			}
		}
	}
	return rows
}

// bestUzbekPlace picks among rows sharing a name, key folded: those of
// the kind asked for, if any; then by rank; then a row named so before
// one that has the name as an alias (Bo'ston, Karakalpakstan, before the
// district of Andijan that took the name); then the more populous.
func bestUzbekPlace(rows []int, kind placename.Kind, key string) (int, bool) {
	if len(rows) == 0 {
		return 0, false
	}
	if wanted := kindOf(kind); wanted != 0 {
		var ofKind []int
		for _, row := range rows {
			if uzbekPlaces[row].kinds&wanted != 0 {
				ofKind = append(ofKind, row)
			}
		}
		if len(ofKind) > 0 {
			rows = ofKind
		}
	}
	return slices.MinFunc(rows, func(x, y int) int {
		px, py := uzbekPlaces[x], uzbekPlaces[y]
		return cmp.Or(
			cmp.Compare(px.rank(), py.rank()),
			trueFirst(placename.Key(px.name) == key, placename.Key(py.name) == key),
			cmp.Compare(py.population, px.population),
			cmp.Compare(x, y),
		)
	}), true
}

func kindOf(kind placename.Kind) placeKind {
	switch kind {
	case placename.Town:
		return townKind
	case placename.District:
		return districtKind
	case placename.Region:
		return regionKind
	default:
		return 0
	}
}

func (p uzbekPlace) place() place { return place{name: p.name, lat: p.lat, lon: p.lon} }

func (c worldCity) place() place {
	return place{name: c.name, timezone: c.timezone, lat: c.lat, lon: c.lon}
}

// sameProvince reports whether two rows are in one region, Tashkent and
// the region around it counting as one: members say "Toshkent" for both.
func sameProvince(x, y uzbekPlace) bool {
	fold := func(province int) int {
		if province == tashkentRegion {
			return tashkentCity
		}
		return province
	}
	return fold(x.province) == fold(y.province)
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
