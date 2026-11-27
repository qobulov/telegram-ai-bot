// Package farosat serves the Farosat Mini App, as the owners asked on
// 2026-11-27 in place of another bot's: a member's farosat in a group opens
// galaxies as it grows, and grams are bought with Telegram Stars. The page
// in public/farosat is static and asks the handler here for the member's
// farosat, the galaxies and an invoice; whose farosat it is comes only from
// the init data Telegram signs with the bot's token.
package farosat

// Galaxy is one galaxy of the collection: it opens once a member's farosat
// in the group reaches Grams. Kind is how the page draws it: spiral,
// barred, elliptical, lenticular, irregular, ring, edge or merger.
type Galaxy struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Class    string `json:"class"`
	Distance string `json:"distance"`
	Fact     string `json:"fact"`
	Grams    int    `json:"grams"`
}

// Galaxies are the collection, the nearest and best known first, each
// needing more farosat than the one before. Distances are rounded, in
// light years.
var Galaxies = []Galaxy{
	{1, "Somon yo'li", "barred", "To'siqli spiral galaktika (SBbc)", "biz shu yerdamiz", "Quyosh tizimimizning uyi: 100–400 mlrd yulduz, diametri ~100 ming yorug'lik yili.", 0},
	{2, "Katta Magellan buluti", "irregular", "Mitti noto'g'ri galaktika (SBm)", "160 ming yorug'lik yili", "Somon yo'lining eng katta yo'ldoshi; 1987-yilgi SN 1987A o'ta yangi yulduzi shu yerda chaqnagan.", 10},
	{3, "Kichik Magellan buluti", "irregular", "Mitti noto'g'ri galaktika", "200 ming yorug'lik yili", "Janubiy osmonda oddiy ko'z bilan ko'rinadi; Katta Magellan buluti bilan gaz ko'prigi orqali bog'langan.", 25},
	{4, "Andromeda (M31)", "spiral", "Spiral galaktika (SA(s)b)", "2,5 mln yorug'lik yili", "Oddiy ko'z bilan ko'rinadigan eng uzoq ob'ekt; ~4,5 mlrd yildan keyin Somon yo'li bilan to'qnashadi.", 50},
	{5, "Uchburchak galaktikasi (M33)", "spiral", "Spiral galaktika (SA(s)cd)", "2,7 mln yorug'lik yili", "Mahalliy guruhning uchinchi katta galaktikasi; yulduz paydo bo'ladigan NGC 604 bulutiga ega.", 80},
	{6, "M32", "elliptical", "Ixcham elliptik galaktika (cE2)", "2,5 mln yorug'lik yili", "Andromedaning kichik yo'ldoshi; tashqi yulduzlarini Andromeda tortib olgan bo'lishi mumkin.", 120},
	{7, "M110", "elliptical", "Mitti elliptik galaktika", "2,7 mln yorug'lik yili", "Andromedaning yana bir yo'ldoshi; ichida yosh yulduzlar izi topilgan.", 170},
	{8, "Barnard galaktikasi (NGC 6822)", "irregular", "Mitti noto'g'ri galaktika", "1,6 mln yorug'lik yili", "Edvard Barnard 1884-yilda kashf etgan; ichida yorqin gaz tumanliklari ko'p.", 230},
	{9, "Leo I", "elliptical", "Mitti sferoid galaktika", "820 ming yorug'lik yili", "Regul yulduzi yonida joylashgani uchun uni kuzatish qiyin.", 300},
	{10, "Haykaltarosh galaktikasi (NGC 253)", "barred", "To'siqli spiral (SAB(s)c)", "11 mln yorug'lik yili", "Yulduzlar portlashsimon tezlikda paydo bo'ladigan galaktika; 'Kumush tanga' deb ham ataladi.", 400},
	{11, "Sentavr A (NGC 5128)", "lenticular", "Linzasimon / elliptik (S0 pec)", "12 mln yorug'lik yili", "Kuchli radio manba; markazidagi qora tuynuk ~55 mln Quyosh massasiga teng.", 500},
	{12, "Bode galaktikasi (M81)", "spiral", "Spiral galaktika (SA(s)ab)", "11,8 mln yorug'lik yili", "Klassik 'grand-design' spiral; markazidagi qora tuynuk ~70 mln Quyosh massasiga teng.", 650},
	{13, "Sigaret galaktikasi (M82)", "edge", "Yulduz portlashli galaktika (I0)", "12 mln yorug'lik yili", "M81 bilan yaqinlashuv tufayli unda yulduzlar Somon yo'lidan ~10 marta tez paydo bo'ladi.", 800},
	{14, "Janubiy shamol tegirmoni (M83)", "barred", "To'siqli spiral (SAB(s)c)", "15 mln yorug'lik yili", "Janubiy osmonning eng yorqin spirallaridan; unda oltita o'ta yangi yulduz kuzatilgan.", 1000},
	{15, "Qora ko'z (M64)", "spiral", "Spiral galaktika (SA(rs)ab)", "17 mln yorug'lik yili", "Markazi oldidagi qora chang chizig'i ko'zga o'xshaydi; tashqi gazi teskari aylanadi.", 1250},
	{16, "Shamol tegirmoni (M101)", "spiral", "Spiral galaktika (SAB(rs)cd)", "21 mln yorug'lik yili", "Diametri Somon yo'lidan ~1,7 marta katta; 2023-yilgi SN 2023ixf shu yerda chaqnagan.", 1500},
	{17, "Girdob galaktikasi (M51)", "spiral", "Spiral galaktika (SA(s)bc)", "23 mln yorug'lik yili", "Kichik qo'shnisi NGC 5195 bilan o'zaro ta'sirda; spiral qo'llari birinchi bo'lib aynan unda ko'rilgan.", 1800},
	{18, "M106", "barred", "To'siqli spiral (SAB(s)bc)", "24 mln yorug'lik yili", "Faol yadroli galaktika; markazidagi qora tuynuk atrofida suv mazeri kuzatilgan.", 2200},
	{19, "Kit galaktikasi (NGC 4631)", "edge", "Yon tomondan ko'ringan spiral (SB(s)d)", "25 mln yorug'lik yili", "Shakli kitga o'xshaydi; yonidagi kichik galaktika 'kit bolasi' deb ataladi.", 2600},
	{20, "Kungaboqar galaktikasi (M63)", "spiral", "Spiral galaktika (SA(rs)bc)", "29 mln yorug'lik yili", "Qisqa, ko'p bo'lakli spiral qo'llari kungaboqar urug'larini eslatadi.", 3000},
	{21, "NGC 891", "edge", "Yon tomondan ko'ringan spiral (SA(s)b)", "30 mln yorug'lik yili", "Somon yo'lini yon tomondan ko'rsak, deyarli shunday ko'rinardi.", 3600},
	{22, "Sombrero galaktikasi (M104)", "lenticular", "Spiral galaktika (SA(s)a)", "31 mln yorug'lik yili", "Yorqin markazi va qalin chang halqasi shlyapaga o'xshatadi.", 4200},
	{23, "Antennalar (NGC 4038/4039)", "merger", "Qo'shilayotgan galaktikalar", "45 mln yorug'lik yili", "Ikki galaktika to'qnashib, uzun 'antenna' dumlarini hosil qilgan.", 5000},
	{24, "M77", "spiral", "Seyfert spiral galaktikasi (SA(rs)b)", "47 mln yorug'lik yili", "Yadrosi juda yorqin faol galaktika; Seyfert galaktikalarining eng mashhuri.", 6000},
	{25, "M87 (Virgo A)", "elliptical", "Gigant elliptik galaktika (E0)", "53 mln yorug'lik yili", "2019-yilda birinchi qora tuynuk surati shu yerda olingan: ~6,5 mlrd Quyosh massasi.", 7000},
	{26, "NGC 1300", "barred", "To'siqli spiral (SB(rs)bc)", "61 mln yorug'lik yili", "To'siqli spiralning darsliklardagi namunasi; to'siq ~3300 yorug'lik yili uzun.", 8500},
	{27, "Stefan beshligi", "merger", "Ixcham galaktikalar guruhi", "290 mln yorug'lik yili", "Besh galaktikadan to'rttasi o'zaro tortishib, zarb to'lqinlari hosil qilmoqda.", 10000},
	{28, "Itbaliq galaktikasi (UGC 10214)", "barred", "Buzilgan to'siqli spiral", "420 mln yorug'lik yili", "Bosqinchi galaktika tortib chiqargan dumi ~280 ming yorug'lik yili uzun.", 12000},
	{29, "Aravacha g'ildiragi (ESO 350-40)", "ring", "Halqali galaktika", "500 mln yorug'lik yili", "Kichik galaktika markazidan o'tib ketgach, suvga tosh tushgandek halqa yoyilgan.", 14500},
	{30, "Xoag ob'ekti", "ring", "Halqali galaktika", "600 mln yorug'lik yili", "Sariq markaz atrofida deyarli mukammal moviy halqa; kelib chiqishi hanuz jumboq.", 17000},
	{31, "IC 1101", "elliptical", "O'ta gigant elliptik galaktika (cD)", "~1 mlrd yorug'lik yili", "Ma'lum eng katta galaktikalardan biri: trillionlab yulduz.", 20000},
	{32, "GN-z11", "irregular", "Juda uzoq yosh galaktika", "~13,4 mlrd yil oldingi yorug'lik", "Katta portlashdan ~400 mln yil o'tib ko'ringan; Hubble topgan eng uzoq galaktikalardan.", 25000},
	{33, "JADES-GS-z14-0", "irregular", "Eng uzoq tasdiqlangan galaktikalardan", "~13,5 mlrd yil oldingi yorug'lik", "James Webb 2024-yilda topgan: Katta portlashdan ~290 mln yil o'tib nur sochgan.", 30000},
}
