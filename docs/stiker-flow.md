# Maria: stiker, GIF va kartochkalar qanday ishlaydi

Holat: 2026-10-22, 19:15 (Toshkent vaqti). Production bazasidan faqat o'qish rejimida olingan.

## Qisqa javob: har stiker uchun AI ishlatiladimi?

**Yo'q.** AI har bir stiker uchun **faqat bir marta, import paytida** ishlatiladi. Stiker yuborilayotganda alohida AI so'rovi bo'lmaydi.

| Bosqich | AI ishlatiladimi? | Necha marta |
| --- | --- | --- |
| Paketni import qilish (`/import_stickers`, `/import_items`) | Ha: model har stikerning rasmiga qaraydi | Har stikerga **1 marta**, umrida bir marta |
| Stiker yuborishga qaror qilish | Alohida so'rov yo'q: qarorni Maria javob yozayotganda o'sha javobning o'zida qabul qiladi | 0 qo'shimcha |
| Qaysi stikerni tanlash | Yo'q: kod bazadan tanlaydi | 0 |
| GIF qidirish | Yo'q: GIPHY qidiruvi | 0 |
| Buyum kartochkasi | Tool chaqiruvi: shu javob ichida modelga yana bir murojaat | Javob boshiga +1 |
| So'kinishga video | Yo'q, lekin so'kinish **kimga qaratilganini** AI aniqlaydi | So'kinishli xabar boshiga 1 |

## 1. Import: AI har stikerga bir marta qaraydi

```
/import_stickers <havola>   (faqat egalar)
  → Telegram'dan paketdagi barcha stikerlar olinadi
  → bot_stickers jadvaliga yoziladi (labelled = false)
  → belgilanmagan har bir stiker uchun (10 tadan parallel):
       rasmi yuklab olinadi (harakatlanuvchi stikerning esa kichik rasmi)
       → AI'ga yuboriladi: "qaysi kayfiyatlarga mos, qisqa tavsif, yuborsa bo'ladimi"
       → javob: categories (3 tagacha), description, fit
       → bazaga yoziladi: labelled = true, enabled = fit va kategoriyasi bor bo'lsa
  → ~40 soniyada ulgurmaganlari keyingi buyruqda davom etadi
```

`/import_items` ham xuddi shunday ishlaydi, faqat AI'dan **buyumning nomi** so'raladi:
- nom topilsa: `categories = {mlbb_item}`, `description = "Athena's Shield"`;
- buyum bo'lmasa: `categories = {mlbb_item_none}`.

Stiker bir marta belgilangach, AI unga qaytib qaramaydi.

## 2. Kayfiyat stikeri: qaror javob ichida, tanlovni kod qiladi

```
A'zo Mariyaga yozadi
  → Maria javob beradi: bitta AI so'rovi, har qanday javobda bo'ladi
     javob shu ko'rinishda bo'lishi mumkin: "[sticker funny]"   ← qaror shu yerda
  → kod markerni ajratadi: matn bilan birga kelsa stiker tashlanadi va matn ketadi,
     yolg'iz kelsa stiker javobning o'zi bo'ladi
  → bazadan tanlanadi (SQL, AI yo'q):
       enabled va kategoriyasi mos bo'lganlar orasidan
       eng uzoq vaqt ishlatilmagani, tenglik bo'lsa tasodifiy
       shu chatga oxirgi stiker/GIF'dan 30 soniya o'tgan bo'lishi shart
  → Telegram'ga stiker yuboriladi
  → used_count++, last_used_at yangilanadi
```

## 3. GIF

```
Maria'ning javobi "[gif funny]" (kayfiyat) yoki "[gif pubg]" (mavzu)
  → GIPHY'da qidiruv (AI yo'q): kayfiyat uchun tayyor inglizcha so'zlar, mavzu uchun shu so'zning o'zi, PG reytingi
  → mavzu bo'lsa: faqat nomida mavzu so'zi bor GIF'lar qoladi ("ling" → Ling GIF'i)
  → 10 tadan biri tasodifiy tanlanib yuboriladi
  → topilmasa: kayfiyat uchun shu kayfiyatdagi stiker ketadi, mavzu uchun "Bunga mos GIF topilmadi."
  → 30 soniyalik tanaffus faqat Mariyaning o'zi tashlaydigan GIF'larga ta'sir qiladi, so'ralgan mavzu GIF'iga emas
```

MLBB qahramonining GIF'i so'ralsa, Maria GIF qidirmaydi: `mlbb` tool'i orqali qahramonning rasmiy portretini yuboradi.

## 4. MLBB buyum kartochkalari

```
"Julian buildini kartochkalari bilan tashla"
  → Maria mlbb_item_card tool'ini chaqiradi, items = [6 tagacha nom]
  → kod nom bo'yicha qidiradi (AI yo'q): to'liq nom yoki faqat bitta kartochkada uchraydigan bo'lak
  → javob: qaysi kartochkalar ketdi, qaysilari topilmadi
  → Maria bir qator yozadi, kartochkalar undan keyin stiker bo'lib ketadi (tanaffussiz)
```

## 5. Mariyaga so'kinilganda video

```
Xabarda so'kinish so'zi bor va guard yoqilgan
  → AI'dan bitta so'rov: kimga qaratilgan? BOT / PERSON / NONE
  → BOT bo'lsa:
       24 soat ichida 1-marta: 2 ta GIF'dan biri tasodifiy (AI yo'q)
       2-marta: 15 daqiqa mute, xabar va yana bitta GIF
       admin bo'lsa yoki mute qilib bo'lmasa: jim
```

## Bazadagi holat

**Paketlar:**

| Paket | Jami | Belgilangan | Kayfiyat uchun yoqilgan | Buyum kartochkasi | Necha marta ishlatilgan |
| --- | --- | --- | --- | --- | --- |
| xaa_by_qstickbot | 120 | 120 | 116 | 0 | 18 |
| BS_INFO_by_fStikBot | 57 | 57 | 0 | 55 | 0* |

\* Buyum kartochkalari `used_count`ni oshirmaydi. Bugun javoblarda kamida 4 marta yuborilgan.

**Kayfiyatlar bo'yicha yoqilgan stikerlar** (bitta stiker bir nechta kayfiyatga tushishi mumkin):

| funny | sarcasm | cool | shock | flirting | sad | wholesome | facepalm | angry | celebration | jealousy | comfort |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 112 | 78 | 33 | 21 | 14 | 12 | 7 | 6 | 6 | 4 | 1 | 1 |

`jealousy` va `comfort` uchun bittadan stiker bor, shuning uchun bu kayfiyatlarda doim bir xil stiker chiqadi. Ko'proq xilma-xillik kerak bo'lsa, shunday stikerlari bor paket import qilish kerak.

**Kunlar bo'yicha yuborilganlar** (guruhlardagi Mariya javoblari):

| Kun | Javoblar | Stiker | GIF | Buyum kartochkasi | So'kinish GIF'i |
| --- | --- | --- | --- | --- | --- |
| 10-21 | 558 | 15 | 6 | 0 | 0 |
| 10-22 | 285 | 4 | 23 | 4 | 0 |

So'kinish GIF'lari deploy qilinganidan beri hali birorta ham yuborilmagan: guard yoqilgan guruhlarda Mariyaga hech kim so'kinmagan.

## AI xarajati qayerda

- **Har bir javob:** 1 ta AI so'rovi, tool chaqirilsa har chaqiruv uchun +1. Stiker va GIF bunga qo'shimcha xarajat qo'shmaydi.
- **Import:** har stiker uchun 1 marta. Yuqoridagi ikki paket uchun jami 120 + 57 = 177 so'rov, bir martalik. BS_INFO paketiga ikki marta qaralgan: avval kayfiyat, keyin buyum nomi uchun, ya'ni yana +57.
- **Guard:** so'kinish so'zi bor har bir xabar uchun 1 ta qisqa so'rov ("kimga?").
