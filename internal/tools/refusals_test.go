package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// The tool's refusals are told to the chat in Uzbek, with what they name
// filled in.
func TestRefusalLinesInUzbek(t *testing.T) {
	for message, want := range map[string]string{
		"that admin was made by another admin, not by me, and Telegram lets only the one who made them":                                     "boshqa admin tayinlagan",
		"I can't do that to admins, my owners, myself, Telegram's service accounts or the person asking":                                    "adminlarga",
		"only admins of this group who have the right to add new admins, or my owners, can ask me to do that; the person asking is neither": "«admin qo'shish»",
		"I don't have the right to ban and mute members in this group; its admins can give it to me":                                        "«a'zolarni cheklash»",
		"their rank is 2, and this needs rank 5 here":                                                                                       "Darajangiz 2, buning uchun 5-daraja",
		"that member's rank is 3, not lower than the asker's 3, so nothing was done":                                                        "Uning darajasi 3",
		"that's 20 actions in 10 minutes; wait a bit":                                                                                       "10 daqiqada 20 ta",
		"I have not seen that @username in this group, so I can't tell who it is; reply":                                                    "@username'ni guruhda ko'rmaganman",
		"that member is an admin of this group (an admin's title, such as a custom one, shows it), and Telegram":                            "Telegram botga adminni",
		"that member is the group's creator: Telegram does not let a bot":                                                                   "guruh egasi",
		"that message of mine is not about a member I acted on lately; reply":                                                               "kim haqida ekanini topmadim",
		"that message of mine names more than one member; tag":                                                                              "bir nechta a'zo",
		"a member's tag has at most 16 characters and no emoji":                                                                             "16 belgi",
		notAsked:                        "buyruq ko'rinmadi",
		unnamedMember:                   "Kimni?",
		noReplyTarget:                   "Kimni?",
		fmt.Sprintf(nothingToPurge, 60): "Oxirgi 60 daqiqada",
		internalFailure:                 "Xatolik chiqdi",
	} {
		if got := RefusalLine(message, LanguageUzbek, ""); !strings.Contains(got, want) {
			t.Errorf("%q = %q, want %q in it", message, got, want)
		}
	}
	if got := RefusalLine("something new", LanguageUzbek, ""); got != "" {
		t.Errorf("an unknown refusal = %q", got)
	}
}

// A refusal is said in the asker's language and script.
func TestRefusalLineLanguages(t *testing.T) {
	message := "I don't have the right to delete messages in this group; its admins can give it to me"
	for language, want := range map[string]string{
		LanguageUzbek:         "Menda bu guruhda «xabarlarni o'chirish» huquqi yo'q",
		LanguageUzbekCyrillic: "Менда бу гуруҳда «хабарларни ўчириш» ҳуқуқи йўқ",
		LanguageRussian:       "У меня нет права «удалять сообщения»",
		LanguageEnglish:       "I don't have the right to delete messages",
		"":                    "Menda bu guruhda",
	} {
		if got := RefusalLine(message, language, ""); !strings.HasPrefix(got, want) {
			t.Errorf("%s: %q, want it to start %q", language, got, want)
		}
	}
	if got := RefusalLine(notAsked, LanguageUzbekCyrillic, ""); !strings.Contains(got, "/mute, /ban") {
		t.Errorf("commands are written in Cyrillic: %q", got)
	}
}

// Telegram's own refusals are told plainly, never as its error codes, and
// one about rights names the right the action needs.
func TestRefusalLinesOfTelegram(t *testing.T) {
	for message, want := range map[string]string{
		"Telegram refused: Bad Request: CHAT_ADMIN_REQUIRED":                      "«xabarlarni o'chirish» huquqi yo'q: adminlar menga shu huquqni bersin",
		"Telegram refused: Bad Request: not enough rights to restrict/unrestrict": "«xabarlarni o'chirish» huquqi yo'q",
		"Telegram refused: Bad Request: USER_NOT_PARTICIPANT":                     "U bu guruhda emas",
		"Telegram refused: Bad Request: message can't be deleted":                 "o'chirishga ruxsat bermadi",
		"Telegram refused: Bad Request: message to delete not found":              "xabar topilmadi",
		"Telegram refused: Bad Request: user is an administrator of the chat":     "U admin",
		"Telegram refused: Bad Request: something odd":                            "Telegram buni rad etdi",
	} {
		got := RefusalLine(message, LanguageUzbek, "delete messages")
		if !strings.Contains(got, want) || strings.Contains(got, "_") {
			t.Errorf("%q = %q, want %q in it", message, got, want)
		}
	}
}

// A refusal reaches the model with the line to send and what to do with
// it; an argument the model got wrong has none, so that it calls again.
func TestModerateRefusalCarriesSaid(t *testing.T) {
	env := newModerationEnv(t)
	russian := WithTurn(context.Background(), Caller{UserID: userID, ChatID: groupA, Human: true, UpdateID: 100, MessageID: 50, Target: bekTarget(),
		Words: "Мари, что скажешь про него?", Language: LanguageRussian}, &Sink{})

	got := env.moderate(russian, `{"action":"mute"}`)
	if got.OK || !strings.HasPrefix(got.Error.Said, "В вашем сообщении нет команды") || got.Error.Note != refusalNote {
		t.Errorf("refusal = %+v, want the Russian line and the note", got)
	}
	if !strings.Contains(got.Error.Message, "does not ask for this") {
		t.Errorf("the model still has the reason: %+v", got)
	}

	got = env.moderate(asking(101, nil), `{"action":"lock","kind":"everything"}`)
	if got.OK || got.Error.Said != "" || got.Error.Note != "" {
		t.Errorf("a wrong argument = %+v, want no said", got)
	}

	env.telegram.failing = "restrictChatMember"
	got = env.moderate(asking(102, bekTarget()), `{"action":"mute"}`)
	if got.OK || !strings.Contains(got.Error.Said, "Telegram buni rad etdi") {
		t.Errorf("Telegram's refusal = %+v", got)
	}
}
