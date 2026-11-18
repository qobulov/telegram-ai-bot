package uzscript

import "testing"

func TestLatinize(t *testing.T) {
	for cyrillic, latin := range map[string]string{
		"Алишер Навоий":   "Alisher Navoiy",
		"Ўзбекистон":      "Oʻzbekiston",
		"Ғафур Ғулом":     "Gʻafur Gʻulom",
		"Қарши":           "Qarshi",
		"Ҳамза":           "Hamza",
		"Шаҳрисабз":       "Shahrisabz",
		"Ер ва поезд":     "Yer va poyezd",
		"Чирчиқ":          "Chirchiq",
		"Юлдуз ёки Ялта":  "Yulduz yoki Yalta",
		"Амир Temur 1336": "Amir Temur 1336",
	} {
		if got := Latin(cyrillic); got != latin {
			t.Errorf("Latin(%q) = %q, want %q", cyrillic, got, latin)
		}
	}
}

func TestIsUzbekCyrillic(t *testing.T) {
	for text, want := range map[string]bool{
		"Салом, бугун ҳаво жуда яхши":                  true,
		"Привет, как дела у тебя?":                     false,
		"Salom, bugun havo yaxshi":                     false,
		"Ўзбекистонда Mobile Legends жуда машҳур ўйин": true,
		"Bu Ўзбекистон haqida gap edi":                 false,
	} {
		if got := IsUzbekCyrillic(text); got != want {
			t.Errorf("IsUzbekCyrillic(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestCyrillic(t *testing.T) {
	for latin, cyrillic := range map[string]string{
		"Shahrisabz va Chirchiq":           "Шаҳрисабз ва Чирчиқ",
		"O'zbekiston, G'afur":              "Ўзбекистон, Ғафур",
		"Yer, yoki Yulduz, Yalta":          "Ер, ёки Юлдуз, Ялта",
		"Ega emas":                         "Эга эмас",
		"huquqi yo'q":                      "ҳуқуқи йўқ",
		"ma'lumot":                         "маълумот",
		"«mut qil» deb yozing yoki /mute":  "«мут қил» деб ёзинг ёки /mute",
		"@some_user xabariga reply qiling": "@some_user хабарига реплай қилинг",
		"https://t.me/x ga qarang":         "https://t.me/x га қаранг",
	} {
		if got := Cyrillic(latin); got != cyrillic {
			t.Errorf("Cyrillic(%q) = %q, want %q", latin, got, cyrillic)
		}
	}
}
