package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"telegram-ai-bot/internal/cards"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

const (
	// cardAutoBatch is how many players one look for due cards takes.
	cardAutoBatch = 20
	// cardAutoBudget is how long a call keeps opening cards, so that the
	// reminders sent after it still end inside the function's time; who is
	// left waits for the next minute.
	cardAutoBudget = 8 * time.Second
	// cardAutoTimeout bounds one player's card: its draw, picture and send.
	cardAutoTimeout = 8 * time.Second
)

// CardAutoStore is what the auto opener needs of Kartalar.
type CardAutoStore interface {
	DueCardAutos(ctx context.Context, now time.Time, every time.Duration, limit int) ([]storage.CardAuto, error)
	DrawCard(ctx context.Context, userID int64, name string, roll storage.CardRoll, every time.Duration, now time.Time) (storage.CardDraw, error)
	StopCardAuto(ctx context.Context, userID int64) error
}

// CardAutoSender sends the opened cards.
type CardAutoSender interface {
	SendPhoto(ctx context.Context, photo telegram.Photo) (int64, error)
	SendMessage(ctx context.Context, msg telegram.Outgoing) (int64, error)
}

// CardPictures fetches a card's picture.
type CardPictures interface {
	FetchPicture(ctx context.Context, link string) ([]byte, string, error)
}

// CardAutoOptions configures OpenDueCards. Store, Sender and Pictures are
// required.
type CardAutoOptions struct {
	Store    CardAutoStore
	Sender   CardAutoSender
	Pictures CardPictures
	// CardsURL is Kartalar's Mini App, whose button goes under each card;
	// "" for none.
	CardsURL string
	Logger   *slog.Logger
	Now      func() time.Time
}

// OpenDueCards opens the card of every player with the auto opener on whose
// next card may be opened, and sends it quietly to their private chat. It
// reports how many it opened. A player the bot may not write to has the
// opener turned off; the card opened is theirs all the same.
func OpenDueCards(ctx context.Context, opts CardAutoOptions) (int, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	opened := 0
	started := opts.Now()
	for opts.Now().Sub(started) < cardAutoBudget && ctx.Err() == nil {
		due, err := opts.Store.DueCardAutos(ctx, opts.Now(), cards.Every, cardAutoBatch)
		if err != nil {
			return opened, err
		}
		for _, player := range due {
			if ctx.Err() != nil || opts.Now().Sub(started) >= cardAutoBudget {
				return opened, nil
			}
			if openCard(ctx, opts, player) {
				opened++
			}
		}
		if len(due) < cardAutoBatch {
			break
		}
	}
	return opened, nil
}

// openCard opens and sends one player's card, and reports whether it opened
// one: another call may have opened it first.
func openCard(ctx context.Context, opts CardAutoOptions, player storage.CardAuto) bool {
	ctx, cancel := context.WithTimeout(ctx, cardAutoTimeout)
	defer cancel()
	draw, err := opts.Store.DrawCard(ctx, player.UserID, player.Name, cards.WithPity(cards.RollRandom), cards.Every, opts.Now())
	var wait *storage.CardWaitError
	switch {
	case errors.As(err, &wait):
		return false
	case err != nil:
		opts.Logger.Error("could not open a card for a player", "user_id", player.UserID, "error", err)
		return false
	}
	caption := textCardAutoOpened.inLanguage(player.Language) + "\n" + cardCaption(player.Language, draw)
	if err := sendOpenedCard(ctx, opts, player, draw.Card, caption); err != nil {
		opts.Logger.Info("could not send an opened card", "user_id", player.UserID, "error", err)
		if unreachable(err) {
			if err := opts.Store.StopCardAuto(context.WithoutCancel(ctx), player.UserID); err != nil {
				opts.Logger.Warn("could not stop a card auto opener", "user_id", player.UserID, "error", err)
			}
		}
	}
	return true
}

// sendOpenedCard sends the card with its picture, or its words alone when
// the picture cannot be had.
func sendOpenedCard(ctx context.Context, opts CardAutoOptions, player storage.CardAuto, card storage.Card, caption string) error {
	var buttons [][]telegram.Button
	if opts.CardsURL != "" {
		buttons = [][]telegram.Button{{{Text: textCardsAppButton.inLanguage(player.Language), WebApp: &telegram.WebAppInfo{URL: opts.CardsURL}}}}
	}
	picture, _, err := opts.Pictures.FetchPicture(ctx, card.ImageURL)
	if err != nil {
		opts.Logger.Warn("could not fetch a card's picture", "card", card.ID, "error", err)
		_, err = opts.Sender.SendMessage(ctx, telegram.Outgoing{ChatID: player.UserID, Text: caption, Buttons: buttons, Silent: true})
		return err
	}
	_, err = opts.Sender.SendPhoto(ctx, telegram.Photo{ChatID: player.UserID, Data: picture, Caption: caption, Buttons: buttons, Silent: true})
	return err
}

// unreachable is a send Telegram refused for good: the player blocked the
// bot or never started it.
func unreachable(err error) bool {
	var apiErr *telegram.APIError
	return errors.As(err, &apiErr) && (apiErr.Code == http.StatusForbidden || apiErr.Code == http.StatusBadRequest)
}

// toggleCardAuto answers /avto: it turns the player's auto opener on, or off
// when it was on.
func (s *Service) toggleCardAuto(ctx context.Context, in incoming) outcome {
	on, err := s.opts.Cards.ToggleCardAuto(ctx, in.userID, doozName(in.msg.From), languageOf(in))
	if err != nil {
		s.opts.Logger.Error("could not toggle a card auto opener", "user_id", in.userID, "error", err)
		return say(textCardsUnavailable.to(in))
	}
	if !on {
		return say(textCardAutoOff.to(in))
	}
	if in.group {
		return outcome{text: textCardAutoOnGroup.to(in), buttons: s.cardsAppButton(in)}
	}
	return say(textCardAutoOn.to(in))
}

var (
	textCardAutoOpened = localText{
		uz: "🤖 Avto-ochildi · o'chirish: /avto",
		ru: "🤖 Открыто автоматически · выключить: /avto",
		en: "🤖 Opened for you · turn off: /avto",
	}
	textCardAutoOn = localText{
		uz: "🤖 Avto-ochish yoqildi: har karta tayyor bo'lishi bilan o'zim ochib, shu yerga ovozsiz yuboraman.\nO'chirish: /avto",
		ru: "🤖 Автооткрытие включено: каждую карту я открою, как только она будет готова, и пришлю сюда без звука.\nВыключить: /avto",
		en: "🤖 Auto opener on: I will open each card as soon as it is ready and send it here quietly.\nTurn off: /avto",
	}
	textCardAutoOnGroup = localText{
		uz: "🤖 Avto-ochish yoqildi: kartalar shaxsiy chatingizga ovozsiz keladi. Botga hali yozmagan bo'lsangiz, pastdagi tugma orqali /start bosing.\nO'chirish: /avto",
		ru: "🤖 Автооткрытие включено: карты будут приходить вам в личку без звука. Если вы ещё не писали боту, нажмите кнопку ниже и /start.\nВыключить: /avto",
		en: "🤖 Auto opener on: your cards will come to your private chat quietly. If you have not written to the bot yet, tap the button below and /start.\nTurn off: /avto",
	}
	textCardAutoOff = localText{
		uz: "⏸ Avto-ochish o'chirildi. Kartani o'zingiz oching: /karta",
		ru: "⏸ Автооткрытие выключено. Открывайте карты сами: /karta",
		en: "⏸ Auto opener off. Open your cards yourself: /karta",
	}
)
