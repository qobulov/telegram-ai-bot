package webapi

import "strings"

// place is a city the tools know without asking a geocoder, which finds
// nothing for Uzbek Cyrillic names and sometimes picks a namesake abroad.
type place struct {
	// name is the Latin Uzbek name shown to the model.
	name string
	// region is the prayer_times key of a region centre, empty otherwise.
	region   string
	lat, lon float64
	// aliases are other spellings: English, Uzbek Cyrillic, Russian.
	aliases []string
}

var places = []place{
	{name: "Toshkent", region: "toshkent", lat: 41.2995, lon: 69.2401, aliases: []string{"Tashkent", "Тошкент", "Ташкент"}},
	{name: "Nukus", region: "nukus", lat: 42.4586, lon: 59.6058, aliases: []string{"Нукус"}},
	{name: "Andijon", region: "andijon", lat: 40.7834, lon: 72.3507, aliases: []string{"Andijan", "Андижон", "Андижан"}},
	{name: "Buxoro", region: "buxoro", lat: 39.7703, lon: 64.4307, aliases: []string{"Bukhara", "Buhara", "Бухоро", "Бухара"}},
	{name: "Jizzax", region: "jizzax", lat: 40.1335, lon: 67.8296, aliases: []string{"Jizzakh", "Djizak", "Жиззах", "Джизак"}},
	{name: "Qarshi", region: "qarshi", lat: 38.8606, lon: 65.7891, aliases: []string{"Karshi", "Қарши", "Карши"}},
	{name: "Navoiy", region: "navoiy", lat: 40.0844, lon: 65.3792, aliases: []string{"Navoi", "Навоий", "Навои"}},
	{name: "Namangan", region: "namangan", lat: 40.9983, lon: 71.6726, aliases: []string{"Наманган"}},
	{name: "Samarqand", region: "samarqand", lat: 39.6546, lon: 66.9644, aliases: []string{"Samarkand", "Самарқанд", "Самарканд"}},
	{name: "Guliston", region: "guliston", lat: 40.4954, lon: 68.7754, aliases: []string{"Gulistan", "Гулистон", "Гулистан"}},
	{name: "Termiz", region: "termiz", lat: 37.2242, lon: 67.2783, aliases: []string{"Termez", "Термиз", "Термез"}},
	{name: "Farg'ona", region: "fargona", lat: 40.3842, lon: 71.7843, aliases: []string{"Fergana", "Ferghana", "Фарғона", "Фергана"}},
	{name: "Urganch", region: "urganch", lat: 41.5518, lon: 60.6314, aliases: []string{"Urgench", "Урганч", "Ургенч"}},
	{name: "Nurafshon", region: "nurafshon", lat: 41.0321, lon: 69.3625, aliases: []string{"Nurafshan", "Нурафшон", "Нурафшан"}},
	{name: "Qo'qon", lat: 40.5286, lon: 70.9425, aliases: []string{"Kokand", "Қўқон", "Коканд"}},
	{name: "Marg'ilon", lat: 40.4724, lon: 71.7246, aliases: []string{"Margilan", "Марғилон", "Маргилан"}},
	{name: "Chirchiq", lat: 41.4689, lon: 69.5822, aliases: []string{"Chirchik", "Чирчиқ", "Чирчик"}},
	{name: "Olmaliq", lat: 40.8447, lon: 69.5983, aliases: []string{"Almalyk", "Олмалиқ", "Алмалык"}},
	{name: "Angren", lat: 41.0167, lon: 70.1436, aliases: []string{"Ангрен"}},
	{name: "Shahrisabz", lat: 39.0578, lon: 66.8342, aliases: []string{"Shakhrisabz", "Шаҳрисабз", "Шахрисабз"}},
	{name: "Xiva", lat: 41.3856, lon: 60.3641, aliases: []string{"Khiva", "Хива"}},
	{name: "Denov", lat: 38.2675, lon: 67.8989, aliases: []string{"Denau", "Денов", "Денау"}},
	{name: "Kattaqo'rg'on", lat: 39.8989, lon: 66.2561, aliases: []string{"Kattakurgan", "Каттақўрғон", "Каттакурган"}},
	{name: "Bekobod", lat: 40.2208, lon: 69.2697, aliases: []string{"Bekabad", "Бекобод", "Бекабад"}},
	{name: "Zarafshon", lat: 41.5800, lon: 64.1949, aliases: []string{"Zarafshan", "Зарафшон", "Зарафшан"}},
}

var placeIndex = indexPlaces()

func indexPlaces() map[string]int {
	index := make(map[string]int)
	for i, p := range places {
		index[normalizeName(p.name)] = i
		for _, alias := range p.aliases {
			index[normalizeName(alias)] = i
		}
	}
	return index
}

// lookupPlace finds a known city by any spelling.
func lookupPlace(name string) (place, bool) {
	key := normalizeName(name)
	i, found := placeIndex[key]
	if key == "" || !found {
		return place{}, false
	}
	return places[i], true
}

// regionPlace finds one of the 14 region centres by any spelling.
func regionPlace(name string) (place, bool) {
	p, found := lookupPlace(name)
	return p, found && p.region != ""
}

// placeWords are dropped from names, so "Toshkent shahri" finds Toshkent.
var placeWords = map[string]bool{"shahri": true, "shahar": true, "city": true, "город": true, "г": true, "шаҳри": true, "шахри": true}

// normalizeName folds the ways people write a name: case, spaces, hyphens
// and the four apostrophes used for o' and g' (', ʻ, ‘, ’).
func normalizeName(name string) string {
	var b strings.Builder
	for _, word := range strings.Fields(strings.ToLower(name)) {
		if placeWords[strings.Trim(word, ".,")] {
			continue
		}
		for _, r := range word {
			switch {
			case isApostrophe(r) || r == '-' || r == '.' || r == ',':
			case r == 'ё':
				b.WriteRune('е')
			default:
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func isApostrophe(r rune) bool {
	return strings.ContainsRune("'`´ʻʼ‘’", r)
}
