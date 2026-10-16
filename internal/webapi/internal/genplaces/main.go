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
// every region and district (ADM1, ADM2), each district at its seat. A
// region or district named like its seat (Paxtakor Tumani and Paxtakor)
// adds its names to the seat; one that is not (Bo'stonliq Tumani and
// G'azalkent) is a row of its own at its seat. GeoNames links few
// districts to their seats, and some of its seats are stale, so fixes.go
// names the seat of every district by its GeoNames id.
//
// A name in a language other than Uzbek is kept only if it is the place's
// own name written another way (Kokand for Qo'qon, Farish for Forish), not
// a Soviet name that is today another city's (Ul'yanovsk for Mug'lon,
// Kuybyshev for Rishton).
package main

import (
	"bufio"
	"bytes"
	"cmp"
	"flag"
	"fmt"
	"go/format"
	"log"
	"maps"
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
	// duplicateReach is how close two towns sharing a name must be to be
	// one town GeoNames lists twice (Pop and Pop Shahri).
	duplicateReach = 0.1
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
	// current are today's names: GeoNames' name and ASCII name, the Uzbek
	// ones and the fixes. keys fold every kept name; currentKeys only
	// today's, so a district is not taken for a town that shares an old
	// Soviet name with it (Kirovskiy Rayon).
	current           []string
	keys, currentKeys map[string]bool
	// twins are today's names of the places GeoNames lists twice for this
	// one (sameAs): its own names too, but never shown.
	twins []string
}

type alternate struct {
	language, name      string
	preferred, historic bool
}

// kinds of a row, as places.go declares them.
const (
	kindTown     = 1
	kindDistrict = 2
	kindRegion   = 4
)

// entry is one row of the table.
type entry struct {
	name       string
	at         *feature // whose coordinates the row has
	population int
	code       string
	kinds      int
	aliases    []string
}

func main() {
	placesPath := flag.String("places", "", "UZ.txt from export/dump/UZ.zip")
	alternatesPath := flag.String("alternates", "", "UZ.txt from export/dump/alternatenames/UZ.zip")
	out := flag.String("out", "places_uz_gen.go", "file to write")
	report := flag.Bool("report", false, "print where each region and district was put")
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
	features = append(features, newDistricts()...)
	rows := table(features, *report)
	source, err := render(rows)
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

// newDistricts are the districts GeoNames lacks, as features of their own.
func newDistricts() []*feature {
	var features []*feature
	for _, d := range missingDistricts {
		f := &feature{id: d.id, name: d.name, ascii: d.name, lat: d.lat, lon: d.lon, class: "A", code: "ADM2", admin1: d.region}
		for _, name := range d.names {
			f.alternates = append(f.alternates, alternate{language: "uz", name: name})
		}
		features = append(features, f)
	}
	return features
}

// table builds the rows: towns, then regions and districts merged into
// their seats or added on their own, then towns GeoNames lists twice made
// one.
func table(features []*feature, report bool) []*entry {
	byID := map[int]*feature{}
	for _, f := range features {
		byID[f.id] = f
	}
	checkFixes(byID)
	for _, f := range features {
		applyFixes(f)
	}
	// A duplicate's names and people go to the place it duplicates: its
	// names today are that place's names too, its others are filtered as
	// the place's own are.
	for _, duplicate := range slices.Sorted(maps.Keys(sameAs)) {
		f, canonical := byID[duplicate], byID[sameAs[duplicate]]
		canonical.twins = append(canonical.twins, f.name, f.ascii)
		for _, alt := range f.alternates {
			if alt.language == "uz" && usable(alt) {
				canonical.twins = append(canonical.twins, alt.name)
			} else if alt.language != "uz" {
				canonical.alternates = append(canonical.alternates, alt)
			}
		}
		canonical.population = max(canonical.population, f.population)
	}
	for _, f := range features {
		f.current = currentNames(f)
		f.currentKeys = keysOf(f.current)
		f.keys = keysOf(names(f))
	}
	var rows []*entry
	towns := map[int]*entry{}
	addTown := func(f *feature) *entry {
		if canonical, found := sameAs[f.id]; found {
			f = byID[canonical]
		}
		if towns[f.id] == nil {
			towns[f.id] = &entry{name: townName(f), at: f, population: f.population, code: f.code, kinds: kindTown, aliases: names(f)}
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
		if _, duplicate := sameAs[f.id]; duplicate || (f.code != "ADM1" && f.code != "ADM2") {
			continue
		}
		seat := findSeat(f, byID, features)
		kind := adminKind(f)
		if seat == nil {
			rows = append(rows, &entry{name: adminName(f), at: f, population: f.population, code: f.code, kinds: kind, aliases: names(f)})
			reportAdmin(report, f, nil, "own point")
			continue
		}
		town := addTown(seat)
		town.code = better(town.code, seatCodes[f.code])
		if kind == kindTown || namedLike(f, seat) {
			town.kinds |= kind
			town.population = max(town.population, f.population)
			town.aliases = append(town.aliases, names(f)...)
			reportAdmin(report, f, seat, "merged")
		} else {
			rows = append(rows, &entry{name: adminName(f), at: seat, population: f.population, code: f.code, kinds: kind, aliases: names(f)})
			reportAdmin(report, f, seat, "at seat")
		}
	}
	rows = mergeDuplicates(rows)
	for _, row := range rows {
		row.aliases = distinct(row.name, row.aliases)
	}
	slices.SortStableFunc(rows, func(x, y *entry) int {
		return cmp.Or(strings.Compare(x.name, y.name), cmp.Compare(x.at.lat, y.at.lat), cmp.Compare(x.at.lon, y.at.lon), strings.Compare(x.code, y.code))
	})
	return rows
}

func reportAdmin(report bool, admin, seat *feature, how string) {
	if !report {
		return
	}
	if seat == nil {
		fmt.Fprintf(os.Stderr, "%s\t%d\t%s\t%s\t-\t-\t-\n", admin.code, admin.id, admin.name, how)
		return
	}
	fmt.Fprintf(os.Stderr, "%s\t%d\t%s\t%s\t%d\t%s\t%.2f\n", admin.code, admin.id, admin.name, how, seat.id, seat.name, math.Hypot(seat.lat-admin.lat, seat.lon-admin.lon))
}

// adminKind is what a region or district is to a member: a city that is
// a district of its own (Andijon Shahri) is a town.
func adminKind(f *feature) int {
	switch {
	case f.code == "ADM1":
		return kindRegion
	case isPlaceWord(lastWord(f.name), "shahri", "shahar"):
		return kindTown
	default:
		return kindDistrict
	}
}

// mergeDuplicates makes one row of towns that share a name and lie within
// duplicateReach: the seat's name, code and point, the larger population
// and both rows' names and kinds.
func mergeDuplicates(rows []*entry) []*entry {
	var kept []*entry
	for _, row := range rows {
		merged := false
		for _, other := range kept {
			if other.kinds&kindTown == 0 || row.kinds&kindTown == 0 || !shareName(row, other) ||
				math.Hypot(row.at.lat-other.at.lat, row.at.lon-other.at.lon) > duplicateReach {
				continue
			}
			other.aliases = slices.Concat(other.aliases, []string{other.name, row.name}, row.aliases)
			if slices.Index(placeCodes, row.code) < slices.Index(placeCodes, other.code) {
				other.name, other.at, other.code = row.name, row.at, row.code
			}
			other.population = max(other.population, row.population)
			other.kinds |= row.kinds
			merged = true
			break
		}
		if !merged {
			kept = append(kept, row)
		}
	}
	return kept
}

func shareName(x, y *entry) bool {
	keys := keysOf(append([]string{x.name}, x.aliases...))
	for _, name := range append([]string{y.name}, y.aliases...) {
		if keys[placename.Key(name)] {
			return true
		}
	}
	return false
}

// checkFixes stops when a fix in fixes.go names a place GeoNames no longer
// has, rather than drop the fix unnoticed.
func checkFixes(byID map[int]*feature) {
	var ids []int
	for admin, seat := range seats {
		ids = append(ids, admin, seat)
	}
	for duplicate, canonical := range sameAs {
		ids = append(ids, duplicate, canonical)
	}
	for _, fixes := range []map[int]bool{extraTowns, demoted, promoted} {
		for id := range fixes {
			ids = append(ids, id)
		}
	}
	for id := range regionFixes {
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

func applyFixes(f *feature) {
	if demoted[f.id] {
		f.code = "PPL"
	}
	if promoted[f.id] {
		f.code = "PPLA2"
	}
	if region, found := regionFixes[f.id]; found {
		f.admin1 = region
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
// fixed in fixes.go, the region centre or linked district seat GeoNames
// has, or else a town of the same name nearby.
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
	for key := range admin.currentKeys {
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

// names are a feature's spellings: today's names, and the other alternate
// names in Latin or Cyrillic letters that are the same name written
// another way (sameName), with Russian adjectives ("Farishskiy Rayon")
// read as the nouns they come from (Farish). GeoNames' ŭ (Uzbek ў) is
// also written o', as Uzbek Latin has it.
func names(f *feature) []string {
	all := slices.Clone(f.current)
	for _, alt := range f.alternates {
		if alt.language == "uz" || !usable(alt) {
			continue
		}
		nouns := placename.Nouns(alt.name)
		if nouns == nil {
			if sameName(alt.name, f.current) {
				all = append(all, alt.name)
			}
			continue
		}
		// Of the nouns an adjective may come from, only those with the
		// very consonants and the ending of a name today: Zangiatinskiy is
		// Zangiata, as Zangiota ends in a vowel, but Farishskiy is Farish.
		for _, noun := range nouns {
			if sameSkeleton(noun, f.current) {
				all = append(all, capitalized(noun))
			}
		}
	}
	for _, name := range all {
		if strings.ContainsAny(name, "ŭŬ") {
			all = append(all, oWithApostrophe.Replace(name))
		}
	}
	return all
}

var oWithApostrophe = strings.NewReplacer("ŭ", "o'", "Ŭ", "O'")

// currentNames are GeoNames' name and ASCII name, the Uzbek names, the
// fixes' names and the names of its twins.
func currentNames(f *feature) []string {
	current := []string{f.name, f.ascii, oWithApostrophe.Replace(f.name)}
	if name, found := displayNames[f.id]; found {
		current = append(current, name)
	}
	current = append(current, extraNames[f.id]...)
	current = append(current, f.twins...)
	for _, alt := range f.alternates {
		if alt.language == "uz" && usable(alt) {
			current = append(current, alt.name)
		}
	}
	return current
}

func sameSkeleton(noun string, names []string) bool {
	for _, name := range names {
		if skeleton(noun) == skeleton(name) && ending(noun) == ending(name) {
			return true
		}
	}
	return false
}

// ending is the kind of sound a name's key ends in: a consonant, a or o
// (Uzbek o is Russian a), i, e or y, or u.
func ending(name string) byte {
	key := placename.Key(name)
	if key == "" {
		return 0
	}
	switch last := key[len(key)-1]; {
	case last == 'a' || last == 'o':
		return 'a'
	case last == 'i' || last == 'e' || last == 'y':
		return 'i'
	case last == 'u':
		return 'u'
	default:
		return 'c'
	}
}

func capitalized(name string) string {
	words := strings.Fields(name)
	for i, word := range words {
		first, size := utf8.DecodeRuneInString(word)
		words[i] = string(unicode.ToUpper(first)) + word[size:]
	}
	return strings.Join(words, " ")
}

// sameName reports whether alias is one of names written another way: it
// has as many words, and their consonants agree but for one more or one
// other at the end (Kokand and Qo'qon, Dzhizak and Jizzax), while
// Kuybyshev and Rishton, or Kyzyl-Tepa and Qo'rg'ontepa, have little in
// common.
func sameName(alias string, names []string) bool {
	a := skeleton(alias)
	if a == "" {
		return false
	}
	for _, name := range names {
		if alike(a, skeleton(name)) && len(placename.Words(alias)) == len(placename.Words(name)) {
			return true
		}
	}
	return false
}

func alike(a, b string) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	common := 0
	for common < len(a) && a[common] == b[common] {
		common++
	}
	switch {
	case common == len(a):
		return len(b)-len(a) <= 1 && len(a) >= 1
	case common == len(a)-1:
		return len(a) == len(b) && len(a) >= 3
	default:
		return false
	}
}

// skeleton is a name's folded key without vowels and doubled letters.
func skeleton(name string) string {
	var b []byte
	for _, c := range []byte(placename.Key(name)) {
		if strings.IndexByte("aeiouy", c) >= 0 || c < 'a' || c > 'z' {
			continue
		}
		if len(b) > 0 && b[len(b)-1] == c {
			continue
		}
		b = append(b, c)
	}
	return string(b)
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
	switch adminKind(f) {
	case kindRegion:
		return name + " viloyati"
	case kindTown:
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
		region, err := strconv.Atoi(row.at.admin1)
		if err != nil {
			return nil, fmt.Errorf("%s has no region: %w", row.name, err)
		}
		fmt.Fprintf(&b, "\t{%q, %s, %s, %d, %q, %d, %s, %q},\n", row.name, coordinate(row.at.lat), coordinate(row.at.lon),
			row.population, row.code, region, kindsOf(row.kinds), strings.Join(row.aliases, "|"))
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}

func kindsOf(kinds int) string {
	var names []string
	for _, kind := range []struct {
		bit  int
		name string
	}{{kindTown, "townKind"}, {kindDistrict, "districtKind"}, {kindRegion, "regionKind"}} {
		if kinds&kind.bit != 0 {
			names = append(names, kind.name)
		}
	}
	return strings.Join(names, " | ")
}

func coordinate(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
