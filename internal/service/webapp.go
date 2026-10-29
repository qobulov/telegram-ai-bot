package service

import (
	"context"

	"telegram-ai-bot/internal/game"
	"telegram-ai-bot/internal/miniapp"
	"telegram-ai-bot/internal/telegram"
)

// GameLinks makes the page a press of the game's Play button opens.
type GameLinks interface {
	PlayURL(p game.Player) string
}

// GameTable sends the game and answers a press of its Play button.
type GameTable interface {
	SendGame(ctx context.Context, chatID int64, shortName string, replyToMessageID int64) (int64, error)
	AnswerGameCallback(ctx context.Context, id, url string) error
}

var (
	textStatsPrivate = localText{
		uz: "Guruhlaringiz statistikasi: kim ko'p yozadi, qaysi soatlarda gap qizg'in, qaysi himoyalar yoqilgan.",
		ru: "Статистика ваших групп: кто пишет больше всех, в какие часы самый разговор, какие защиты включены.",
		en: "Your groups' statistics: who writes the most, the busiest hours, which guards are on.",
	}
	textStatsGroup = localText{
		uz: "Guruh statistikasi: kim ko'p yozadi, qaysi soatlarda gap qizg'in, qaysi himoyalar yoqilgan. Faqat shu guruh a'zolari ko'ra oladi.",
		ru: "Статистика группы: кто пишет больше всех, в какие часы самый разговор, какие защиты включены. Видят только участники этой группы.",
		en: "This group's statistics: who writes the most, the busiest hours, which guards are on. Only its members can see them.",
	}
	textStatsButton = localText{uz: "📊 Statistikani ochish", ru: "📊 Открыть статистику", en: "📊 Open the statistics"}
)

// statsCommand answers /stats with a button that opens the dashboard. In a
// private chat it is a Mini App button, which lists the user's groups. A
// group cannot have one, so there it is a link to the bot's main Mini App
// with the group in its startapp parameter; the dashboard shows the group
// only to whom Telegram says is in it.
func (s *Service) statsCommand(in incoming) outcome {
	if s.opts.StatsURL == "" || s.opts.Bot.Username == "" {
		return say(textUnknown.to(in))
	}
	if s.restricted(in) {
		return say(textForbidden)
	}
	if !in.group {
		button := telegram.Button{Text: textStatsButton.to(in), WebApp: &telegram.WebAppInfo{URL: s.opts.StatsURL}}
		return outcome{text: textStatsPrivate.to(in), buttons: [][]telegram.Button{{button}}}
	}
	link := "https://t.me/" + s.opts.Bot.Username + "?startapp=" + miniapp.GroupParam(in.scope.ChatID)
	return outcome{text: textStatsGroup.to(in), buttons: [][]telegram.Button{{{Text: textStatsButton.to(in), URL: link}}}}
}

// gameCommand answers /game with the game itself, which Telegram shows with
// its Play button and table of scores.
func (s *Service) gameCommand(ctx context.Context, in incoming) outcome {
	if s.opts.Game == nil || s.opts.GameTable == nil {
		return say(textUnknown.to(in))
	}
	if s.restricted(in) {
		return say(textForbidden)
	}
	if _, err := s.opts.GameTable.SendGame(ctx, in.msg.Chat.ID, game.ShortName, in.replyTarget()); err != nil {
		s.opts.Logger.Error("could not send the game", "chat_id", in.msg.Chat.ID, "error", err)
		return say(textError.to(in))
	}
	return outcome{}
}

// restricted reports a command ADMIN_COMMANDS keeps for administrators,
// sent by someone else, as runCommand refuses it.
func (s *Service) restricted(in incoming) bool {
	return s.opts.AdminCommands[commandName(in.text)] && !s.opts.IsAdmin(in.userID)
}

// pressedGame opens the game for whoever pressed its Play button, with a
// session for them on that game message: what the page sends with their
// score, which is set there.
func (s *Service) pressedGame(q *telegram.CallbackQuery) {
	if s.opts.Game == nil || s.opts.GameTable == nil || q.From == nil || q.GameShortName != game.ShortName {
		return
	}
	player := game.Player{UserID: q.From.ID, InlineMessageID: q.InlineMessageID}
	if q.Message != nil {
		player.ChatID, player.MessageID = q.Message.Chat.ID, q.Message.MessageID
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if err := s.opts.GameTable.AnswerGameCallback(ctx, q.ID, s.opts.Game.PlayURL(player)); err != nil {
		s.opts.Logger.Warn("could not open the game", "user_id", q.From.ID, "error", err)
	}
}
