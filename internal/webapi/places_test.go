package webapi

import (
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/webapi/internal/placename"
)

// A zone of "" means Uzbekistan. Coordinates are the town meant, or for a
// district its seat, within 0.2 degrees.
var placeTests = []struct {
	input    string
	zone     string
	lat, lon float64
}{
	// Dropped apostrophes reached Boston, Sogamoso in Colombia and Bozoum
	// in the Central African Republic.
	{"Bo'ston", "", 41.85, 60.95},
	{"Bo‘ston", "", 41.85, 60.95},
	{"Boʻston", "", 41.85, 60.95},
	{"So'x", "", 39.96, 71.13},
	{"Bo‘z", "", 40.69, 71.92},
	// Kogon was the geocoder's 8th answer; with 5 asked, Cogon in the
	// Philippines, Olot in Spain and Juma in Zambia won.
	{"Kogon", "", 39.73, 64.55},
	{"Olot", "", 39.42, 63.80},
	{"Juma", "", 39.72, 66.66},
	// GeoNames has no Uzbek place under these spellings, so the geocoder
	// took a lookalike abroad.
	{"Forish", "", 40.42, 67.18},
	{"Besharyk", "", 40.44, 70.61},
	{"Guzar", "", 38.62, 66.25},
	{"Kamashi", "", 38.82, 66.46},
	{"Baysun", "", 38.21, 67.21},
	{"Rishtan", "", 40.36, 71.28},
	{"Akhangaran", "", 40.91, 69.64},
	{"Bagdad", "", 40.46, 71.21},
	{"Пахтакор", "", 40.31, 67.96},
	{"Олот", "", 39.42, 63.80},
	{"Бўстон", "", 41.85, 60.95},
	{"Сўх", "", 39.96, 71.13},
	{"Сох", "", 39.96, 71.13},
	{"Боз", "", 40.69, 71.92},
	{"Бўз", "", 40.69, 71.92},
	{"Алат", "", 39.42, 63.80},
	// The town meant, not a village of the same name.
	{"Paxtakor", "", 40.31, 67.96},
	{"Bo'stonliq", "", 41.56, 69.77},
	{"Zaamin", "", 39.96, 68.40},
	{"Dangara", "", 40.58, 70.91},
	{"Uchqo'rg'on", "", 41.11, 72.08},
	// Cities abroad by their Uzbek and Russian names, not villages.
	{"Pekin", "Asia/Shanghai", 39.91, 116.40},
	{"Rim", "Europe/Rome", 41.89, 12.51},
	{"Tokio", "Asia/Tokyo", 35.69, 139.69},
	{"Kiyev", "Europe/Kiev", 50.45, 30.52},
	{"Praga", "Europe/Prague", 50.09, 14.42},
	{"Stambul", "Europe/Istanbul", 41.01, 28.95},
	{"Istambul", "Europe/Istanbul", 41.01, 28.95},
	{"Jidda", "Asia/Riyadh", 21.49, 39.19},
	{"Hirot", "Asia/Kabul", 34.35, 62.20},
	// Right before, and must stay so.
	{"Baxt", "", 40.72, 68.69},
	{"Pop", "", 40.88, 71.10},
	{"Urgut", "", 39.42, 67.26},
	{"Qo'ng'irot", "", 43.05, 58.85},
	{"To'rtko'l", "", 41.56, 61.00},
	{"G'uzor", "", 38.62, 66.25},
	{"Bulung'ur", "", 39.76, 67.27},
	{"G'allaorol", "", 40.03, 67.59},
	{"Yangiyo'l", "", 41.11, 69.05},
	{"G'ijduvon", "", 40.10, 64.68},
	{"Chust", "", 41.00, 71.24},
	{"Rishton", "", 40.36, 71.28},
	{"Kitob", "", 39.12, 66.88},
	{"Romitan", "", 39.93, 64.38},
	{"Toshkent", "", 41.26, 69.22},
	{"Тошкент", "", 41.26, 69.22},
	{"Ташкент", "", 41.26, 69.22},
	{"Samarqand", "", 39.65, 66.96},
	{"Chimkent", "Asia/Almaty", 42.31, 69.60},
	{"New York", "America/New_York", 40.71, -74.01},
	{"London", "Europe/London", 51.51, -0.13},
	{"Paris", "Europe/Paris", 48.85, 2.35},
	{"Parij", "Europe/Paris", 48.85, 2.35},
	{"Berlin", "Europe/Berlin", 52.52, 13.41},
	{"Ankara", "Europe/Istanbul", 39.92, 32.85},
	{"Tokyo", "Asia/Tokyo", 35.69, 139.69},
	{"Osh", "Asia/Bishkek", 40.53, 72.80},
	{"Seul", "Asia/Seoul", 37.57, 126.98},
	{"Moskva", "Europe/Moscow", 55.75, 37.62},
	{"Astana", "Asia/Almaty", 51.18, 71.45},
	{"Makka", "Asia/Riyadh", 21.43, 39.83},
	{"Istanbul", "Europe/Istanbul", 41.01, 28.95},
	{"Almaty", "Asia/Almaty", 43.25, 76.91},
	{"Dubai", "Asia/Dubai", 25.08, 55.31},
	// A spelling that is exact abroad beats one that matches an Uzbek
	// town only without its apostrophe.
	{"Boston", "America/New_York", 42.36, -71.06},
	{"Бостон", "America/New_York", 42.36, -71.06},
	// A region or district by its name.
	{"Qashqadaryo viloyati", "", 38.86, 65.79},
	{"Forish tumani", "", 40.42, 67.18},
	{"Chilonzor", "", 41.27, 69.20},

	// An Uzbek place word says the place is Uzbek: Bo'ston, not Boston.
	{"Boston shahri", "", 41.85, 60.95},
	{"Boston Shahri", "", 41.85, 60.95},
	{"Boston tumani", "", 40.69, 71.92},
	// And which of the Uzbek namesakes: the district of Andijan that took
	// the name Bo'ston (seat Bo'z), Navoiy's Navbahor (seat Beshrabot),
	// Tashkent's Uchtepa, Khorezm's Yangibozor.
	{"Bo'ston tumani", "", 40.69, 71.92},
	{"Bo‘ston tumani", "", 40.69, 71.92},
	{"Bo’ston tumani", "", 40.69, 71.92},
	{"Boʻston tumani", "", 40.69, 71.92},
	{"Bo`ston tumani", "", 40.69, 71.92},
	{"Бўстон тумани", "", 40.69, 71.92},
	{"Navbahor tumani", "", 40.20, 65.34},
	{"Навбаҳор тумани", "", 40.20, 65.34},
	{"Uchtepa tumani", "", 41.30, 69.18},
	{"Учтепа тумани", "", 41.30, 69.18},
	{"Yangibozor tumani", "", 41.71, 60.53},
	{"Янгибозор тумани", "", 41.71, 60.53},
	{"Sirdaryo tumani", "", 40.84, 68.66},
	{"Sirdaryo shahri", "", 40.84, 68.66},
	// A district of the capital before a seat elsewhere, a seat before
	// its region.
	{"Учтепа", "", 41.30, 69.18},
	{"Sirdaryo", "", 40.84, 68.66},
	{"Сирдарё", "", 40.84, 68.66},
	// Districts at their seats, which GeoNames does not link or links
	// wrong.
	{"Kokdala", "", 39.22, 66.16},
	{"Кокдала", "", 39.22, 66.16},
	{"Кўкдала", "", 39.22, 66.16},
	{"Карлук", "", 38.19, 67.69},
	{"Karluk", "", 38.19, 67.69},
	{"Qo'shtepa", "", 40.52, 71.66},
	{"Qo‘shtepa", "", 40.52, 71.66},
	{"Qoshtepa", "", 40.52, 71.66},
	{"Қўштепа", "", 40.52, 71.66},
	{"Куштепа", "", 40.52, 71.66},
	{"Yangihayot", "", 41.22, 69.20},
	{"Yangihayot tumani", "", 41.22, 69.20},
	{"Ellikqal'a", "", 41.85, 60.95},
	{"Ellikqalʼa", "", 41.85, 60.95},
	{"Элликқалъа", "", 41.85, 60.95},
	{"Элликкала", "", 41.85, 60.95},
	{"Tomdi", "", 41.75, 64.62},
	{"Томди", "", 41.75, 64.62},
	{"Peshku", "", 40.04, 64.39},
	{"Пешку", "", 40.04, 64.39},
	{"Taxtakopir", "", 43.01, 60.30},
	{"Arnasoy", "", 40.50, 67.88},
	{"Арнасай", "", 40.50, 67.88},
	{"Baxmal", "", 39.74, 67.65},
	{"Бахмал", "", 39.74, 67.65},
	{"Tuproqqal'a", "", 41.22, 61.31},
	{"Yangiobod tumani", "", 39.89, 68.79},
	{"Sayxunobod", "", 40.71, 68.84},
	{"Muzrabot", "", 37.46, 66.92},
	{"Gulbahor", "", 41.09, 69.02},
	{"Keles", "", 41.40, 69.21},
	// Aliases that were another place's: Kyzyl-Tepa was Qo'rg'ontepa's,
	// Shakhimardan G'ozg'on's, Almazar Zafar's.
	{"Кызылтепа", "", 40.03, 64.85},
	{"Kyzyltepa", "", 40.03, 64.85},
	{"Shakhimardan", "", 39.98, 71.81},
	{"Алмазар", "", 41.36, 69.23},
	// Russian names of districts and of their seats.
	{"Фариш", "", 40.42, 67.18},
	{"Нишан", "", 38.65, 65.69},
	{"Хатырчи", "", 40.03, 65.96},
	{"Мингбулак", "", 40.86, 71.46},
	{"Алтынсай", "", 38.19, 67.69},
	{"Амударья", "", 42.12, 60.06},
	{"Канлыкуль", "", 42.84, 59.00},
	{"Бозатау", "", 43.20, 59.67},
	{"Зангиата", "", 41.25, 69.15},
	{"Бостанлык", "", 41.56, 69.77},
	{"Bostanlyk", "", 41.56, 69.77},
	{"Чиланзар", "", 41.27, 69.20},
	{"Юнусабад", "", 41.36, 69.29},
	{"Яккасарай", "", 41.28, 69.25},
	{"Шайхантахур", "", 41.32, 69.21},
	{"Мирабад", "", 41.28, 69.30},
	{"Яшнабад", "", 41.28, 69.34},
	{"Дустабад", "", 40.86, 68.93},
	{"Dostobod", "", 40.86, 68.93},
	{"Джумашуй", "", 40.86, 71.46},
	{"Jomashoy", "", 40.86, 71.46},
	{"Сарык", "", 37.67, 67.23},
	{"Халкабад", "", 37.46, 66.92},
	{"Гулабад", "", 39.58, 66.96},
	{"Сайхун", "", 40.71, 68.84},
	{"Карауул", "", 41.55, 60.58},
	{"Каршина", "", 38.34, 66.56},
	{"Бешрабат", "", 40.20, 65.34},
	{"Янгирабат", "", 40.03, 65.96},
	{"Мирзаабад", "", 40.50, 68.56},
	{"Эшонгузар", "", 41.25, 69.15},
	{"Китабский район", "", 39.12, 66.88},
	{"Бостанлыкский район", "", 41.56, 69.77},
	{"Хорезмская область", "", 41.55, 60.63},
	{"Кашкадарьинская область", "", 38.86, 65.79},
	// h and x are one letter to members.
	{"Xazorasp", "", 41.32, 61.07},
	// Place words, abbreviated and as typed on a Russian keyboard.
	{"Keles shahri", "", 41.40, 69.21},
	{"Келес шахри", "", 41.40, 69.21},
	{"Toshkent sh.", "", 41.26, 69.22},
	{"Тошкент ш.", "", 41.26, 69.22},
	{"Ташкент шахар", "", 41.26, 69.22},
	{"Ширин шахри", "", 40.22, 69.15},
	// Resorts.
	{"Chorvoq", "", 41.63, 69.94},
	{"Charvak", "", 41.63, 69.94},
	{"Чарвак", "", 41.63, 69.94},
	{"Xumson", "", 41.67, 69.96},
	{"Chimgan", "", 41.57, 70.02},
	{"Бурчмулла", "", 41.62, 70.08},
	// Cities abroad whose names are Uzbek ones too, or that the geocoder
	// cannot find by the names members use.
	{"Baghdad", "Asia/Baghdad", 33.34, 44.40},
	{"Leningrad", "Europe/Moscow", 59.94, 30.31},
	{"Ленинград", "Europe/Moscow", 59.94, 30.31},
	{"Riyad", "Asia/Riyadh", 24.69, 46.72},
	{"Курган-Тюбе", "Asia/Dushanbe", 37.84, 68.78},
	{"Kurgan-Tyube", "Asia/Dushanbe", 37.84, 68.78},
	{"Ansan", "Asia/Seoul", 37.32, 126.82},
	{"Ансан", "Asia/Seoul", 37.32, 126.82},
	{"Qaraghandy", "Asia/Almaty", 49.80, 73.10},
	{"Қарағанди", "Asia/Almaty", 49.80, 73.10},
	{"Saryagash", "Asia/Almaty", 41.46, 69.17},
	{"Jetisoy", "Asia/Almaty", 40.78, 68.33},
	{"O'zgan", "Asia/Bishkek", 40.77, 73.30},
	{"Ko'lob", "Asia/Dushanbe", 37.91, 69.78},
	{"Panjikent", "Asia/Dushanbe", 39.50, 67.61},
	{"Ура-Тюбе", "Asia/Dushanbe", 39.91, 69.00},
	{"Turkmenbashi", "Asia/Ashgabat", 40.02, 52.96},
	{"Konye-Urgench", "Asia/Ashgabat", 42.33, 59.15},
	{"Шибирган", "Asia/Kabul", 36.67, 65.75},
	{"Sheroz", "Asia/Tehran", 29.61, 52.53},
	{"Tegu", "Asia/Seoul", 35.87, 128.59},
}

func TestLookupPlace(t *testing.T) {
	for _, test := range placeTests {
		p, found := lookupPlace(test.input)
		if !found || p.timezone != test.zone || math.Abs(p.lat-test.lat) > 0.2 || math.Abs(p.lon-test.lon) > 0.2 {
			t.Errorf("lookupPlace(%q) = %q %q %v,%v, %v; want %q %v,%v", test.input, p.name, p.timezone, p.lat, p.lon, found, test.zone, test.lat, test.lon)
		}
	}
	// Left to the geocoder: names abroad that the Uzbek table held as old
	// Soviet names of its towns (Ul'yanovsk was Mug'lon, Sverdlovsk
	// Jondor, Kuybyshev Rishton), or that are only villages here.
	unknown := []string{
		"", "Mexico City", "Toshkentda yomg'ir", "'", "shahri", "Bo'ritepa",
		"Ulyanovsk", "Ульяновск", "Ulyanovsk shahri", "Свердловск", "Куйбышев", "Kuybyshev", "Сталино",
		"Zheleznodorozhnyy", "Комсомольск", "Shelek", "Шелек", "Kirov", "Киров", "Aqtau", "Xartum",
		"Qashqar", "Odessa", "Kioto",
	}
	for _, name := range unknown {
		if p, found := lookupPlace(name); found {
			t.Errorf("lookupPlace(%q) = %q, want no match", name, p.name)
		}
	}
}

// The spellings the hand-written table had must still work.
func TestLookupPlaceKeepsTheOldSpellings(t *testing.T) {
	tests := map[string][]string{
		"Toshkent":        {"Toshkent", "  toshkent ", "Tashkent", "Тошкент", "Ташкент", "Toshkent shahri"},
		"Nukus":           {"Нукус"},
		"Andijon":         {"Andijan", "Андижон", "Андижан"},
		"Buxoro":          {"Bukhara", "Buhara", "Бухоро", "Бухара"},
		"Jizzax":          {"Jizzakh", "Djizak", "Жиззах", "Джизак"},
		"Qarshi":          {"Karshi", "Қарши", "Карши"},
		"Navoiy":          {"Navoi", "Навоий", "Навои"},
		"Namangan":        {"Наманган"},
		"Samarqand":       {"Samarkand", "Самарқанд", "Самарканд"},
		"Guliston":        {"Gulistan", "Гулистон", "Гулистан"},
		"Termiz":          {"Termez", "Термиз", "Термез"},
		"Farg'ona":        {"Farg'ona", "Fargʻona", "Farg‘ona", "Farg’ona", "Fargona", "Fergana", "Ferghana", "Фарғона", "Фергана"},
		"Urganch":         {"Urgench", "Урганч", "Ургенч"},
		"Nurafshon":       {"Nurafshan", "Нурафшон", "Нурафшан", "To'ytepa"},
		"Qo'qon":          {"Qo'qon", "Kokand", "Қўқон", "Коканд"},
		"Marg'ilon":       {"Margilan", "Марғилон", "Маргилан"},
		"Chirchiq":        {"Chirchik", "Чирчиқ", "Чирчик"},
		"Olmaliq":         {"Almalyk", "Олмалиқ", "Алмалык"},
		"Angren":          {"Ангрен"},
		"Shahrisabz":      {"Shakhrisabz", "Шаҳрисабз", "Шахрисабз"},
		"Xiva":            {"Khiva", "Хива"},
		"Denov":           {"Denau", "Денов", "Денау"},
		"Kattaqo'rg'on":   {"KATTAQO'RG'ON", "Kattakurgan", "Каттақўрғон", "Каттакурган"},
		"Bekobod":         {"Bekabad", "Бекобод", "Бекабад"},
		"Zarafshon":       {"Zarafshan", "Зарафшон", "Зарафшан"},
		"Yangiyo'l":       {"Yangiyo‘l", "Yangiyul", "Янгийўл", "Янгиюль"},
		"G'ijduvon":       {"Gijduvan", "Ғиждувон", "Гиждуван"},
		"Moskva":          {"Moscow", "Москва", "г. Москва"},
		"Sankt-Peterburg": {"Sankt Peterburg", "St. Petersburg", "Saint Petersburg", "Питер"},
		"Seul":            {"Seoul", "Сеул"},
		"Nyu-York":        {"Nyu York", "New York", "NYC", "Нью-Йорк"},
		"Chimkent":        {"Shymkent", "Чимкент", "Шымкент"},
		"Olmaota":         {"Almaty", "Alma-Ata", "Алматы"},
		"Xo'jand":         {"Khujand", "Хўжанд", "Худжанд"},
		"Qozon":           {"Kazan", "Казань"},
		"Dubay":           {"Dubai", "Дубай"},
	}
	for want, spellings := range tests {
		for _, spelling := range append(spellings, want) {
			if p, found := lookupPlace(spelling); !found || p.name != want {
				t.Errorf("lookupPlace(%q) = %q, %v; want %q", spelling, p.name, found, want)
			}
		}
	}
}

func TestWorldCities(t *testing.T) {
	owner := map[string]string{}
	for _, city := range worldCities {
		if _, err := time.LoadLocation(city.timezone); err != nil {
			t.Errorf("%s: time zone %q: %v", city.name, city.timezone, err)
		}
		if city.timezone == tashkentZone || city.timezone == "Asia/Samarkand" {
			t.Errorf("%s keeps Uzbekistan's time; it belongs in the Uzbek table", city.name)
		}
		for _, spelling := range append([]string{city.name}, city.aliases...) {
			key := placename.Strict(spelling)
			if other, taken := owner[key]; taken && other != city.name {
				t.Errorf("%q names both %s and %s", spelling, other, city.name)
			}
			owner[key] = city.name
			if p, found := lookupPlace(spelling); !found || p.name != city.name {
				t.Errorf("lookupPlace(%q) = %q, %v; want %s", spelling, p.name, found, city.name)
			}
		}
	}
	if n := len(worldCities); n < 80 || n > 130 {
		t.Errorf("%d cities abroad, want 80 to 130", n)
	}
}

func TestCountries(t *testing.T) {
	for _, name := range slices.Concat(countries, []string{"O‘zbekiston", "Oʻzbekiston", "O`zbekiston", "uzbekistan", "ТУРЦИЯ"}) {
		if !isCountry(name) {
			t.Errorf("isCountry(%q) = false", name)
		}
	}
	// A place word makes it a place: O'zbekiston tumani is in Farg'ona.
	for _, name := range []string{"O'zbekiston tumani", "Mexico", "Mexico City", "Toshkent", "Moskva", "Singapur"} {
		if isCountry(name) {
			t.Errorf("isCountry(%q) = true", name)
		}
	}
	if p, found := lookupPlace("O'zbekiston tumani"); !found || math.Abs(p.lat-40.38) > 0.2 || math.Abs(p.lon-70.82) > 0.2 {
		t.Errorf("lookupPlace(O'zbekiston tumani) = %+v, %v", p, found)
	}
}

func TestUzbekPlaces(t *testing.T) {
	for _, p := range uzbekPlaces {
		if _, known := featureRanks[p.feature]; !known || !inUzbekistan(p.lat, p.lon) || p.name == "" || p.kinds == 0 || p.province < 1 || p.province > 16 {
			t.Errorf("%+v", p)
		}
		for _, alias := range strings.Split(p.aliases, "|") {
			if strings.ContainsAny(alias, ",") {
				t.Errorf("%s: alias %q has a comma, which the weather tool reads as a qualifier", p.name, alias)
			}
		}
	}
	if n := len(uzbekPlaces); n < 280 {
		t.Errorf("%d places in Uzbekistan, want at least 280", n)
	}
	// The table is compiled in: it must stay small.
	info, err := os.Stat("places_uz_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 600<<10 {
		t.Errorf("places_uz_gen.go is %d KB, over 600 KB", info.Size()>>10)
	}
}

func inUzbekistan(lat, lon float64) bool {
	return lat >= 37 && lat <= 46 && lon >= 55 && lon <= 74
}

func TestRegionPlace(t *testing.T) {
	if len(regions) != 14 {
		t.Errorf("%d regions, want 14", len(regions))
	}
	for _, r := range regions {
		for _, spelling := range []string{r.key, r.name} {
			if found, ok := regionPlace(spelling); !ok || found.key != r.key {
				t.Errorf("regionPlace(%q) = %+v, %v; want %s", spelling, found, ok, r.key)
			}
		}
	}
	for spelling, want := range map[string]string{"Tashkent": "toshkent", "Самарканд": "samarqand", "Qashqadaryo": "qarshi", "Toshkent viloyati": "toshkent", "Fergana": "fargona"} {
		if found, ok := regionPlace(spelling); !ok || found.key != want {
			t.Errorf("regionPlace(%q) = %+v, %v; want %s", spelling, found, ok, want)
		}
	}
	for _, other := range []string{"Chirchiq", "London", "Kogon", ""} {
		if found, ok := regionPlace(other); ok {
			t.Errorf("regionPlace(%q) = %+v, want none", other, found)
		}
	}
}
