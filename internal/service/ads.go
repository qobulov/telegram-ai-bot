package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
)

// adData starts the callback data of the buttons under an owner's ad:
// adData+"g:<message>" drafts it for the groups, "a:" for the groups and
// private chats, "u:" for the private chats; adData+"x" puts it aside.
const adData = "ad:"

// adWords finds a game account's ad in a caption: a price, a sale, an
// account, its skins.
var adWords = regexp.MustCompile(`(?i)(\$|so['ʻ‘’]?m\b|сўм|сум|narx|нарх|цена|sotil|сотил|sotaman|продаж|продам|prodam|akk|акк|akaunt|аккаунт|account|collector|kollektor|skin|скин|mlbb|млбб)`)

// isAd reports an owner's photo or video whose caption reads as an ad: it
// is offered for posting in code, without the model, whose provider
// refuses some of them.
func isAd(msg *telegram.Message) bool {
	media := len(msg.Photo) > 0 || msg.Video != nil
	caption := strings.TrimSpace(msg.Caption)
	return media && caption != "" && !strings.HasPrefix(caption, "/") && adWords.MatchString(caption)
}

// offerAd answers an owner's ad with where it may be posted; nothing is
// drafted until they choose.
func offerAd(in incoming, ad *telegram.Message) outcome {
	id := strconv.FormatInt(ad.MessageID, 10)
	out := say(textAdOffer.to(in))
	out.buttons = [][]telegram.Button{
		{{Text: textAdGroups.to(in), Data: adData + "g:" + id}, {Text: textAdEveryone.to(in), Data: adData + "a:" + id}},
		{{Text: textAdPrivate.to(in), Data: adData + "u:" + id}, {Text: textAdAside.to(in), Data: adData + "x"}},
	}
	return out
}

// pressedAd drafts the ad for where the owner chose, in place of the
// choice, and asks them to confirm with the announcement's buttons.
func (s *Service) pressedAd(q *telegram.CallbackQuery) {
	if q.Message.Chat.Type != telegram.ChatTypePrivate {
		return
	}
	in := incoming{
		// A press has no update ID; send_to_groups needs one other than
		// the confirmation's, and the press's own ID stands in.
		updateID: -int64(hashOf(q.ID) >> 1),
		msg:      &telegram.Message{MessageID: q.Message.MessageID, From: q.From, Chat: q.Message.Chat},
		scope:    storage.Scope{ChatID: q.Message.Chat.ID, UserID: q.From.ID},
		userID:   q.From.ID,
	}
	ctx, cancel := context.WithTimeout(context.Background(), keyboardTimeout)
	defer cancel()
	if err := s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, "", false); err != nil {
		s.opts.Logger.Debug("could not answer an ad press", "user_id", in.userID, "error", err)
	}
	if !s.isOwner(q.From) || s.opts.Tools == nil {
		return
	}
	text, buttons := s.draftAd(ctx, in, q.Data)
	if err := s.opts.Buttons.EditMessageMenu(ctx, in.msg.Chat.ID, in.msg.MessageID, text, buttons); err != nil {
		s.deliver(in, &outcome{text: text, buttons: buttons})
	}
}

// draftAd is what a press of an ad's choice shows: where the draft goes
// and the buttons to send or drop it, or why there is none.
func (s *Service) draftAd(ctx context.Context, in incoming, data string) (string, [][]telegram.Button) {
	choice, id, _ := strings.Cut(strings.TrimPrefix(data, adData), ":")
	to := map[string]string{"g": "groups", "a": "all", "u": "users"}[choice]
	messageID, err := strconv.ParseInt(id, 10, 64)
	if to == "" || err != nil {
		return textAdAsideDone.to(in), nil
	}
	caller := s.caller(in)
	caller.Target = &tools.Target{MessageID: messageID, UserID: in.userID}
	ctx = tools.WithTurn(ctx, caller, &tools.Sink{})
	args, _ := json.Marshal(map[string]any{"action": "prepare", "copy": true, "preview": false, "to": to})
	var envelope struct {
		OK     bool `json:"ok"`
		Result struct {
			Groups  []string `json:"groups"`
			Private int      `json:"private_chats"`
		} `json:"result"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	raw := s.opts.Tools.Execute(ctx, in.userID, "send_to_groups", string(args))
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil || !envelope.OK {
		s.opts.Logger.Warn("an owner's ad could not be drafted", "user_id", in.userID, "result", truncateLog(raw))
		return fmt.Sprintf(textAnnounceRefused.to(in), envelope.Error.Message), nil
	}
	text := draftText(in, "", envelope.Result.Groups, envelope.Result.Private, textAdConfirm)
	return text, [][]telegram.Button{{
		{Text: textAnnounceSendButton.to(in), Data: announceData + "ok"},
		{Text: textAnnounceDropButton.to(in), Data: announceData + "no"},
	}}
}

var (
	textAdOffer = localText{
		uz: "📢 Shu reklamani e'lon qilaymi? Qayerga?",
		ru: "📢 Разослать это объявление? Куда?",
		en: "📢 Post this ad? Where to?",
	}
	textAdGroups    = localText{uz: "📢 Guruhlarga", ru: "📢 В группы", en: "📢 Groups"}
	textAdEveryone  = localText{uz: "🌍 Hammaga", ru: "🌍 Всем", en: "🌍 Everyone"}
	textAdPrivate   = localText{uz: "👤 Faqat lichkaga", ru: "👤 Только в личку", en: "👤 Private chats"}
	textAdAside     = localText{uz: "❌ Kerak emas", ru: "❌ Не надо", en: "❌ No"}
	textAdAsideDone = localText{uz: "Mayli, e'lon qilinmaydi.", ru: "Хорошо, не рассылаю.", en: "All right, it is not posted."}
	textAdConfirm   = localText{
		uz: "📢 Reklama tayyor.\n%s\n\nYuboraymi?",
		ru: "📢 Объявление готово.\n%s\n\nОтправить?",
		en: "📢 The ad is ready.\n%s\n\nSend it?",
	}
)
