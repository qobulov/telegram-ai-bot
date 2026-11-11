package service

import (
	"context"

	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/telegram"
)

// GameLinks makes the page a press of a game's Play button opens; ok is
// false for a game that has none.
type GameLinks interface {
	PlayURL(shortName string, p game.Player) (link string, ok bool)
}

// GameTable sends the game and answers a press of its Play button.
type GameTable interface {
	SendGame(ctx context.Context, chatID int64, shortName string, replyToMessageID int64) (int64, error)
	AnswerGameCallback(ctx context.Context, id, url string) error
}

var (
	textStatsPrivate = localText{
		uz: "Guruhlar statistikasi: kim ko'p yozadi, kim ko'p so'kinadi, qaysi soatlarda gap qizg'in, qaysi himoyalar yoqilgan.",
		ru: "Статистика групп: кто пишет больше всех, кто больше ругается, в какие часы самый разговор, какие защиты включены.",
		en: "The groups' statistics: who writes and who swears the most, the busiest hours, which guards are on.",
	}
	textStatsGroup = localText{
		uz: "Guruh statistikasi. Faqat bot egalari ko'ra oladi.",
		ru: "Статистика группы. Видят только владельцы бота.",
		en: "This group's statistics. Only the bot's owners can see them.",
	}
	textStatsButton = localText{uz: "📊 Statistikani ochish", ru: "📊 Открыть статистику", en: "📊 Open the statistics"}
)

// statsCommand answers /stats, for the bot's owners known by ID alone,
// with a button that opens the dashboard; to anyone else it is an unknown
// command, as the dashboard names who swears most. In a private chat it is
// a Mini App button, which lists the bot's groups. A group cannot have
// one, so there it is a link to the bot's main Mini App with the group in
// its startapp parameter; the dashboard checks again that an owner opened
// it.
func (s *Service) statsCommand(in incoming) outcome {
	if s.opts.StatsURL == "" || s.opts.Bot.Username == "" || !s.opts.OwnerIDs[in.userID] {
		return say(textUnknown.to(in))
	}
	if !in.group {
		button := telegram.Button{Text: textStatsButton.to(in), WebApp: &telegram.WebAppInfo{URL: s.opts.StatsURL}}
		return outcome{text: textStatsPrivate.to(in), buttons: [][]telegram.Button{{button}}}
	}
	link := "https://t.me/" + s.opts.Bot.Username + "?startapp=" + miniapp.GroupParam(in.scope.ChatID)
	return outcome{text: textStatsGroup.to(in), buttons: [][]telegram.Button{{{Text: textStatsButton.to(in), URL: link}}}}
}

// gameCommand answers /game, /ilon, /otinchi and /boss with the game itself, which Telegram
// shows with its Play button and table of scores.
func (s *Service) gameCommand(ctx context.Context, in incoming, shortName string) outcome {
	if s.opts.Game == nil || s.opts.GameTable == nil {
		return say(textUnknown.to(in))
	}
	if s.restricted(in) {
		return say(textForbidden)
	}
	// In a group the game goes through the send_game tool, which allows a chat
	// two games in ten minutes: the command does not count in the group's
	// quota, and another game too soon gets only a reaction.
	if in.group && s.opts.Tools != nil {
		var sent struct{}
		switch _, failure := s.itemResult(ctx, in, "send_game", struct {
			Game string `json:"game"`
		}{shortName}, &sent); failure {
		case "":
			return outcome{}
		case "invalid_arguments":
			return outcome{reaction: gameTooSoonReaction}
		}
		return say(textGameLater.to(in))
	}
	if _, err := s.opts.GameTable.SendGame(ctx, in.msg.Chat.ID, shortName, in.replyTarget()); err != nil {
		s.opts.Logger.Error("could not send the game", "game", shortName, "chat_id", in.msg.Chat.ID, "error", err)
		return say(textError.to(in))
	}
	return outcome{}
}

// gamesMenu answers /oyinlar: every game, as commands to tap, which the
// menu no longer lists one by one.
func (s *Service) gamesMenu(in incoming) outcome {
	lang := languageOf(in)
	text := textGamesTitle.to(in) + "\n" + textGamesDooz.to(in)
	if s.opts.Game != nil && s.opts.GameTable != nil {
		text += helpPages[lang]
	}
	if s.opts.Cards != nil {
		text += "\n\n" + textGamesCards.to(in)
	}
	return say(text)
}

var (
	textGamesTitle = localText{uz: "🎮 **O'yinlar** — bosing va o'ynang:", ru: "🎮 **Игры** — нажмите и играйте:", en: "🎮 **Games** — tap one to play:"}
	textGamesDooz  = localText{
		uz: "/dooz — Dooz (X-O): do'stingiz yoki men bilan",
		ru: "/dooz — крестики-нолики: с другом или со мной",
		en: "/dooz — tic-tac-toe: with a friend or with me",
	}
	textGamesCards = localText{
		uz: "🎴 Kartalar: /karta · /kartalar (ilova) · /kartatop · /kristal · /avto · /duel",
		ru: "🎴 Карты: /karta · /kartalar (приложение) · /kartatop · /kristal · /avto · /duel",
		en: "🎴 Cards: /karta · /kartalar (the app) · /kartatop · /kristal · /avto · /duel",
	}
)

// gameTooSoonReaction answers a game asked for too soon after the last two:
// Telegram takes only a few emoji as reactions, and no hourglass.
const gameTooSoonReaction = overQuotaReaction

// restricted reports a command ADMIN_COMMANDS keeps for administrators,
// sent by someone else, as runCommand refuses it.
func (s *Service) restricted(in incoming) bool {
	return s.opts.AdminCommands[commandName(in.text)] && !s.opts.IsAdmin(in.userID)
}

// pressedGame opens the game for whoever pressed its Play button, with a
// session for them on that game message: what the page sends with their
// score, which is set there.
func (s *Service) pressedGame(q *telegram.CallbackQuery) {
	if s.opts.Game == nil || s.opts.GameTable == nil || q.From == nil {
		return
	}
	player := game.Player{UserID: q.From.ID, Name: q.From.FirstName, InlineMessageID: q.InlineMessageID}
	if q.Message != nil {
		player.ChatID, player.MessageID = q.Message.Chat.ID, q.Message.MessageID
	}
	link, ok := s.opts.Game.PlayURL(q.GameShortName, player)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if err := s.opts.GameTable.AnswerGameCallback(ctx, q.ID, link); err != nil {
		s.opts.Logger.Warn("could not open the game", "user_id", q.From.ID, "error", err)
	}
}
