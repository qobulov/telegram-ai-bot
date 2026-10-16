package main

// Fixes for what GeoNames lacks or has wrong, by GeoNames id. checkFixes
// stops the generator when one of these ids is gone from GeoNames.

// seats are the seats of every region and district whose seat GeoNames
// does not link, or links to a stale one: Arnasoy District's is
// G'oliblar, not the village Arnasoy; Tomdi's is Tomdibuloq, 80 km from
// the district's point.
var seats = map[int]int{
	1484838: 1512524, // Tashkent Region: Nurafshon (GeoNames says Amir Timur).

	// Andijan.
	1513039: 1513040, // Paxtaobod: Paxtaobod.
	1513193: 1512832, // Shahrixon: Shahrixon.
	1513240: 1513241, // Marhamat: Marhamat.
	1513270: 1513271, // Asaka: Asaka.
	1513363: 1513364, // Qo'rg'ontepa: Qo'rg'ontepa.
	1513493: 1514707, // Ulug'nor: Oqoltin.
	1513598: 1513600, // Xo'jaobod: Xo'jaobod.
	1513888: 1513010, // Izboskan: Poytug'.
	1514108: 1514109, // Jalaquduq: Jalaquduq.
	1514336: 1514337, // Bo'ston (Bo'z Tumani): Bo'z.
	1514451: 1514454, // Baliqchi: Baliqchi.
	1514584: 1513327, // Andijon District: Kuyganyor.
	1514606: 1514608, // Oltinko'l: Oltinko'l.
	1538281: 1514325, // Buloqboshi: Buloqboshi.

	// Bukhara.
	1215788: 1512423, // Vobkent: Vobkent.
	1215958: 1346531, // Termiz District (filed under Bukhara): Uchqizil.
	1216048: 1215663, // Jondor: Jondor.
	1216289: 1216290, // Romitan: Romitan.
	1217081: 1217085, // Qorako'l: Qorako'l.
	1217179: 1217180, // Kogon District: Kogon.
	1217663: 1217340, // Buxoro District: Galaosiyo.
	1217905: 1217907, // Olot: Olot.
	1346433: 1346442, // Qorovulbozor: Qorovulbozor.
	1512837: 1512838, // Shofirkon: Shofirkon.
	1513006: 1512364, // Peshku: Yangibozor.
	1538236: 1513983, // G'ijduvon: G'ijduvon.

	// Fergana.
	1512318: 1512319, // Yozyovon: Yozyovon.
	1512439: 1512320, // O'zbekiston: Yaypan.
	1512646: 1512568, // Toshloq: Toshloq.
	1512956: 1512955, // Rishton: Rishton.
	1513272: 1512502, // Uchko'prik: Uchko'prik.
	1513329: 1526979, // Quva: Quva.
	1513547: 1514382, // Beshariq: Beshariq.
	1514016: 1514178, // Dang'ara: Dang'ara.
	1514017: 1512403, // Farg'ona District: Vodil.
	1514299: 1512351, // Buvayda: Ibrat.
	1514482: 1514483, // Bag'dod: Bag'dod.
	1514616: 1514615, // Oltiariq: Oltiariq.
	1514681: 1538293, // Qo'shtepa (Oxunboboyev Tumani): Langar.
	1538294: 1513132, // Furqat: Navbahor.
	1538295: 1346571, // So'x: Ravon.

	// Khorezm.
	1512360: 1512361, // Yangibozor: Yangibozor.
	1512367: 1512368, // Yangiariq: Yangiariq.
	1512467: 1538225, // Urganch District: Qorovul.
	1512818: 1512790, // Shovot: Shovot.
	1513477: 1512978, // Qo'shko'pir: Qo'shko'pir.
	1513603: 1513604, // Xiva District: Xiva.
	1513610: 1513957, // Hazorasp: Hazorasp.
	1513635: 1513577, // Xonqa: Xonqa.
	1513961: 1513962, // Gurlan: Gurlan.
	1514485: 1514486, // Bog'ot: Bog'ot.

	// Namangan.
	1513021: 1513023, // Pop: Pop.
	1512289: 1514058, // Mingbuloq: Jomasho'y.
	1512349: 1512350, // Yangiqo'rg'on: Yangiqo'rg'on.
	1512448: 1512449, // Uychi: Uychi.
	1512500: 1512501, // Uchqo'rg'on: Uchqo'rg'on.
	1512548: 1512549, // To'raqo'rg'on: To'raqo'rg'on.
	1513144: 1513655, // Norin: Haqqulobod.
	1513156: 1512658, // Namangan District: Toshbuloq.
	1513713: 1513714, // Kosonsoy: Kosonsoy.
	1514190: 1514192, // Chust: Chust.
	1538272: 1514258, // Chortoq: Chortoq.

	// Navoiy.
	1216582: 1538233, // Qiziltepa: Qiziltepa.
	1512504: 1538231, // Uchquduq: Uchquduq.
	1512673: 1512675, // Tomdi: Tomdibuloq.
	1513082: 1538234, // Nurota: Nurota.
	1513133: 1538232, // Karmana: Karmana.
	1513135: 1514369, // Navbahor: Beshrabot.
	1513622: 1512348, // Xatirchi: Yangirabot.
	1513830: 1513831, // Konimex: Konimex.

	// Qashqadaryo.
	1215766: 1346492, // Yakkabog': Yakkabog'.
	1215855: 1215856, // Kasbi: Mug'lon.
	1216186: 1346482, // Shahrisabz District: Shahrisabz.
	1216399: 1346493, // Nishon: Yangi Nishon.
	1216474: 1346486, // Muborak: Muborak.
	1216786: 1346485, // Kitob: Kitob.
	1217005: 1346483, // Koson: Koson.
	1217013: 1346487, // Qarshi District: Beshkent.
	1217142: 1346489, // Qamashi: Qamashi.
	1217259: 1217262, // G'uzor: G'uzor.
	1217479: 1346480, // Dehqonobod: Qorashina.
	1217538: 1346491, // Chiroqchi: Chiroqchi.
	1346479: 1216490, // Mirishkor: Yangi Mirishkor.

	// Karakalpakstan.
	1512534: 1538222, // To'rtko'l: To'rtko'l.
	1512698: 1512699, // Taxtako'pir: Taxtako'pir.
	1513732: 1513733, // Qorao'zak: Qorao'zak.
	1514037: 1538223, // Ellikqal'a: Bo'ston.
	1514386: 1538221, // Beruniy: Beruniy.
	1514593: 1538224, // Amudaryo: Mang'it.
	601276:  829964,  // Shumanay: Shumanay.
	601292:  601423,  // Nukus District: Oqmang'it.
	601301:  829962,  // Mo'ynoq: Mo'ynoq.
	601309:  601306,  // Qanliko'l: Qanliko'l.
	601322:  829955,  // Qo'ng'irot: Qo'ng'irot.
	601340:  601339,  // Xo'jayli: Xo'jayli.
	601347:  601349,  // Kegeyli: Kegeyli.
	601394:  829960,  // Chimboy: Chimboy.
	601398:  7889135, // Bo'zatov: Bo'zatov.

	// Samarqand.
	1215838: 1215839, // Urgut: Urgut.
	1216079: 1216080, // Nurobod: Nurobod.
	1216266: 1346461, // Samarqand District: Gulobod.
	1216337: 1216430, // Payariq: Payariq.
	1216341: 1217362, // Pastdarg'om: Juma.
	1216354: 1512260, // Paxtachi: Ziyovuddin.
	1216429: 1217925, // Narpay: Oqtosh.
	1216987: 1513011, // Kattaqo'rg'on District: Payshanba.
	1217214: 1217215, // Ishtixon: Ishtixon.
	1217412: 1217414, // Jomboy: Jomboy.
	1217655: 1217658, // Bulung'ur: Bulung'ur.
	1217960: 1216577, // Oqdaryo: Loyish.
	1346451: 1217569, // Chelak (now in Payariq): Chelak.
	1346455: 1346454, // Go'zalkent: Go'zalkent.
	1346462: 1215975, // Toyloq: Toyloq.
	1513472: 1538239, // Qo'shrabot: Qo'shrabot.

	// Surxondaryo.
	1216113: 1216115, // Sho'rchi: Sho'rchi.
	1216154: 1216157, // Sherobod: Sherobod.
	1216243: 1216244, // Sariosiyo: Sariosiyo.
	1216571: 1215733, // Qiziriq: Sariq.
	1216685: 1216686, // Qumqo'rg'on: Qumqo'rg'on.
	1217341: 1216757, // Muzrabot: Xalqobod.
	1217391: 1217192, // Jarqo'rg'on: Jarqo'rg'on.
	1217476: 1217474, // Denov: Denov.
	1217733: 1217734, // Boysun: Boysun.
	1217872: 1217874, // Angor: Angor.
	1217894: 1217020, // Oltinsoy: Qarluq.
	1346497: 1215797, // Uzun: Uzun.
	1346534: 1217764, // Bandixon: Bandixon.
	1538254: 1513973, // Yangiyo'l District (filed under Surxondaryo): Gulbahor.

	// Tashkent region.
	1512652: 1513677, // Toshkent District: Keles.
	1512751: 1512524, // O'rta Chirchiq: Nurafshon.
	1512985: 1512986, // Piskent: Piskent.
	1513016: 1513017, // Parkent: Parkent.
	1513057: 1513567, // Qibray: Qibray.
	1513498: 1512362, // Yuqori Chirchiq: Yangibozor.
	1513860: 1513861, // Zangiota: Eshonguzar.
	1514010: 1512762, // Quyi Chirchiq: Do'stobod.
	1514214: 1514215, // Chinoz: Chinoz.
	1514327: 1514330, // Bo'ka: Bo'ka.
	1514338: 1513996, // Bo'stonliq: G'azalkent.
	1514399: 1538255, // Bekobod District: Zafar.
	1514667: 1514668, // Oqqo'rg'on: Oqqo'rg'on.
	1514687: 1513072, // Ohangaron District: Ohangaron.

	// Jizzakh.
	1215692: 1215694, // Zomin: Zomin.
	1217334: 1514008, // G'allaorol: G'allaorol.
	1217778: 1215810, // Baxmal: Usmat.
	1512277: 1512278, // Zarbdor: Zarbdor.
	1513035: 1513038, // Paxtakor: Paxtakor.
	1513067: 1512286, // Zafarobod: Zafarobod.
	1513202: 1514011, // Mirzacho'l: Gagarin.
	1514024: 1512355, // Forish: Yangiqishloq (GeoNames' Bog'don).
	1514068: 1512493, // Sharof Rashidov (Jizzax Tumani): Uchtepa.
	1514124: 1514125, // Do'stlik: Do'stlik.
	1514555: 1512298, // Arnasoy: G'oliblar.

	// Sirdaryo.
	1512404: 1512407,  // Sayxunobod: Sayxun.
	1512717: 1512770,  // Sirdaryo District: Sirdaryo.
	1513490: 1512366,  // Mirzaobod: Navro'z.
	1513618: 1513620,  // Xovos: Xovos.
	1513967: 11242381, // Guliston District: Dehqonobod.
	1514430: 1514151,  // Boyovut: Boyovut.
	1514705: 1512909,  // Oqoltin: Sardoba.

	// Districts GeoNames lacks (missingDistricts).
	tuproqqala: 1514140, // Pitnak.
	kokdala:    1216765, // Ko'kdala.
	yangiobod:  1215651, // Balandchaqir.
	sardoba:    1513041, // Paxtaobod.
}

// sameAs are places GeoNames lists twice under names too different to
// merge alone: their names go to the place they duplicate.
var sameAs = map[int]int{
	7668830:  1513493, // Ulugnar: Ulug'nor Tumani.
	7668831:  1217905, // Alat: Olot Tumani.
	1513982:  1538236, // Ghijduwon Tumani: G'ijduvon Tumani.
	7668842:  1512318, // Yazyavan: Yozyovon Tumani.
	7668820:  1512956, // Rishtan: Rishton Tumani.
	7668819:  1514482, // Bagdad: Bag'dod Tumani.
	7668818:  1514616, // Altyarik: Oltiariq Tumani.
	11611821: 1513021, // Pap District: Pop Tumani.
	8714365:  1513135, // Navbakhor Tumani: Navbahor Tumani.
	1215811:  1346479, // Usmon Yusupov Tumani, Mirishkor's old name.
	8714362:  1217142, // Kamashi District: Qamashi Tumani.
	8714363:  1217259, // Guzar District: G'uzor Tumani.
	7668833:  1346479, // Mirishkar: Mirishkor Tumani.
	7668834:  1216341, // Pastdargom: Pastdarg'om Tumani.
	9166490:  1346462, // Toyloq District: Toyloq Tumani.
	7668840:  1514430, // Bayaut: Boyovut Tumani.
	1513952:  1514068, // Sharof-Rashidow Tumani (filed under Sirdaryo): Jizzax Tumani.
	1512994:  1514140, // Pitnak: Druzhba, as GeoNames names Pitnak.
	1513092:  1538222, // Novyy Turtkul': To'rtko'l.
	1512506:  1538231, // Uchkuduk, put in the Bukhara desert: Uchquduq.
	12127991: 1538239, // Qo'shrabod: Qo'shrabot.
	1538280:  1512549, // Tŭragŭrghon Shahri: To'raqo'rg'on.
}

// demoted are seats GeoNames still calls one.
var demoted = map[int]bool{
	1514027: true, // Amir Timur, Tashkent Region's centre until Nurafshon.
}

// promoted are cities the region governs directly that GeoNames calls
// villages (PPL); they rank with district seats, so that Yangiyo'l is the
// city, not the district seated at Gulbahor.
var promoted = map[int]bool{
	1512339: true, // Yangiyo'l.
}

// regionFixes are regions GeoNames files a place under wrongly or not at
// all.
var regionFixes = map[int]string{
	1215958: "12", // Termiz District, filed under Bukhara.
	1538254: "14", // Yangiyo'l District, filed under Surxondaryo.
	1512769: "14", // Sijjak.
	1513153: "14", // Nanay.
}

// extraTowns are villages GeoNames counts nobody in that members ask
// about: a city it gives no people, and the mountain resorts.
var extraTowns = map[int]bool{
	1538230: true, // Zarafshon, Navoiy region.
	829958:  true, // Taxiatosh.
	1514256: true, // Chorvoq.
	1513570: true, // Xumson.
	1514315: true, // Burchmulla.
	1514225: true, // Chimyon (GeoNames' Kyzyldzhar).
	1512769: true, // Sijjak.
	1513153: true, // Nanay.
}

// displayNames replace the Uzbek name GeoNames has.
var displayNames = map[int]string{
	1215957: "Termiz",
	1538230: "Zarafshon",
	453752:  "Qoraqalpog'iston",
	1514140: "Pitnak",
	1512351: "Ibrat",
	1512549: "To'raqo'rg'on",
	1514681: "Qo'shtepa tumani",
	1514336: "Bo'ston tumani",
	1512404: "Sayxunobod tumani",
	1217341: "Muzrabot tumani",
	1514068: "Sharof Rashidov tumani",
	1512366: "Navro'z",
	1514256: "Chorvoq",
	1513570: "Xumson",
	1514225: "Chimyon",
	1512769: "Sijjak",
	829958:  "Taxiatosh",
}

// extraNames are spellings members use that GeoNames lacks: today's names
// of renamed districts, and the Russian names of district seats and of
// Tashkent's districts.
var extraNames = map[int][]string{
	1215957: {"Termiz", "Термиз"},
	1538230: {"Zarafshon", "Зарафшон", "Зарафшан"},
	1514140: {"Pitnak", "Питнак"},
	1514681: {"Qo'shtepa tumani", "Qo'shtepa", "Қўштепа", "Kushtepa", "Куштепа"},
	1514336: {"Bo'ston tumani", "Bo'ston", "Бўстон"},
	1512404: {"Sayxunobod tumani", "Sayxunobod", "Сайхунобод", "Сайхунабад"},
	1217341: {"Muzrabot tumani", "Muzrabot", "Музработ", "Музрабад"},
	1514068: {"Sharof Rashidov tumani", "Sharof Rashidov", "Шароф Рашидов"},
	601309:  {"Канлыкуль", "Kanlykul"},
	601398:  {"Бозатау", "Bozatau"},
	1215733: {"Сарык", "Saryk"},
	1216757: {"Халкабад", "Khalkabad"},
	1538225: {"Карауул", "Karauul"},
	1346480: {"Каршина", "Karshina"},
	1514369: {"Бешрабат", "Beshrabat"},
	1512348: {"Янгирабат", "Yangirabat"},
	1512366: {"Navro'z", "Navruz", "Навруз"},
	1514229: {"Чиланзар", "Chilanzar"},
	1513546: {"Юнусабад", "Yunusabad"},
	1536761: {"Яккасарай", "Yakkasaray"},
	1513066: {"Шайхантахур", "Shaykhantakhur", "Shayxontoxur"},
	1513269: {"Мирабад", "Mirabad"},
	1513648: {"Яшнабад", "Yashnabad"},
	1512949: {"Алмазар", "Almazar"},
	1513328: {"Мирзо Улугбек", "Mirzo Ulugbek"},
	1514256: {"Chorvoq", "Чорвоқ", "Чарвак", "Charvak"},
	1513570: {"Xumson", "Хумсон", "Хумсан"},
	1514225: {"Chimyon", "Chimgan", "Чимён", "Чимган"},
	1512769: {"Sijjak", "Сижжак", "Сиджак"},
	829958:  {"Taxiatosh", "Тахиаташ", "Takhiatash"},
}

// missingDistricts are districts made since GeoNames' last look, with
// their seats in seats, or a point of their own.
var missingDistricts = []struct {
	id       int
	name     string
	region   string
	lat, lon float64
	names    []string
}{
	{id: tuproqqala, name: "Tuproqqal'a Tumani", region: "05", names: []string{"Tuproqqal'a tumani", "Тупроққалъа", "Tuproqqala", "Тупраккала"}},
	{id: kokdala, name: "Ko'kdala Tumani", region: "08", names: []string{"Ko'kdala tumani", "Кўкдала", "Kokdala", "Кокдала"}},
	{id: yangiobod, name: "Yangiobod Tumani", region: "15", names: []string{"Yangiobod tumani"}},
	{id: sardoba, name: "Sardoba Tumani", region: "16", names: []string{"Sardoba tumani"}},
	// Tashkent's twelfth district, made in 2020 between Chilonzor and
	// Sergeli. GeoNames has neither it nor a seat: a point about its
	// middle.
	{id: yangihayot, name: "Yangihayot Tumani", region: "13", lat: 41.215, lon: 69.2, names: []string{"Yangihayot tumani", "Янгиҳаёт", "Янгихаёт", "Yangikhayot"}},
}

// Ids of missingDistricts, below GeoNames' so they never meet one.
const (
	tuproqqala = -1 - iota
	kokdala
	yangiobod
	sardoba
	yangihayot
)
