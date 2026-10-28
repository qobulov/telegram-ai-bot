package app

import (
	"context"
	"log/slog"
	"sync"

	"telegram-ai-bot/internal/telegram"
)

// profileText is the bot's bio (the short description on its profile page)
// and description (shown in an empty chat with it) in one language.
type profileText struct {
	bio, description string
}

// profileTexts are kept here, beside the /help they summarise, so that the
// profile does not go on promising what the bot no longer does, as it did
// presentations. "" is for every language without its own.
var profileTexts = map[string]profileText{
	"": {
		bio: "Maria — AI suhbatdosh: savollar, MLBB, valyuta kursi, ob-havo, musiqa, kino va guruh moderatsiyasi.",
		description: "Salom! Men Maria — o'zbek, rus va ingliz tilida gaplashadigan AI suhbatdosh.\n\n" +
			"• Savollarga javob beraman; rasm, PDF va Word fayllarni o'qiyman\n" +
			"• Valyuta kursi, ob-havo, namoz vaqtlari, yangiliklarni topaman\n" +
			"• Mobile Legends: qahramonlar, build, skin, counter\n" +
			"• Qo'shiq, kino va video qidiraman\n" +
			"• Test faylini guruhda viktorinaga aylantiraman\n" +
			"• Guruhda moderatsiya: o'chirish, mute, ban\n\n" +
			"Guruhga qo'shing va ismim bilan chaqiring. /help — barcha imkoniyatlar.",
	},
	"ru": {
		bio: "Maria — AI-собеседник: вопросы, MLBB, курсы валют, погода, музыка, фильмы и модерация групп.",
		description: "Привет! Я Maria — AI-собеседник, говорю на узбекском, русском и английском.\n\n" +
			"• Отвечаю на вопросы; читаю фото, PDF и Word\n" +
			"• Курсы валют, погода, время намаза, новости\n" +
			"• Mobile Legends: герои, сборки, скины, контрпики\n" +
			"• Ищу песни, фильмы и видео\n" +
			"• Превращаю файл с тестом в викторину в группе\n" +
			"• Модерация группы: удаление, мут, бан\n\n" +
			"Добавьте меня в группу и позовите по имени. /help — все возможности.",
	},
	"en": {
		bio: "Maria — an AI chat companion: questions, MLBB, exchange rates, weather, music, films and group moderation.",
		description: "Hi! I'm Maria, an AI chat companion speaking Uzbek, Russian and English.\n\n" +
			"• I answer questions and read photos, PDF and Word files\n" +
			"• Exchange rates, weather, prayer times, news\n" +
			"• Mobile Legends: heroes, builds, skins, counters\n" +
			"• I find songs, films and videos\n" +
			"• I turn a test file into a quiz in a group\n" +
			"• Group moderation: delete, mute, ban\n\n" +
			"Add me to a group and call me by name. /help shows everything.",
	},
}

// ProfileAccount reads and sets the bot's profile texts.
type ProfileAccount interface {
	GetBotProfile(ctx context.Context, language string) (telegram.BotProfile, error)
	SetBotBio(ctx context.Context, bio, language string) error
	SetBotDescription(ctx context.Context, description, language string) error
}

// SetProfileTexts sets the bio and description that differ from
// profileTexts, logging a failure: the bot works without them. Only what
// changed is set, as Telegram limits how often a profile may change and a
// serverless deployment starts often.
func SetProfileTexts(ctx context.Context, account ProfileAccount, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, commandsTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for language, want := range profileTexts {
		wg.Go(func() {
			if err := setProfileText(ctx, account, language, want); err != nil {
				logger.Warn("could not set the profile texts", "language", language, "error", err)
			}
		})
	}
	wg.Wait()
}

func setProfileText(ctx context.Context, account ProfileAccount, language string, want profileText) error {
	have, err := account.GetBotProfile(ctx, language)
	if err != nil {
		return err
	}
	if have.Bio != want.bio {
		if err := account.SetBotBio(ctx, want.bio, language); err != nil {
			return err
		}
	}
	if have.Description != want.description {
		return account.SetBotDescription(ctx, want.description, language)
	}
	return nil
}
