package webapi

// worldCity is a city abroad that members ask about. Coordinates and time
// zones are GeoNames' (CC BY 4.0).
type worldCity struct {
	// name is the Uzbek Latin name shown to the model.
	name     string
	timezone string
	lat, lon float64
	// aliases are the Uzbek Cyrillic, Russian and English names and other
	// spellings members use.
	aliases []string
}

// worldCities are matched before the geocoder, which finds namesakes:
// Seul gives Seulo in Italy, Pekin a village in Illinois, Rim one in
// Croatia, Jidda one in Nigeria, Hirot one in Japan.
var worldCities = []worldCity{
	// Russia, Ukraine, Belarus, the Baltics and Moldova.
	{name: "Moskva", timezone: "Europe/Moscow", lat: 55.7520, lon: 37.6178, aliases: []string{"Moscow", "Москва"}},
	{name: "Sankt-Peterburg", timezone: "Europe/Moscow", lat: 59.9386, lon: 30.3141, aliases: []string{"Saint Petersburg", "St Petersburg", "Peterburg", "Piter", "Leningrad", "Санкт-Петербург", "Петербург", "Питер", "Ленинград"}},
	{name: "Qozon", timezone: "Europe/Moscow", lat: 55.7887, lon: 49.1221, aliases: []string{"Kazan", "Қозон", "Казань"}},
	{name: "Novosibirsk", timezone: "Asia/Novosibirsk", lat: 55.0226, lon: 82.9318, aliases: []string{"Новосибирск"}},
	{name: "Yekaterinburg", timezone: "Asia/Yekaterinburg", lat: 56.8573, lon: 60.6153, aliases: []string{"Ekaterinburg", "Екатеринбург"}},
	{name: "Samara", timezone: "Europe/Samara", lat: 53.2077, lon: 50.1355, aliases: []string{"Самара"}},
	{name: "Krasnodar", timezone: "Europe/Moscow", lat: 45.0453, lon: 38.9818, aliases: []string{"Краснодар"}},
	{name: "Rostov-na-Donu", timezone: "Europe/Moscow", lat: 47.2200, lon: 39.7077, aliases: []string{"Rostov-on-Don", "Rostov", "Ростов-на-Дону", "Ростов"}},
	{name: "Kiyev", timezone: "Europe/Kiev", lat: 50.4547, lon: 30.5238, aliases: []string{"Kiev", "Kyiv", "Kiyv", "Киев", "Київ"}},
	{name: "Minsk", timezone: "Europe/Minsk", lat: 53.9002, lon: 27.5665, aliases: []string{"Минск"}},
	{name: "Riga", timezone: "Europe/Riga", lat: 56.9460, lon: 24.1059, aliases: []string{"Рига"}},
	{name: "Vilnyus", timezone: "Europe/Vilnius", lat: 54.6892, lon: 25.2798, aliases: []string{"Vilnius", "Вильнюс"}},
	{name: "Tallin", timezone: "Europe/Tallinn", lat: 59.4370, lon: 24.7535, aliases: []string{"Tallinn", "Таллин"}},
	{name: "Kishinyov", timezone: "Europe/Chisinau", lat: 47.0090, lon: 28.8594, aliases: []string{"Chisinau", "Chișinău", "Kishinev", "Кишинёв"}},

	// Europe.
	{name: "Varshava", timezone: "Europe/Warsaw", lat: 52.2298, lon: 21.0118, aliases: []string{"Warsaw", "Warszawa", "Варшава"}},
	{name: "Praga", timezone: "Europe/Prague", lat: 50.0880, lon: 14.4208, aliases: []string{"Prague", "Praha", "Прага"}},
	{name: "Vena", timezone: "Europe/Vienna", lat: 48.2085, lon: 16.3721, aliases: []string{"Vienna", "Wien", "Вена"}},
	{name: "Budapesht", timezone: "Europe/Budapest", lat: 47.4984, lon: 19.0405, aliases: []string{"Budapest", "Будапешт"}},
	{name: "Berlin", timezone: "Europe/Berlin", lat: 52.5244, lon: 13.4105, aliases: []string{"Берлин"}},
	{name: "Myunxen", timezone: "Europe/Berlin", lat: 48.1374, lon: 11.5755, aliases: []string{"Myunhen", "Munich", "München", "Muenchen", "Мюнхен"}},
	{name: "Frankfurt", timezone: "Europe/Berlin", lat: 50.1155, lon: 8.6842, aliases: []string{"Frankfurt am Main", "Франкфурт"}},
	{name: "Gamburg", timezone: "Europe/Berlin", lat: 53.5507, lon: 9.9930, aliases: []string{"Hamburg", "Гамбург"}},
	{name: "Parij", timezone: "Europe/Paris", lat: 48.8534, lon: 2.3488, aliases: []string{"Paris", "Париж"}},
	{name: "London", timezone: "Europe/London", lat: 51.5085, lon: -0.1257, aliases: []string{"Лондон"}},
	{name: "Manchester", timezone: "Europe/London", lat: 53.4810, lon: -2.2374, aliases: []string{"Манчестер"}},
	{name: "Madrid", timezone: "Europe/Madrid", lat: 40.4165, lon: -3.7026, aliases: []string{"Мадрид"}},
	{name: "Barselona", timezone: "Europe/Madrid", lat: 41.3888, lon: 2.1590, aliases: []string{"Barcelona", "Барселона"}},
	{name: "Lissabon", timezone: "Europe/Lisbon", lat: 38.7251, lon: -9.1498, aliases: []string{"Lisbon", "Lisboa", "Лиссабон"}},
	{name: "Rim", timezone: "Europe/Rome", lat: 41.8919, lon: 12.5113, aliases: []string{"Rome", "Roma", "Рим"}},
	{name: "Milan", timezone: "Europe/Rome", lat: 45.4643, lon: 9.1895, aliases: []string{"Milano", "Милан"}},
	{name: "Amsterdam", timezone: "Europe/Amsterdam", lat: 52.3740, lon: 4.8897, aliases: []string{"Амстердам"}},
	{name: "Bryussel", timezone: "Europe/Brussels", lat: 50.8505, lon: 4.3488, aliases: []string{"Brussels", "Bruxelles", "Брюссель"}},
	{name: "Jeneva", timezone: "Europe/Zurich", lat: 46.2022, lon: 6.1457, aliases: []string{"Geneva", "Genève", "Geneve", "Женева"}},
	{name: "Syurix", timezone: "Europe/Zurich", lat: 47.3667, lon: 8.5500, aliases: []string{"Zurich", "Zürich", "Цюрих"}},
	{name: "Stokgolm", timezone: "Europe/Stockholm", lat: 59.3294, lon: 18.0687, aliases: []string{"Stockholm", "Стокгольм"}},
	{name: "Oslo", timezone: "Europe/Oslo", lat: 59.9127, lon: 10.7461, aliases: []string{"Осло"}},
	{name: "Kopengagen", timezone: "Europe/Copenhagen", lat: 55.6759, lon: 12.5655, aliases: []string{"Copenhagen", "København", "Копенгаген"}},
	{name: "Xelsinki", timezone: "Europe/Helsinki", lat: 60.1695, lon: 24.9355, aliases: []string{"Helsinki", "Хельсинки"}},
	{name: "Afina", timezone: "Europe/Athens", lat: 37.9838, lon: 23.7278, aliases: []string{"Athens", "Afiny", "Афины"}},
	{name: "Belgrad", timezone: "Europe/Belgrade", lat: 44.8040, lon: 20.4651, aliases: []string{"Belgrade", "Beograd", "Белград"}},
	{name: "Buxarest", timezone: "Europe/Bucharest", lat: 44.4323, lon: 26.1063, aliases: []string{"Bucharest", "București", "Buharest", "Бухарест"}},
	{name: "Sofiya", timezone: "Europe/Sofia", lat: 42.6975, lon: 23.3242, aliases: []string{"Sofia", "София"}},

	// Turkey and the Caucasus.
	{name: "Istanbul", timezone: "Europe/Istanbul", lat: 41.0138, lon: 28.9497, aliases: []string{"İstanbul", "Stambul", "Istambul", "Стамбул", "Истанбул", "Истамбул"}},
	{name: "Anqara", timezone: "Europe/Istanbul", lat: 39.9199, lon: 32.8543, aliases: []string{"Ankara", "Анқара", "Анкара"}},
	{name: "Antaliya", timezone: "Europe/Istanbul", lat: 36.9081, lon: 30.6956, aliases: []string{"Antalya", "Анталья", "Анталия"}},
	{name: "Izmir", timezone: "Europe/Istanbul", lat: 38.4127, lon: 27.1384, aliases: []string{"İzmir", "Измир"}},
	{name: "Boku", timezone: "Asia/Baku", lat: 40.3777, lon: 49.8920, aliases: []string{"Baku", "Bakı", "Баку", "Боку"}},
	{name: "Tbilisi", timezone: "Asia/Tbilisi", lat: 41.6914, lon: 44.8341, aliases: []string{"Тбилиси"}},
	{name: "Yerevan", timezone: "Asia/Yerevan", lat: 40.1777, lon: 44.5126, aliases: []string{"Erevan", "Ереван"}},

	// Iran, Afghanistan, Pakistan and India.
	{name: "Tehron", timezone: "Asia/Tehran", lat: 35.6944, lon: 51.4215, aliases: []string{"Tehran", "Teheran", "Теҳрон", "Тегеран"}},
	{name: "Sheroz", timezone: "Asia/Tehran", lat: 29.6103, lon: 52.5311, aliases: []string{"Shiraz", "Shiroz", "Шероз", "Шираз"}},
	{name: "Mashhad", timezone: "Asia/Tehran", lat: 36.2981, lon: 59.6057, aliases: []string{"Mashad", "Машҳад", "Мешхед"}},
	{name: "Kobul", timezone: "Asia/Kabul", lat: 34.5281, lon: 69.1723, aliases: []string{"Kabul", "Кобул", "Кабул"}},
	{name: "Hirot", timezone: "Asia/Kabul", lat: 34.3482, lon: 62.1997, aliases: []string{"Herat", "Hirat", "Ҳирот", "Герат"}},
	{name: "Mozori Sharif", timezone: "Asia/Kabul", lat: 36.7090, lon: 67.1109, aliases: []string{"Mazar-i-Sharif", "Mazar-e Sharif", "Mazari Sharif", "Мозори Шариф", "Мазари-Шариф"}},
	{name: "Islomobod", timezone: "Asia/Karachi", lat: 33.7215, lon: 73.0433, aliases: []string{"Islamabad", "Исломобод", "Исламабад"}},
	{name: "Lohur", timezone: "Asia/Karachi", lat: 31.5580, lon: 74.3507, aliases: []string{"Lahore", "Лоҳур", "Лахор"}},
	{name: "Dehli", timezone: "Asia/Kolkata", lat: 28.6214, lon: 77.2148, aliases: []string{"Delhi", "New Delhi", "Nyu-Dehli", "Деҳли", "Дели", "Нью-Дели"}},
	{name: "Mumbay", timezone: "Asia/Kolkata", lat: 19.0728, lon: 72.8826, aliases: []string{"Mumbai", "Bombay", "Мумбаи"}},

	// Central Asia.
	{name: "Dushanbe", timezone: "Asia/Dushanbe", lat: 38.5358, lon: 68.7791, aliases: []string{"Душанбе"}},
	{name: "Xo'jand", timezone: "Asia/Dushanbe", lat: 40.2826, lon: 69.6222, aliases: []string{"Khujand", "Khodjent", "Хўжанд", "Худжанд", "Ходжент"}},
	{name: "Bishkek", timezone: "Asia/Bishkek", lat: 42.8700, lon: 74.5900, aliases: []string{"Бишкек"}},
	{name: "O'sh", timezone: "Asia/Bishkek", lat: 40.5283, lon: 72.7985, aliases: []string{"Osh", "Ўш", "Ош"}},
	{name: "Jalolobod", timezone: "Asia/Bishkek", lat: 40.9333, lon: 73.0000, aliases: []string{"Jalal-Abad", "Jalalabad", "Жалолобод", "Джалал-Абад", "Жалал-Абад"}},
	{name: "Olmaota", timezone: "Asia/Almaty", lat: 43.2525, lon: 76.9115, aliases: []string{"Almaty", "Alma-Ata", "Олмаота", "Алматы", "Алма-Ата"}},
	{name: "Astana", timezone: "Asia/Almaty", lat: 51.1801, lon: 71.4460, aliases: []string{"Nur-Sultan", "Астана", "Нур-Султан"}},
	{name: "Chimkent", timezone: "Asia/Almaty", lat: 42.3099, lon: 69.6004, aliases: []string{"Shymkent", "Чимкент", "Шымкент", "Шимкент"}},
	{name: "Turkiston", timezone: "Asia/Almaty", lat: 43.2946, lon: 68.2569, aliases: []string{"Turkistan", "Turkestan", "Туркистон", "Туркестан", "Түркістан"}},
	{name: "Taroz", timezone: "Asia/Almaty", lat: 42.8980, lon: 71.3733, aliases: []string{"Taraz", "Тароз", "Тараз"}},
	{name: "Ashxobod", timezone: "Asia/Ashgabat", lat: 37.9500, lon: 58.3833, aliases: []string{"Ashgabat", "Ashkhabad", "Aşgabat", "Ашхобод", "Ашхабад"}},
	{name: "Qarag'anda", timezone: "Asia/Almaty", lat: 49.8019, lon: 73.1021, aliases: []string{"Qarag'andi", "Karaganda", "Karagandy", "Qaraghandy", "Қарағанда", "Қарағанди", "Караганда", "Қарағанды"}},
	{name: "Aqto'be", timezone: "Asia/Aqtobe", lat: 50.2797, lon: 57.2072, aliases: []string{"Aktobe", "Aqtobe", "Aktyubinsk", "Ақтўбе", "Актобе", "Актюбинск"}},
	{name: "Qizilo'rda", timezone: "Asia/Qyzylorda", lat: 44.8528, lon: 65.5092, aliases: []string{"Kyzylorda", "Qyzylorda", "Қизилўрда", "Кызылорда"}},
	{name: "Saryog'och", timezone: "Asia/Almaty", lat: 41.4604, lon: 69.1679, aliases: []string{"Saryagash", "Saryaghash", "Сарёғоч", "Сарыагаш"}},
	{name: "Jetisoy", timezone: "Asia/Almaty", lat: 40.7763, lon: 68.3277, aliases: []string{"Zhetysay", "Zhetysai", "Жетисой", "Жетысай"}},
	{name: "O'zgan", timezone: "Asia/Bishkek", lat: 40.7699, lon: 73.3007, aliases: []string{"Uzgen", "Özgön", "Ўзган", "Узген", "Узгень"}},
	{name: "Ko'lob", timezone: "Asia/Dushanbe", lat: 37.9146, lon: 69.7845, aliases: []string{"Kulob", "Kulyab", "Кўлоб", "Куляб", "Кулоб"}},
	{name: "Bohtar", timezone: "Asia/Dushanbe", lat: 37.8365, lon: 68.7776, aliases: []string{"Bokhtar", "Kurgan-Tyube", "Qurghonteppa", "Бохтар", "Курган-Тюбе"}},
	{name: "Panjikent", timezone: "Asia/Dushanbe", lat: 39.4952, lon: 67.6093, aliases: []string{"Panjakent", "Penjikent", "Панжикент", "Пенджикент"}},
	{name: "O'ratepa", timezone: "Asia/Dushanbe", lat: 39.9142, lon: 69.0033, aliases: []string{"Istaravshan", "Ura-Tyube", "Ўратепа", "Истаравшан", "Ура-Тюбе"}},
	{name: "Toshhovuz", timezone: "Asia/Ashgabat", lat: 41.8363, lon: 59.9666, aliases: []string{"Dashoguz", "Daşoguz", "Tashauz", "Тошҳовуз", "Дашогуз", "Ташауз"}},
	{name: "Turkmanobod", timezone: "Asia/Ashgabat", lat: 39.0733, lon: 63.5786, aliases: []string{"Türkmenabat", "Turkmenabat", "Chorjo'y", "Chardzhou", "Туркманобод", "Чоржўй", "Туркменабат", "Чарджоу"}},
	{name: "Turkmanboshi", timezone: "Asia/Ashgabat", lat: 40.0222, lon: 52.9552, aliases: []string{"Turkmenbashi", "Türkmenbaşy", "Krasnovodsk", "Туркманбоши", "Туркменбаши", "Красноводск"}},
	{name: "Ko'hna Urganch", timezone: "Asia/Ashgabat", lat: 42.3261, lon: 59.1531, aliases: []string{"Köneürgenç", "Konye-Urgench", "Kunya-Urgench", "Кўҳна Урганч", "Куня-Ургенч", "Кёнеургенч"}},
	{name: "Shibirg'on", timezone: "Asia/Kabul", lat: 36.6676, lon: 65.7529, aliases: []string{"Sheberghan", "Shibirghan", "Шибирғон", "Шибирган", "Шеберган"}},

	// The Middle East and Egypt.
	{name: "Dubay", timezone: "Asia/Dubai", lat: 25.0773, lon: 55.3093, aliases: []string{"Dubai", "Дубай"}},
	{name: "Abu-Dabi", timezone: "Asia/Dubai", lat: 24.4512, lon: 54.3970, aliases: []string{"Abu Dhabi", "Абу-Даби"}},
	{name: "Shorja", timezone: "Asia/Dubai", lat: 25.3342, lon: 55.4122, aliases: []string{"Sharjah", "Шоржа", "Шарджа"}},
	{name: "Makka", timezone: "Asia/Riyadh", lat: 21.4266, lon: 39.8256, aliases: []string{"Mecca", "Makkah", "Макка", "Мекка"}},
	{name: "Madina", timezone: "Asia/Riyadh", lat: 24.4686, lon: 39.6142, aliases: []string{"Medina", "Madinah", "Мадина", "Медина"}},
	{name: "Jidda", timezone: "Asia/Riyadh", lat: 21.4901, lon: 39.1862, aliases: []string{"Jeddah", "Жидда", "Джидда"}},
	{name: "Ar-Riyod", timezone: "Asia/Riyadh", lat: 24.6877, lon: 46.7219, aliases: []string{"Riyod", "Riyadh", "Riyad", "Ар-Риёд", "Эр-Рияд", "Рияд"}},
	// Bag'dod, and Russian Багдад, are the town in Farg'ona.
	{name: "Baghdad", timezone: "Asia/Baghdad", lat: 33.3406, lon: 44.4009},
	{name: "Doha", timezone: "Asia/Qatar", lat: 25.2855, lon: 51.5310, aliases: []string{"Доҳа", "Доха"}},
	{name: "Quvayt", timezone: "Asia/Kuwait", lat: 29.3670, lon: 47.9743, aliases: []string{"Kuwait", "Kuwait City", "Қувайт", "Эль-Кувейт", "Кувейт"}},
	{name: "Qohira", timezone: "Africa/Cairo", lat: 30.0626, lon: 31.2497, aliases: []string{"Cairo", "Қоҳира", "Каир"}},
	{name: "Quddus", timezone: "Asia/Jerusalem", lat: 31.7690, lon: 35.2163, aliases: []string{"Jerusalem", "Al-Quds", "Қуддус", "Иерусалим"}},

	// East and Southeast Asia.
	{name: "Pekin", timezone: "Asia/Shanghai", lat: 39.9075, lon: 116.3972, aliases: []string{"Beijing", "Peking", "Пекин"}},
	{name: "Shanxay", timezone: "Asia/Shanghai", lat: 31.2222, lon: 121.4581, aliases: []string{"Shanghai", "Шанхай"}},
	{name: "Guanchjou", timezone: "Asia/Shanghai", lat: 23.1167, lon: 113.2500, aliases: []string{"Guangzhou", "Гуанчжоу"}},
	{name: "Urumchi", timezone: "Asia/Urumqi", lat: 43.8010, lon: 87.6005, aliases: []string{"Urumqi", "Ürümqi", "Урумчи"}},
	{name: "Gonkong", timezone: "Asia/Hong_Kong", lat: 22.2783, lon: 114.1747, aliases: []string{"Hong Kong", "Гонконг"}},
	{name: "Seul", timezone: "Asia/Seoul", lat: 37.5660, lon: 126.9784, aliases: []string{"Seoul", "Сеул"}},
	{name: "Pusan", timezone: "Asia/Seoul", lat: 35.1017, lon: 129.0300, aliases: []string{"Busan", "Пусан"}},
	// GeoNames names it Ansan-si, so the geocoder offers a French village.
	{name: "Ansan", timezone: "Asia/Seoul", lat: 37.3236, lon: 126.8219, aliases: []string{"Ansan-si", "Ансан"}},
	{name: "Tegu", timezone: "Asia/Seoul", lat: 35.8703, lon: 128.5911, aliases: []string{"Daegu", "Тегу"}},
	{name: "Kvanju", timezone: "Asia/Seoul", lat: 35.1547, lon: 126.9156, aliases: []string{"Gwangju", "Kwangju", "Кванджу", "Кванжу"}},
	{name: "Tokio", timezone: "Asia/Tokyo", lat: 35.6895, lon: 139.6917, aliases: []string{"Tokyo", "Токио"}},
	{name: "Osaka", timezone: "Asia/Tokyo", lat: 34.6938, lon: 135.5011, aliases: []string{"Осака"}},
	{name: "Ulan-Bator", timezone: "Asia/Ulaanbaatar", lat: 47.9077, lon: 106.8832, aliases: []string{"Ulaanbaatar", "Улан-Батор"}},
	{name: "Bangkok", timezone: "Asia/Bangkok", lat: 13.7540, lon: 100.5014, aliases: []string{"Бангкок"}},
	{name: "Xanoy", timezone: "Asia/Ho_Chi_Minh", lat: 21.0245, lon: 105.8412, aliases: []string{"Hanoi", "Ханой"}},
	{name: "Kuala-Lumpur", timezone: "Asia/Kuala_Lumpur", lat: 3.1412, lon: 101.6865, aliases: []string{"Kuala Lumpur", "Куала-Лумпур"}},
	{name: "Singapur", timezone: "Asia/Singapore", lat: 1.2897, lon: 103.8501, aliases: []string{"Singapore", "Сингапур"}},
	{name: "Jakarta", timezone: "Asia/Jakarta", lat: -6.2146, lon: 106.8451, aliases: []string{"Жакарта", "Джакарта"}},
	{name: "Manila", timezone: "Asia/Manila", lat: 14.6042, lon: 120.9822, aliases: []string{"Манила"}},

	// America and Australia.
	{name: "Nyu-York", timezone: "America/New_York", lat: 40.7143, lon: -74.0060, aliases: []string{"New York", "New York City", "NYC", "Нью-Йорк"}},
	{name: "Vashington", timezone: "America/New_York", lat: 38.8951, lon: -77.0364, aliases: []string{"Washington", "Washington DC", "Вашингтон"}},
	{name: "Boston", timezone: "America/New_York", lat: 42.3584, lon: -71.0598, aliases: []string{"Бостон"}},
	{name: "Mayami", timezone: "America/New_York", lat: 25.7743, lon: -80.1937, aliases: []string{"Miami", "Майами"}},
	{name: "Chikago", timezone: "America/Chicago", lat: 41.8500, lon: -87.6501, aliases: []string{"Chicago", "Чикаго"}},
	{name: "Los-Anjeles", timezone: "America/Los_Angeles", lat: 34.0522, lon: -118.2437, aliases: []string{"Los Angeles", "Лос-Анжелес", "Лос-Анджелес"}},
	{name: "San-Fransisko", timezone: "America/Los_Angeles", lat: 37.7749, lon: -122.4194, aliases: []string{"San Francisco", "Сан-Франсиско", "Сан-Франциско"}},
	{name: "Toronto", timezone: "America/Toronto", lat: 43.7064, lon: -79.3986, aliases: []string{"Торонто"}},
	{name: "Sidney", timezone: "Australia/Sydney", lat: -33.8679, lon: 151.2073, aliases: []string{"Sydney", "Сидней"}},
}

// countries are the names of countries members write where a town is
// wanted, in Uzbek, Russian and English. The weather tool asks for a town
// instead of forecasting a namesake: Turkiya is a village in Nepal, Xitoy
// one in Qashqadaryo, Rossiya one in Orenburg oblast. The geocoder knows
// the rest: a country is among its answers.
var countries = []string{
	"O'zbekiston", "Uzbekistan", "Ўзбекистон", "Узбекистан",
	"Qozog'iston", "Kazakhstan", "Қозоғистон", "Казахстан",
	"Qirg'iziston", "Kyrgyzstan", "Kirgiziya", "Қирғизистон", "Кыргызстан", "Киргизия",
	"Tojikiston", "Tajikistan", "Тожикистон", "Таджикистан",
	"Turkmaniston", "Turkmenistan", "Туркманистон", "Туркменистан",
	"Afg'oniston", "Afghanistan", "Афғонистон", "Афганистан",
	"Rossiya", "Russia", "Россия",
	"Ukraina", "Ukraine", "Украина",
	"Belarus", "Belorussiya", "Беларусь", "Белоруссия",
	"Turkiya", "Turkey", "Türkiye", "Туркия", "Турция",
	"Ozarbayjon", "Azerbaijan", "Озарбайжон", "Азербайджан",
	"Gruziya", "Georgia", "Грузия",
	"Armaniston", "Armenia", "Арманистон", "Армения",
	"Eron", "Iran", "Эрон", "Иран",
	"Iroq", "Iraq", "Ироқ", "Ирак",
	"Suriya", "Syria", "Сурия", "Сирия",
	"Saudiya Arabistoni", "Arabiston", "Saudi Arabia", "Саудия Арабистони", "Саудовская Аравия",
	"BAA", "UAE", "United Arab Emirates", "Birlashgan Arab Amirliklari", "ОАЭ",
	"Misr", "Egypt", "Миср", "Египет",
	"Isroil", "Israel", "Исроил", "Израиль",
	"Xitoy", "China", "Хитой", "Китай",
	"Yaponiya", "Japan", "Япония",
	"Koreya", "Korea", "Janubiy Koreya", "South Korea", "Корея", "Южная Корея",
	"Hindiston", "India", "Ҳиндистон", "Индия",
	"Pokiston", "Pakistan", "Покистон", "Пакистан",
	"Malayziya", "Malaysia", "Малайзия",
	"Indoneziya", "Indonesia", "Индонезия",
	"Tailand", "Thailand", "Таиланд",
	"Germaniya", "Germany", "Германия",
	"Fransiya", "France", "Франция",
	"Angliya", "England", "Britaniya", "Buyuk Britaniya", "United Kingdom", "Англия", "Великобритания",
	"Italiya", "Italy", "Италия",
	"Ispaniya", "Spain", "Испания",
	"Polsha", "Poland", "Польша",
	"Chexiya", "Czechia", "Чехия",
	"AQSH", "Amerika", "USA", "America", "United States", "АҚШ", "США", "Америка",
	"Kanada", "Canada", "Канада",
	"Avstraliya", "Australia", "Австралия",
	"Yevropa", "Europe", "Европа",
}
