package service

import (
	"cmp"
	"fmt"
	"regexp"
	"strings"
)

// modRefusals are the moderate tool's refusals, written for the model, in
// the words a group is told when a command is refused: the first whose
// pattern matches. A pattern's groups fill the text's %s in order.
var modRefusals = []struct {
	pattern *regexp.Regexp
	text    string
}{
	{regexp.MustCompile(`^that admin was made by another admin`), "Bu adminni men emas, boshqa admin tayinlagan: Telegram uning unvoni va huquqlarini faqat o'sha admin yoki guruh egasiga o'zgartirishga ruxsat beradi. Men qilishim uchun uni adminlikdan olib, men orqali qayta admin qiling."},
	{regexp.MustCompile(`^that member is not an admin I can change`), "U admin emas, men faqat o'zim tayinlagan adminlarni o'zgartira olaman."},
	{regexp.MustCompile(`^I can't do that to admins`), "Buni adminlarga, bot egalariga, menga yoki o'zingizga qila olmayman."},
	{regexp.MustCompile(`^reply to the message of the member you mean`), "Kimga? A'zoning xabariga reply qiling yoki @username bilan yozing."},
	{regexp.MustCompile(`^more than one member is tagged`), "Bir nechta a'zo belgilangan: bittasini @ bilan yozing yoki uning xabariga reply qiling."},
	{regexp.MustCompile(`^I have not seen that @username`), "Bu @username'ni guruhda ko'rmaganman: uning xabariga reply qilib yozing."},
	{regexp.MustCompile(`^Telegram did not confirm that the tagged account`), "Telegram bu akkaunt guruhda ekanini tasdiqlamadi: uning xabariga reply qilib yozing."},
	{regexp.MustCompile(`^their own message does not name that member`), "Bu ismli a'zoni topmadim: @username bilan yozing yoki uning xabariga reply qiling."},
	{regexp.MustCompile(`^more than one member here goes by that name`), "Bu ismda bir nechta a'zo bor: @ bilan yozing yoki xabariga reply qiling."},
	{regexp.MustCompile(`^only a person in a group`), "Bu faqat guruhda ishlaydi."},
	{regexp.MustCompile(`^Telegram did not answer`), "Telegram javob bermadi, bajarilmagan bo'lishi mumkin: birozdan keyin qayta urinib ko'ring."},
	{regexp.MustCompile(`^that message was posted on behalf of a channel`), "Bu xabar kanal nomidan yozilgan: uni faqat o'chirish, qadash yoki kanalni bloklash mumkin."},
	{regexp.MustCompile(`^only admins of this group who have the right to (.+), or my owners`), "Buni faqat «%s» huquqi bor adminlar yoki yetarli darajali moderatorlar so'ray oladi."},
	{regexp.MustCompile(`^I don't have the right to (.+) in this group`), "Menda «%s» huquqi yo'q: guruh adminlari menga bu huquqni bersin."},
	{regexp.MustCompile(`^their rank is (\d+), and this needs rank (\d+)`), "Darajangiz %s, buning uchun %s-daraja kerak."},
	{regexp.MustCompile(`^that member's rank is (\d+), not lower than the asker's (\d+)`), "Uning darajasi %s, sizniki %s: o'zingizdan past darajadagilarga qila olasiz."},
	{regexp.MustCompile(`^that's (\d+) actions in (\d+) minutes`), "%s daqiqada %s ta amal bo'ldi, biroz kuting."},
	{regexp.MustCompile(`has at most (\d+) characters and no emoji`), "Teg ko'pi bilan %s belgi bo'ladi, emoji'siz."},
	{regexp.MustCompile(`an admin title has at most (\d+) characters`), "Unvon ko'pi bilan %s belgi bo'ladi."},
}

// rightNames are the rights the tool names, in Uzbek.
var rightNames = map[string]string{
	"delete messages": "xabarlarni o'chirish", "ban and mute members": "a'zolarni cheklash",
	"pin messages": "xabarlarni qadash", "change the group's info": "guruh ma'lumotlarini o'zgartirish",
	"invite users": "a'zo qo'shish", "add new admins": "admin qo'shish", "manage topics": "mavzularni boshqarish",
	"manage members' tags": "teglarni boshqarish",
}

// modRefusalText is a tool refusal as the group is told it; one not known
// is said as could not be done.
func modRefusalText(message string) string {
	for _, r := range modRefusals {
		m := r.pattern.FindStringSubmatch(message)
		if m == nil {
			continue
		}
		args := make([]any, 0, len(m)-1)
		for _, g := range m[1:] {
			args = append(args, cmp.Or(rightNames[g], g))
		}
		if strings.HasPrefix(r.pattern.String(), "^that's") && len(args) == 2 {
			args[0], args[1] = args[1], args[0]
		}
		return fmt.Sprintf(r.text, args...)
	}
	return ""
}
