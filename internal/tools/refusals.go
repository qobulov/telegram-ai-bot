package tools

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"telegram-ai-bot/internal/uzscript"
)

// The languages a Caller may write in, as refusals are said to them.
const (
	LanguageUzbek         = "uz"
	LanguageUzbekCyrillic = "uz-Cyrl"
	LanguageRussian       = "ru"
	LanguageEnglish       = "en"
)

// SaidError is a failure with the line the chat is told about it, ready in
// the asker's language: the model sends it as it is rather than making up a
// reason of its own, or saying it was done.
type SaidError struct {
	Err  error
	Said string
}

func (e *SaidError) Error() string { return e.Err.Error() }

func (e *SaidError) Unwrap() error { return e.Err }

// refusalNote tells the model what to do with a refusal's said.
const refusalNote = "Nothing was done. Your whole reply is the line in said, exactly as it is: do not reword it, add to it, guess another reason, or say anything was done."

// refusal is one of moderate's refusals, as the chat is told it. In the
// texts, %s takes the pattern's groups in order (%[2]s and the like where
// the order differs), and {right} the admin right the action needs.
type refusal struct {
	pattern    *regexp.Regexp
	uz, ru, en string
}

// refusals are moderate's refusals and failures, the first whose pattern
// matches the message the model is given. A message not here, such as an
// argument the model got wrong, has no said: the model is to correct its
// call.
var refusals = []refusal{
	{regexp.MustCompile(`^their own message does not ask for this`),
		"So'zingizda buyruq ko'rinmadi, hech narsa qilinmadi: «mut qil», «ban qil», «o'chir» deb yozing yoki /mute, /ban buyrug'ini ishlating.",
		"В вашем сообщении нет команды, ничего не сделано: напишите «замуть», «забань», «удали» или используйте /mute, /ban.",
		"I didn't see an order in your message, so nothing was done: write \"mute\", \"ban\" or \"delete\", or use /mute, /ban."},
	{regexp.MustCompile(`^that admin was made by another admin`),
		"Bu adminni men emas, boshqa admin tayinlagan: Telegram uning unvoni va huquqlarini faqat o'sha admin yoki guruh egasiga o'zgartirishga ruxsat beradi. Men qilishim uchun uni adminlikdan olib, men orqali qayta admin qiling.",
		"Этого админа назначил не я, а другой админ: Telegram разрешает менять его звание и права только тому админу или владельцу группы. Снимите его и назначьте снова через меня.",
		"Another admin made this one admin, not me: Telegram lets only that admin or the group's owner change their title or rights. Remove them and make them admin again through me."},
	{regexp.MustCompile(`^that member is not an admin I can change`),
		"U admin emas, men faqat o'zim tayinlagan adminlarni o'zgartira olaman.",
		"Он не админ: я могу менять только админов, которых назначила сама.",
		"They are not an admin: I can only change admins I made myself."},
	{regexp.MustCompile(`^that member is the group's creator`),
		"U guruh egasi: Telegram botga guruh egasini mute, ban, kik qilish yoki cheklashga ruxsat bermaydi.",
		"Это владелец группы: Telegram не даёт боту мутить, банить, кикать или ограничивать владельца.",
		"That's the group's owner: Telegram doesn't let a bot mute, ban, kick or restrict the owner."},
	{regexp.MustCompile(`^that member is an admin of this group`),
		"U shu guruh admini (unvoni shuni ko'rsatadi): Telegram botga adminni mute, ban, kik qilish yoki cheklashga ruxsat bermaydi, shuning uchun adminlarga ham, xabarlariga ham tegmayman. Avval guruh egasi yoki uni admin qilgan kishi adminlikni olib tashlasin.",
		"Это админ группы (его звание это показывает): Telegram не даёт боту мутить, банить, кикать или ограничивать админов, поэтому я не трогаю ни их, ни их сообщения. Сначала владелец или тот, кто его назначил, должен снять админку.",
		"That's an admin of this group (their title shows it): Telegram doesn't let a bot mute, ban, kick or restrict an admin, so I leave admins and their messages alone. The owner, or whoever made them admin, must remove their adminship first."},
	{regexp.MustCompile(`^I can't do that to admins`),
		"Buni adminlarga, bot egalariga, menga yoki o'zingizga qila olmayman.",
		"Этого я не могу сделать с админами, моими владельцами, собой или с вами.",
		"I can't do that to admins, my owners, myself or you."},
	{regexp.MustCompile(`^that message of mine is not about a member`),
		"Bu xabarim kim haqida ekanini topmadim: a'zoning o'z xabariga reply qiling yoki ismini yozing.",
		"Не поняла, о ком это моё сообщение: ответьте на сообщение самого участника или напишите его имя.",
		"I can't tell who that message of mine is about: reply to the member's own message or write their name."},
	{regexp.MustCompile(`^that message of mine names more than one member`),
		"Bu xabarimda bir nechta a'zo bor: keragini @ bilan yozing yoki uning o'z xabariga reply qiling.",
		"В этом моём сообщении несколько участников: укажите нужного через @ или ответьте на его сообщение.",
		"That message of mine names several members: tag the one you mean with @ or reply to their own message."},
	{regexp.MustCompile(`^reply to the message of the member you mean`),
		"Kimni? A'zoning xabariga reply qiling yoki @username bilan yozing.",
		"Кого? Ответьте на сообщение участника или напишите его @username.",
		"Who? Reply to the member's message or write their @username."},
	{regexp.MustCompile(`^more than one member is tagged`),
		"Bir nechta a'zo belgilangan: bittasini @ bilan yozing yoki uning xabariga reply qiling.",
		"Отмечено несколько участников: укажите одного через @ или ответьте на его сообщение.",
		"Several members are tagged: tag just one with @ or reply to their message."},
	{regexp.MustCompile(`^I have not seen that @username`),
		"Bu @username'ni guruhda ko'rmaganman: uning xabariga reply qilib yozing.",
		"Я не видела этот @username в группе: ответьте на его сообщение.",
		"I haven't seen that @username in this group: reply to one of their messages instead."},
	{regexp.MustCompile(`^Telegram did not confirm that the tagged account`),
		"Telegram bu akkaunt guruhda ekanini tasdiqlamadi: uning xabariga reply qilib yozing.",
		"Telegram не подтвердил, что этот аккаунт в группе: ответьте на его сообщение.",
		"Telegram didn't confirm that account is in this group: reply to one of their messages instead."},
	{regexp.MustCompile(`^their own message does not name that member`),
		"Kimni? Bu ismli a'zoni topmadim: xabariga reply qiling yoki @username bilan yozing.",
		"Кого? Не нашла участника с таким именем: ответьте на его сообщение или напишите @username.",
		"Who? I found no member by that name: reply to their message or write their @username."},
	{regexp.MustCompile(`^more than one member here goes by that name`),
		"Bu ismda bir nechta a'zo bor: @ bilan yozing yoki xabariga reply qiling.",
		"С таким именем здесь несколько участников: укажите через @ или ответьте на сообщение.",
		"Several members here go by that name: tag the one with @ or reply to their message."},
	{regexp.MustCompile(`^only a person in a group`),
		"Bu faqat guruhda ishlaydi.",
		"Это работает только в группе.",
		"This works only in a group."},
	{regexp.MustCompile(`^Telegram did not answer`),
		"Telegram javob bermadi, bajarilmagan bo'lishi mumkin: birozdan keyin qayta urinib ko'ring.",
		"Telegram не ответил, возможно, ничего не сделано: попробуйте чуть позже.",
		"Telegram didn't answer, so it may not have been done: try again in a bit."},
	{regexp.MustCompile(`^could not ask Telegram who may do this`),
		"Telegram'dan kim nima qila olishini bilib bo'lmadi, hech narsa qilinmadi: birozdan keyin qayta urinib ko'ring.",
		"Не удалось узнать у Telegram, у кого какие права, ничего не сделано: попробуйте чуть позже.",
		"I couldn't get Telegram to say who may do this, so nothing was done: try again in a bit."},
	{regexp.MustCompile(`^that message was posted on behalf of a channel`),
		"Bu xabar kanal nomidan yozilgan: uni faqat o'chirish, qadash yoki kanalni bloklash mumkin.",
		"Это сообщение от имени канала: его можно только удалить, закрепить или забанить канал.",
		"That message was posted as a channel: it can only be deleted or pinned, or the channel banned."},
	{regexp.MustCompile(`^only admins of this group who have the right to (.+), or my owners`),
		"Buni faqat «%s» huquqi bor adminlar yoki yetarli darajali moderatorlar so'ray oladi.",
		"Это могут просить только админы с правом «%s» или модераторы с нужным рангом.",
		"Only admins with the right to %s, or moderators of a high enough rank, can ask for this."},
	{regexp.MustCompile(`^I don't have the right to (.+) in this group`),
		"Menda bu guruhda «%s» huquqi yo'q: adminlar menga shu huquqni bersin.",
		"У меня нет права «%s» в этой группе: пусть админы мне его дадут.",
		"I don't have the right to %s in this group: its admins need to give it to me."},
	{regexp.MustCompile(`^their rank is (\d+), and this needs rank (\d+)`),
		"Darajangiz %s, buning uchun %s-daraja kerak.",
		"Ваш ранг %s, а для этого нужен ранг %s.",
		"Your rank is %s, and this needs rank %s."},
	{regexp.MustCompile(`^that member's rank is (\d+), not lower than the asker's (\d+)`),
		"Uning darajasi %s, sizniki %s: o'zingizdan past darajadagilarga qila olasiz.",
		"Его ранг %s, ваш %s: можно только тем, у кого ранг ниже вашего.",
		"Their rank is %s and yours is %s: you can only do this to lower ranks."},
	{regexp.MustCompile(`^that's (\d+) actions in (\d+) minutes`),
		"%[2]s daqiqada %[1]s ta amal bo'ldi, biroz kuting.",
		"За %[2]s минут уже %[1]s действий, подождите немного.",
		"That's %[1]s actions in %[2]s minutes, wait a bit."},
	{regexp.MustCompile(`has at most (\d+) characters and no emoji`),
		"Teg ko'pi bilan %s belgi bo'ladi, emoji'siz.",
		"Тег — не больше %s символов, без эмодзи.",
		"A tag has at most %s characters and no emoji."},
	{regexp.MustCompile(`an admin title has at most (\d+) characters`),
		"Unvon ko'pi bilan %s belgi bo'ladi.",
		"Звание — не больше %s символов.",
		"An admin title has at most %s characters."},
	{regexp.MustCompile(`^there is no photo in the message`),
		"Xabarda rasm yo'q: rasmni so'rov bilan birga yuboring yoki rasmga reply qiling.",
		"В сообщении нет фото: пришлите фото вместе с просьбой или ответьте на фото.",
		"There's no photo in the message: send it with the request, or reply to one."},
	{regexp.MustCompile(`^there is no sticker pack to set`),
		"Qaysi to'plam? Kerakli to'plamdagi stikerga reply qilib yozing.",
		"Какой набор? Ответьте на стикер из нужного набора.",
		"Which pack? Reply to a sticker from the pack you want."},
	{regexp.MustCompile(`^Telegram does not let this group have a sticker pack`),
		"Telegram bu guruhga stiker to'plami qo'yishga ruxsat bermaydi: bu faqat yetarlicha katta superguruhlarda bor.",
		"Telegram не даёт этой группе стикерпак: он есть только в достаточно больших супергруппах.",
		"Telegram doesn't let this group have a sticker pack: only big enough supergroups get one."},
	{regexp.MustCompile(`^I keep no log of this group's messages`),
		"Bu guruh xabarlarini saqlamayman, uning xabarlarini topa olmayman: o'chirish uchun har biriga reply qiling.",
		"Я не храню сообщения этой группы и не найду его сообщения: ответьте на каждое, которое нужно удалить.",
		"I keep no log of this group, so I can't find their messages: reply to each one to delete."},
	{regexp.MustCompile(`^I have no message of theirs from the last (\d+) minutes`),
		"Oxirgi %s daqiqada uning xabari menda yo'q, hech narsa o'chmadi.",
		"За последние %s минут у меня нет его сообщений, ничего не удалено.",
		"I have no message of theirs from the last %s minutes, so nothing was deleted."},
	{regexp.MustCompile(`^warnings are not kept here|^the bans are not kept here`),
		"Bu yerda bu ishlamaydi, hech narsa qilinmadi.",
		"Здесь это не работает, ничего не сделано.",
		"That doesn't work here, so nothing was done."},
	{regexp.MustCompile(`^Telegram does not let a link both admit a number`),
		"Telegram havolaga ham odam soni chegarasini, ham admin tasdig'ini birga qo'yishga ruxsat bermaydi: qaysi biri kerak?",
		"Telegram не даёт ссылке сразу и лимит людей, и одобрение админами: что из этого нужно?",
		"Telegram doesn't let a link both cap how many join and ask admins to approve each: which one do you want?"},
	{regexp.MustCompile(`^I have made no invite link in this group|^that is not an invite link I made`),
		"Bu havolani men yaratmaganman: faqat o'zim yaratgan havolalarni bekor qila yoki o'zgartira olaman.",
		"Эту ссылку создала не я: отменять и менять могу только свои ссылки.",
		"I didn't make that link: I can only revoke or change links I made myself."},
	{regexp.MustCompile(`^nothing I did in this group can be put back`),
		"Qaytaradigan narsa yo'q: o'chirilgan xabar esa qaytmaydi.",
		"Нечего возвращать, а удалённое сообщение не вернуть.",
		"There's nothing I can put back, and a deleted message never comes back."},
	{regexp.MustCompile(`^they were removed, but the ban that removed them could not be lifted`),
		"Guruhdan chiqdi, lekin bloki olinmadi, qaytib kira olmaydi: /unban qiling.",
		"Удалён(а) из группы, но бан не снялся, вернуться не сможет: сделайте /unban.",
		"They're out of the group, but the ban couldn't be lifted, so they can't come back: use /unban."},
	// Telegram's own refusals, by its words.
	{regexp.MustCompile(`(?i)^Telegram refused: .*(CHAT_ADMIN_REQUIRED|not enough rights|need administrator rights|have no rights|RIGHT_FORBIDDEN|bot is not a member)`),
		"Menda bu guruhda «{right}» huquqi yo'q: adminlar menga shu huquqni bersin.",
		"У меня нет права «{right}» в этой группе: пусть админы мне его дадут.",
		"I don't have the right to {right} in this group: its admins need to give it to me."},
	{regexp.MustCompile(`(?i)^Telegram refused: .*(user is an administrator|USER_ADMIN_INVALID|can't remove chat owner|can't restrict self|chat owner)`),
		"U admin: Telegram botga adminni cheklashga ruxsat bermaydi.",
		"Это админ: Telegram не даёт боту ограничивать админов.",
		"They're an admin: Telegram doesn't let a bot restrict an admin."},
	{regexp.MustCompile(`(?i)^Telegram refused: .*(USER_NOT_PARTICIPANT|user not found|PARTICIPANT_ID_INVALID|member not found)`),
		"U bu guruhda emas, hech narsa qilinmadi.",
		"Его нет в этой группе, ничего не сделано.",
		"They're not in this group, so nothing was done."},
	{regexp.MustCompile(`(?i)^Telegram refused: .*(message can't be deleted|MESSAGE_DELETE_FORBIDDEN)`),
		"Telegram bu xabarni o'chirishga ruxsat bermadi.",
		"Telegram не дал удалить это сообщение.",
		"Telegram wouldn't let me delete that message."},
	{regexp.MustCompile(`(?i)^Telegram refused: .*(not found|MESSAGE_ID_INVALID)`),
		"Bu xabar topilmadi, allaqachon o'chgan bo'lishi mumkin.",
		"Сообщение не найдено, возможно, его уже удалили.",
		"That message wasn't found; it may be gone already."},
	{regexp.MustCompile(`^Telegram refused`),
		"Telegram buni rad etdi, hech narsa o'zgarmadi.",
		"Telegram отказал, ничего не изменилось.",
		"Telegram refused, so nothing changed."},
	{regexp.MustCompile(`^something went wrong on my side`),
		"Xatolik chiqdi, hech narsa qilinmadi: birozdan keyin qayta urinib ko'ring.",
		"Что-то пошло не так, ничего не сделано: попробуйте чуть позже.",
		"Something went wrong, so nothing was done: try again in a bit."},
}

// rightNames name the rights rightWords does, in Uzbek and Russian.
var rightNames = map[string][2]string{
	"delete messages":         {"xabarlarni o'chirish", "удалять сообщения"},
	"ban and mute members":    {"a'zolarni cheklash", "ограничивать участников"},
	"pin messages":            {"xabarlarni qadash", "закреплять сообщения"},
	"change the group's info": {"guruh ma'lumotlarini o'zgartirish", "менять данные группы"},
	"invite users":            {"a'zo qo'shish", "приглашать участников"},
	"add new admins":          {"admin qo'shish", "назначать админов"},
	"manage topics":           {"mavzularni boshqarish", "управлять темами"},
	"manage members' tags":    {"teglarni boshqarish", "управлять тегами"},
}

// RefusalLine is a refusal's message, as moderate gives it to the model, in
// the words the chat is told, in language (Uzbek when it is none of the
// Language constants); right is the right the action needs, as rightWords
// names it, for a refusal of Telegram's that does not say. It is "" for a
// message not among the refusals.
func RefusalLine(message, language, right string) string {
	for _, r := range refusals {
		groups := r.pattern.FindStringSubmatch(message)
		if groups == nil {
			continue
		}
		text, args := r.uz, make([]any, 0, len(groups)-1)
		switch language {
		case LanguageRussian:
			text = r.ru
		case LanguageEnglish:
			text = r.en
		}
		if !strings.Contains(text, "%") {
			// A group only told the refusals apart.
			groups = groups[:1]
		}
		for _, g := range groups[1:] {
			args = append(args, rightIn(g, language))
		}
		if len(args) > 0 {
			text = fmt.Sprintf(text, args...)
		}
		text = strings.ReplaceAll(text, "{right}", rightIn(cmp.Or(right, "ban and mute members"), language))
		if language == LanguageUzbekCyrillic {
			text = uzscript.Cyrillic(text)
		}
		return text
	}
	return ""
}

// rightIn is a right rightWords names, in language; anything else as it is.
func rightIn(right, language string) string {
	names, known := rightNames[right]
	switch {
	case !known || language == LanguageEnglish:
		return right
	case language == LanguageRussian:
		return names[1]
	}
	return names[0]
}

// withRefusalLine is err with its line for the chat, when it has one.
func withRefusalLine(err error, language, right string) error {
	var said *SaidError
	if errors.As(err, &said) {
		return err
	}
	line := RefusalLine(err.Error(), language, right)
	if line == "" {
		return err
	}
	return &SaidError{Err: err, Said: line}
}

// actionOf is the action moderate's arguments ask for, or "".
func actionOf(raw json.RawMessage) string {
	var args struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(raw, &args)
	return args.Action
}
