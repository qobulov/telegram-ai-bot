// Command genplaces writes places_uz_gen.go, the weather tool's table of
// places in Uzbekistan, from a GeoNames extract (CC BY 4.0,
// https://www.geonames.org). Builds never touch the network: run it by
// hand when the table should change, and commit its output.
//
//	mkdir -p "$GEONAMES/places" "$GEONAMES/alternates"
//	curl -o "$GEONAMES/UZ.zip" https://download.geonames.org/export/dump/UZ.zip
//	curl -o "$GEONAMES/alt.zip" https://download.geonames.org/export/dump/alternatenames/UZ.zip
//	unzip "$GEONAMES/UZ.zip" -d "$GEONAMES/places"
//	unzip "$GEONAMES/alt.zip" -d "$GEONAMES/alternates"
//	GEONAMES="$GEONAMES" go generate ./internal/webapi
//
// The table holds every capital and seat of a region or district (PPLC,
// PPLA, PPLA2, PPLA3), every other town of at least 5,000 people, and
// every region and district (ADM1, ADM2). A region or district named like
// its seat (Paxtakor Tumani and Paxtakor) adds its names to the seat; one
// that is not (Bo'stonliq Tumani and G'azalkent) is an entry of its own at
// its seat when the seat is known, else at its own coordinates.
package main

import (
	"bufio"
	"bytes"
	"cmp"
	"flag"
	"fmt"
	"go/format"
	"log"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"telegram-ai-bot/internal/webapi/internal/placename"
)

const (
	// minPopulation is the smallest town kept that is not a seat.
	minPopulation = 5000
	// maxAlias bounds a name in characters: longer ones are descriptions
	// such as "Karakalpakskaya Avtonomnaya Sovetskaya...".
	maxAlias = 40
	// seatReach is how far, in degrees, a district's namesake may lie from
	// the district's point and still be taken for its seat; nearReach is
	// the same across a region border, which GeoNames sometimes has wrong.
	seatReach = 0.6
	nearReach = 0.25
)

// Fixes for what GeoNames lacks or has wrong, by GeoNames id.
var (
	// seats GeoNames does not link to their region or district.
	seats = map[int]int{
		1484838: 1512524, // Tashkent Region: Nurafshon (GeoNames says Amir Timur).
		1514338: 1513996, // Bo'stonliq District: G'azalkent.
		1514024: 1512355, // Forish District: Yangiqishloq (GeoNames' Bog'don).
	}
	// extraTowns are towns GeoNames counts nobody in.
	extraTowns = map[int]bool{
		1538230: true, // Zarafshon, Navoiy region.
	}
	// displayNames replace the Uzbek name GeoNames has.
	displayNames = map[int]string{
		1215957: "Termiz",
		1538230: "Zarafshon",
		453752:  "Qoraqalpog'iston",
	}
	// extraNames are spellings members use that GeoNames lacks.
	extraNames = map[int][]string{
		1215957: {"Termiz", "Термиз"},
		1538230: {"Zarafshon", "Зарафшон", "Зарафшан"},
	}
)

// skippedLanguages are alternate-name "languages" that are not names:
// links, codes and pronunciations.
var skippedLanguages = map[string]bool{
	"link": true, "wkdt": true, "post": true, "iata": true, "icao": true, "faac": true,
	"tcid": true, "unlc": true, "abbr": true, "phon": true, "piny": true, "fr_1793": true,
}

// placeCodes rank the populated places that can be a seat, best first.
var placeCodes = []string{"PPLC", "PPLA", "PPLA2", "PPLA3", "PPLA4", "PPL"}

// seatCodes are what the seat of a region (a region centre) or of a
// district (a district seat) is at least.
var seatCodes = map[string]string{"ADM1": "PPLA", "ADM2": "PPLA2"}

type feature struct {
	id             int
	name, ascii    string
	lat, lon       float64
	class, code    string
	admin1, admin2 string
	population     int
	alternates     []alternate
	// keys fold every name of the feature; current folds only today's
	// names, so a district is not taken for a town that shares an old
	// Soviet name with it (Kirovskiy Rayon).
	keys, current map[string]bool
}

type alternate struct {
	language, name      string
	preferred, historic bool
}

// entry is one row of the table.
type entry struct {
	name       string
	at         *feature // whose coordinates the row has
	population int
	code       string
	aliases    []string
}

func main() {
	placesPath := flag.String("places", "", "UZ.txt from export/dump/UZ.zip")
	alternatesPath := flag.String("alternates", "", "UZ.txt from export/dump/alternatenames/UZ.zip")
	out := flag.String("out", "places_uz_gen.go", "file to write")
	flag.Parse()
	if *placesPath == "" || *alternatesPath == "" {
		log.Fatal("genplaces needs -places and -alternates; the package comment says where to download them")
	}
	features, err := readPlaces(*placesPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := readAlternates(*alternatesPath, features); err != nil {
		log.Fatal(err)
	}
	source, err := render(table(features))
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, source, 0o644); err != nil {
		log.Fatal(err)
	}
}

// readPlaces reads GeoNames' geoname table: id, name, asciiname,
// alternatenames, latitude, longitude, feature class, feature code,
// country, cc2, admin1, admin2, admin3, admin4, population, ...
func readPlaces(path string) ([]*feature, error) {
	var features []*feature
	err := eachRow(path, 19, func(cols []string) error {
		id, err := strconv.Atoi(cols[0])
		if err != nil {
			return err
		}
		lat, err := strconv.ParseFloat(cols[4], 64)
		if err != nil {
			return err
		}
		lon, err := strconv.ParseFloat(cols[5], 64)
		if err != nil {
			return err
		}
		population, _ := strconv.Atoi(cols[14])
		features = append(features, &feature{
			id: id, name: cols[1], ascii: cols[2], lat: lat, lon: lon,
			class: cols[6], code: cols[7], admin1: cols[10], admin2: cols[11], population: population,
		})
		return nil
	})
	return features, err
}

// readAlternates reads alternateNamesV2: id, geonameid, isolanguage, name,
// isPreferredName, isShortName, isColloquial, isHistoric, from, to.
func readAlternates(path string, features []*feature) error {
	byID := make(map[int]*feature, len(features))
	for _, f := range features {
		byID[f.id] = f
	}
	return eachRow(path, 8, func(cols []string) error {
		id, err := strconv.Atoi(cols[1])
		if err != nil {
			return err
		}
		if f := byID[id]; f != nil {
			f.alternates = append(f.alternates, alternate{language: cols[2], name: cols[3], preferred: cols[4] == "1", historic: cols[7] == "1"})
		}
		return nil
	})
}

func eachRow(path string, columns int, row func([]string) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for line := 1; scanner.Scan(); line++ {
		cols := strings.Split(scanner.Text(), "\t")
		if len(cols) < columns {
			return fmt.Errorf("%s:%d: %d columns, want at least %d", path, line, len(cols), columns)
		}
		if err := row(cols); err != nil {
			return fmt.Errorf("%s:%d: %w", path, line, err)
		}
	}
	return scanner.Err()
}

// table builds the rows: towns, then regions and districts merged into
// their seats or added on their own.
func table(features []*feature) []*entry {
	byID := map[int]*feature{}
	for _, f := range features {
		byID[f.id] = f
		f.keys = keysOf(names(f))
		f.current = keysOf(currentNames(f))
	}
	checkFixes(byID)
	var rows []*entry
	towns := map[int]*entry{}
	addTown := func(f *feature) *entry {
		if towns[f.id] == nil {
			towns[f.id] = &entry{name: townName(f), at: f, population: f.population, code: f.code, aliases: names(f)}
			rows = append(rows, towns[f.id])
		}
		return towns[f.id]
	}
	for _, f := range features {
		if isTown(f) {
			addTown(f)
		}
	}
	for _, f := range features {
		if f.code != "ADM1" && f.code != "ADM2" {
			continue
		}
		seat := findSeat(f, byID, features)
		if seat == nil {
			rows = append(rows, &entry{name: adminName(f), at: f, population: f.population, code: f.code, aliases: names(f)})
			continue
		}
		town := addTown(seat)
		town.code = better(town.code, seatCodes[f.code])
		if namedLike(f, seat) {
			town.aliases = append(town.aliases, names(f)...)
		} else {
			rows = append(rows, &entry{name: adminName(f), at: seat, population: f.population, code: f.code, aliases: names(f)})
		}
	}
	for _, row := range rows {
		row.aliases = distinct(row.name, row.aliases)
	}
	slices.SortStableFunc(rows, func(x, y *entry) int {
		return cmp.Or(strings.Compare(x.name, y.name), cmp.Compare(x.at.lat, y.at.lat), cmp.Compare(x.at.lon, y.at.lon), strings.Compare(x.code, y.code))
	})
	return rows
}

// checkFixes stops when a fix above names a place GeoNames no longer has,
// rather than drop the fix unnoticed.
func checkFixes(byID map[int]*feature) {
	var ids []int
	for admin, seat := range seats {
		ids = append(ids, admin, seat)
	}
	for id := range extraTowns {
		ids = append(ids, id)
	}
	for id := range displayNames {
		ids = append(ids, id)
	}
	for id := range extraNames {
		ids = append(ids, id)
	}
	for _, id := range ids {
		if byID[id] == nil {
			log.Fatalf("GeoNames has no place %d, which a fix in genplaces names", id)
		}
	}
}

func isTown(f *feature) bool {
	switch {
	case f.class != "P":
		return false
	case f.code == "PPLC" || f.code == "PPLA" || f.code == "PPLA2" || f.code == "PPLA3":
		return true
	default:
		return (f.code == "PPL" && f.population >= minPopulation) || extraTowns[f.id]
	}
}

// findSeat is where a region's or district's weather is taken: a seat
// fixed above, the region centre or linked district seat GeoNames has, or
// else a town of the same name nearby.
func findSeat(admin *feature, byID map[int]*feature, features []*feature) *feature {
	if id, found := seats[admin.id]; found {
		return byID[id]
	}
	for _, f := range features {
		linked := (admin.code == "ADM1" && (f.code == "PPLA" || f.code == "PPLC")) ||
			(admin.code == "ADM2" && admin.admin2 != "" && f.code == "PPLA2" && f.admin2 == admin.admin2)
		if linked && f.class == "P" && f.admin1 == admin.admin1 {
			return f
		}
	}
	return namesake(admin, features)
}

// namesake is the populated place named like admin that is likeliest its
// seat: a seat before a village, then the larger, then the nearer.
func namesake(admin *feature, features []*feature) *feature {
	var best *feature
	var bestDistance float64
	for _, f := range features {
		distance := math.Hypot(f.lat-admin.lat, f.lon-admin.lon)
		near := distance <= nearReach || (f.admin1 == admin.admin1 && distance <= seatReach)
		if f.class != "P" || !slices.Contains(placeCodes, f.code) || !near || !namedLike(admin, f) {
			continue
		}
		if best == nil || cmp.Or(
			cmp.Compare(slices.Index(placeCodes, f.code), slices.Index(placeCodes, best.code)),
			cmp.Compare(best.population, f.population),
			cmp.Compare(distance, bestDistance),
		) < 0 {
			best, bestDistance = f, distance
		}
	}
	return best
}

// namedLike reports whether a region's or district's name today is one of
// the town's names.
func namedLike(admin, town *feature) bool {
	for key := range admin.current {
		if town.keys[key] {
			return true
		}
	}
	return false
}

// better is the higher of two feature codes of a seat.
func better(code, floor string) string {
	if slices.Index(placeCodes, floor) < slices.Index(placeCodes, code) {
		return floor
	}
	return code
}

// names are a feature's spellings: GeoNames' name and ASCII name, the
// alternate names in Latin or Cyrillic letters, the fixes above, and
// GeoNames' ŭ (Uzbek ў) also written o', as Uzbek Latin has it. A region
// or district keeps only its Uzbek and English alternate names: the rest
// are Russian adjectives ("Alatskiy Rayon") and Soviet names ("Moskva
// Rayoni", now Shahrixon), which would shadow Moscow.
func names(f *feature) []string {
	all := []string{f.name, f.ascii}
	for _, alt := range f.alternates {
		if usable(alt) && (f.class != "A" || alt.language == "uz" || alt.language == "en") {
			all = append(all, alt.name)
		}
	}
	all = append(all, extraNames[f.id]...)
	for _, name := range all {
		if strings.ContainsAny(name, "ŭŬ") {
			all = append(all, oWithApostrophe.Replace(name))
		}
	}
	return all
}

var oWithApostrophe = strings.NewReplacer("ŭ", "o'", "Ŭ", "O'")

// currentNames are GeoNames' name and ASCII name and the Uzbek names.
func currentNames(f *feature) []string {
	current := []string{f.name, f.ascii, oWithApostrophe.Replace(f.name)}
	for _, alt := range f.alternates {
		if alt.language == "uz" && usable(alt) {
			current = append(current, alt.name)
		}
	}
	return current
}

func usable(alt alternate) bool {
	return !skippedLanguages[alt.language] && !alt.historic &&
		utf8.RuneCountInString(alt.name) <= maxAlias && !strings.ContainsAny(alt.name, "|()[],;/") &&
		latinOrCyrillic(alt.name)
}

func latinOrCyrillic(name string) bool {
	for _, r := range name {
		if unicode.IsLetter(r) && !unicode.In(r, unicode.Latin, unicode.Cyrillic) {
			return false
		}
	}
	return true
}

func keysOf(names []string) map[string]bool {
	keys := map[string]bool{}
	for _, name := range names {
		if key := placename.Key(name); key != "" {
			keys[key] = true
		}
	}
	return keys
}

// distinct drops spellings that fold to the same as the name or an
// earlier spelling, and names that fold to nothing.
func distinct(name string, aliases []string) []string {
	seen := map[string]bool{placename.Strict(name): true}
	var kept []string
	for _, alias := range aliases {
		strict := placename.Strict(alias)
		if seen[strict] || placename.Key(alias) == "" {
			continue
		}
		seen[strict] = true
		kept = append(kept, alias)
	}
	return kept
}

func townName(f *feature) string {
	if name, found := displayNames[f.id]; found {
		return name
	}
	return trimPlaceWords(uzbekName(f))
}

// adminName is a region's or district's name in Uzbek: "Forish tumani",
// "Xorazm viloyati", or just the name of a city that is a district.
func adminName(f *feature) string {
	if name, found := displayNames[f.id]; found {
		return name
	}
	name := trimPlaceWords(uzbekName(f))
	switch {
	case f.code == "ADM1":
		return name + " viloyati"
	case isPlaceWord(lastWord(f.name), "shahri", "shahar"):
		return name
	default:
		return name + " tumani"
	}
}

// uzbekName is GeoNames' Uzbek name of f in Latin letters, preferring one
// marked preferred, then one spelled as f's main name, then one in
// today's letters, then one with o' or g'; without one, f's main name.
// The old romanization (ŭ, gh, kh) is put in today's letters.
func uzbekName(f *feature) string {
	best, bestScore := f.name, -1
	for _, alt := range f.alternates {
		if alt.language != "uz" || !usable(alt) || hasCyrillic(alt.name) {
			continue
		}
		score := 0
		if alt.preferred {
			score += 8
		}
		if placename.Strict(alt.name) == placename.Strict(f.name) {
			score += 4
		}
		if !oldRomanization(alt.name) {
			score += 2
		}
		if strings.ContainsAny(alt.name, "'‘’ʻʼ`") {
			score++
		}
		if score > bestScore {
			best, bestScore = alt.name, score
		}
	}
	return apostrophes.Replace(todaysLetters.Replace(best))
}

var (
	todaysLetters = strings.NewReplacer("Ŭ", "O'", "ŭ", "o'", "Gh", "G'", "gh", "g'", "Kh", "X", "kh", "x")
	apostrophes   = strings.NewReplacer("‘", "'", "’", "'", "ʻ", "'", "ʼ", "'", "`", "'", "´", "'")
)

func oldRomanization(name string) bool {
	lower := strings.ToLower(name)
	return strings.ContainsRune(lower, 'ŭ') || strings.Contains(lower, "gh") || strings.Contains(lower, "kh")
}

func hasCyrillic(name string) bool {
	for _, r := range name {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// trimPlaceWords drops "Shahri", "Tumani" and the like from the end of a
// name, leaving at least one word.
func trimPlaceWords(name string) string {
	words := strings.Fields(name)
	for len(words) > 1 && isPlaceWord(words[len(words)-1], "shahri", "shahar", "shaharchasi", "qishlogi", "tumani", "viloyati", "district", "region", "rayoni", "city") {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " ")
}

func isPlaceWord(word string, placeWords ...string) bool {
	plain := strings.ToLower(strings.Map(func(r rune) rune {
		if strings.ContainsRune("'‘’ʻʼ`´", r) {
			return -1
		}
		return r
	}, word))
	return slices.Contains(placeWords, plain)
}

func lastWord(name string) string {
	words := strings.Fields(name)
	if len(words) == 0 {
		return ""
	}
	return words[len(words)-1]
}

const header = `// Code generated by genplaces from GeoNames (https://www.geonames.org, CC BY 4.0); DO NOT EDIT.

package webapi

// uzbekPlaces are the towns, districts and regions of Uzbekistan that
// get_weather finds without a geocoder. Data: GeoNames (CC BY 4.0).
var uzbekPlaces = [...]uzbekPlace{
`

func render(rows []*entry) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(header)
	for _, row := range rows {
		fmt.Fprintf(&b, "\t{%q, %s, %s, %d, %q, %q},\n", row.name, coordinate(row.at.lat), coordinate(row.at.lon),
			row.population, row.code, strings.Join(row.aliases, "|"))
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}

func coordinate(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
