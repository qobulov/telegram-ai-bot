package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
	"telegram-ai-bot/internal/tools"
)

// AnnouncementWriter is the model that writes an announcement from an
// owner's notes.
type AnnouncementWriter interface {
	Complete(ctx context.Context, req ai.Request) (ai.Message, error)
}

// RecentMessages gives a conversation's latest messages, oldest first.
type RecentMessages interface {
	History(ctx context.Context, scope storage.Scope, limit int) ([]ai.Message, error)
}

// announceData starts the callback data of the buttons under an
// announcement's preview: announceData+"ok" sends it, +"no" drops it.
const announceData = "an:"

// announceTimeout bounds a press of Send: posting in every group and
// private chat, one by one, as send_to_groups allows.
const announceTimeout = 45 * time.Second

// announceWords finds an owner asking for an announcement or a post in the
// groups or private chats.
var announceWords = regexp.MustCompile(`(?i)(e['ʼ‘’` + "`" + `]?lon|эълон|элон|объявлени|reklama|реклам|tarqat|guruhlarga|guruhga|lichka|личк|personal|hammaga|разошли|разошл)`)

// announceCommand is an owner's /elon: send_to_groups run in code, without
// the model, whose provider may refuse a sale's words or a video's picture.
// "/elon <text>" drafts a post for every group, in reply to a photo or a
// video that post under the text as its caption, the owner seeing a
// preview first; "/elon hammaga <text>" drafts it for the private chats of
// those who wrote to the bot too, "/elon lichkaga <text>" for those only;
// "/elon ha" sends it, "/elon bekor" drops it.
func (s *Service) announceCommand(ctx context.Context, in incoming) outcome {
	rest := strings.TrimSpace(strings.TrimPrefix(in.text, strings.Fields(in.text)[0]))
	switch strings.ToLower(rest) {
	case "ha", "yubor", "да", "yes":
		return s.sendAnnouncement(ctx, in)
	case "bekor", "yo'q", "yoq", "нет", "no", "cancel":
		if _, failure := s.runAnnounce(ctx, in, `{"action":"cancel"}`); failure != "" {
			return say(failure)
		}
		return say(textAnnounceCancelled.to(in))
	}
	to, rest := audienceOf(rest)
	if rest == "" && in.msg.ReplyToMessage == nil {
		return say(textAnnounceUsage.to(in))
	}
	args, _ := json.Marshal(map[string]any{"action": "prepare", "text": rest, "copy": in.msg.ReplyToMessage != nil, "to": to})
	raw, failure := s.runAnnounce(ctx, in, string(args))
	if failure != "" {
		return say(failure)
	}
	var draft struct {
		Groups       []string `json:"groups"`
		Private      int      `json:"private_chats"`
		Text         string   `json:"text"`
		PreviewError string   `json:"preview_error"`
	}
	_ = json.Unmarshal(raw, &draft)
	if draft.PreviewError != "" {
		return say(fmt.Sprintf(textAnnounceNoPreview.to(in), draft.PreviewError))
	}
	return say(draftText(in, draft.Text, draft.Groups, draft.Private, textAnnounceDrafted))
}

// draftText is what an owner is shown of a draft: its text unless a
// preview of it came already, and where it goes.
func draftText(in incoming, text string, groups []string, private int, footer localText) string {
	where := fmt.Sprintf(textAnnounceGroups.to(in), len(groups), strings.Join(groups, ", "))
	if len(groups) == 0 {
		where = ""
	}
	if private > 0 {
		where = strings.TrimSpace(where + "\n" + fmt.Sprintf(textAnnouncePrivate.to(in), private))
	}
	shown := fmt.Sprintf(footer.to(in), where)
	if text != "" {
		shown = text + "\n\n———\n" + shown
	}
	return shown
}

// announceForBlocked takes an owner's request for an announcement that
// the main model's provider refused: a model asked only to write it, from
// the owner's latest words, drafts it with send_to_groups, for the groups
// or private chats the words name, under the replied photo or video if
// any, and the owner confirms with a button. ok is false for words that
// ask for no announcement.
func (s *Service) announceForBlocked(ctx context.Context, in incoming) (outcome, bool) {
	if (len(in.msg.Photo) > 0 || in.msg.Video != nil) && s.opts.Tools != nil && s.opts.Buttons != nil {
		// An owner's photo or video the provider refused is most likely an
		// ad: offer to post it as it is.
		return offerAd(in, in.msg), true
	}
	words := strings.TrimSpace(in.msg.Text + " " + in.msg.Caption)
	if s.opts.Writer == nil || s.opts.Tools == nil || !announceWords.MatchString(words) {
		return outcome{}, false
	}
	written, err := s.writeAnnouncement(ctx, in, words)
	if err != nil {
		s.opts.Logger.Warn("could not write a blocked announcement", "user_id", in.userID, "error", err)
		return outcome{}, false
	}
	to, _ := audienceOf(words)
	if to == "groups" {
		to = audienceNamed(words)
	}
	args, _ := json.Marshal(map[string]any{"action": "prepare", "text": written, "copy": copiable(s, in.msg.ReplyToMessage), "to": to})
	raw, failure := s.runAnnounce(ctx, in, string(args))
	if failure != "" {
		return say(failure), true
	}
	var draft struct {
		Groups       []string `json:"groups"`
		Private      int      `json:"private_chats"`
		Text         string   `json:"text"`
		PreviewError string   `json:"preview_error"`
	}
	_ = json.Unmarshal(raw, &draft)
	if draft.PreviewError != "" {
		return say(fmt.Sprintf(textAnnounceNoPreview.to(in), draft.PreviewError)), true
	}
	out := say(draftText(in, draft.Text, draft.Groups, draft.Private, textAnnounceDraftedButtons))
	out.buttons = [][]telegram.Button{{
		{Text: textAnnounceSendButton.to(in), Data: announceData + "ok"},
		{Text: textAnnounceDropButton.to(in), Data: announceData + "no"},
	}}
	return out, true
}

// audienceNamed is whom words for an announcement name: the private chats
// too for "lichka" or "personal", only them with "faqat"; the groups
// otherwise.
func audienceNamed(words string) string {
	lower := strings.ToLower(words)
	private := strings.Contains(lower, "lichka") || strings.Contains(lower, "личк") || strings.Contains(lower, "personal") || strings.Contains(lower, "hammaga")
	switch {
	case private && (strings.Contains(lower, "faqat") || strings.Contains(lower, "только")):
		return "users"
	case private:
		return "all"
	}
	return "groups"
}

// copiable reports a replied message that can be posted under a new
// caption: a photo, a video, a GIF or a file.
func copiable(s *Service, replied *telegram.Message) bool {
	if replied == nil {
		return false
	}
	return len(replied.Photo) > 0 || replied.Video != nil || replied.Animation != nil || replied.Document != nil
}

// writeAnnouncement has the writer model turn the owner's latest words,
// and what was said just before, into an announcement, with no persona and
// no picture, so that the provider is asked as little as can be.
func (s *Service) writeAnnouncement(ctx context.Context, in incoming, words string) (string, error) {
	var notes strings.Builder
	if s.opts.Recent != nil {
		recent, err := s.opts.Recent.History(ctx, in.scope, 8)
		if err != nil {
			s.opts.Logger.Warn("could not read the owner's recent words", "user_id", in.userID, "error", err)
		}
		for _, m := range recent {
			if m.Content == "" || (m.Role != ai.RoleUser && m.Role != ai.RoleAssistant) {
				continue
			}
			who := "Owner"
			if m.Role == ai.RoleAssistant {
				who = "Bot"
			}
			fmt.Fprintf(&notes, "%s: %s\n", who, m.Content)
		}
	}
	if replied := in.msg.ReplyToMessage; replied != nil && strings.TrimSpace(replied.Text+replied.Caption) != "" {
		fmt.Fprintf(&notes, "The post it is for says: %s\n", strings.TrimSpace(replied.Text+" "+replied.Caption))
	}
	fmt.Fprintf(&notes, "Owner, now: %s", words)
	reply, err := s.opts.Writer.Complete(ctx, ai.Request{Messages: []ai.Message{
		{Role: ai.RoleSystem, Content: announcementPrompt},
		{Role: ai.RoleUser, Content: notes.String()},
	}})
	if err != nil {
		return "", err
	}
	written := strings.TrimSpace(strings.ReplaceAll(reply.Content, "**", ""))
	if written == "" {
		return "", fmt.Errorf("an empty announcement (finish reason %s)", reply.FinishReason)
	}
	return written, nil
}

// announcementPrompt is all the writer model is told.
const announcementPrompt = "You write one Telegram announcement for a bot's owner, from their notes and the chat before them. " +
	"Reply with the announcement text only, in the language of the notes (Uzbek in Latin letters unless they are in another language): " +
	"a first line that catches the eye, with one emoji, then the facts as short lines starting with •. " +
	"Keep every price, number, name and @username exactly as given, and add nothing that is not in the notes. " +
	"Leave out the owner's instructions to the bot, such as asking to post it, to write a description or where to send it. " +
	"No markdown and no asterisks. At most 600 characters."

// pressedAnnounce sends or drops the draft whose preview's button an owner
// pressed, putting the outcome in place of the preview's buttons.
func (s *Service) pressedAnnounce(q *telegram.CallbackQuery) {
	if q.Message.Chat.Type != telegram.ChatTypePrivate {
		return
	}
	in := incoming{
		// A press has no update ID; send_to_groups needs one other than
		// the draft's, and the press's own ID stands in.
		updateID: -int64(hashOf(q.ID) >> 1),
		msg:      &telegram.Message{MessageID: q.Message.MessageID, From: q.From, Chat: q.Message.Chat},
		scope:    storage.Scope{ChatID: q.Message.Chat.ID, UserID: q.From.ID},
		userID:   q.From.ID,
	}
	ctx, cancel := context.WithTimeout(context.Background(), announceTimeout)
	defer cancel()
	if err := s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, textWorking.to(in), false); err != nil {
		s.opts.Logger.Debug("could not answer an announcement press", "user_id", in.userID, "error", err)
	}
	if !s.isOwner(q.From) || s.opts.Tools == nil {
		return
	}
	var out outcome
	if strings.TrimPrefix(q.Data, announceData) == "ok" {
		out = s.sendAnnouncement(ctx, in)
	} else if _, failure := s.runAnnounce(ctx, in, `{"action":"cancel"}`); failure != "" {
		out = say(failure)
	} else {
		out = say(textAnnounceCancelled.to(in))
	}
	if err := s.opts.Buttons.EditMessageMenu(ctx, in.msg.Chat.ID, in.msg.MessageID, q.Message.Text+"\n\n"+out.text, nil); err != nil {
		s.deliver(in, &out)
	}
}

// audienceOf is whom an announcement is for, by its first word: "all" for
// "hammaga", "users" for "lichkaga", the groups otherwise; and the rest of
// its words.
func audienceOf(words string) (to, rest string) {
	first, after, _ := strings.Cut(words, " ")
	switch strings.ToLower(first) {
	case "hammaga", "всем", "everyone":
		return "all", strings.TrimSpace(after)
	case "lichkaga", "личка", "private":
		return "users", strings.TrimSpace(after)
	}
	return "groups", words
}

func (s *Service) sendAnnouncement(ctx context.Context, in incoming) outcome {
	raw, failure := s.runAnnounce(ctx, in, `{"action":"confirm"}`)
	if failure != "" {
		return say(failure)
	}
	var sent struct {
		Sent      int      `json:"sent"`
		Failed    []string `json:"failed"`
		Unreached int      `json:"private_unreached"`
	}
	_ = json.Unmarshal(raw, &sent)
	text := fmt.Sprintf(textAnnounceSent.to(in), sent.Sent)
	if len(sent.Failed) > 0 {
		text += "\n" + fmt.Sprintf(textAnnounceFailed.to(in), strings.Join(sent.Failed, ", "))
	}
	if sent.Unreached > 0 {
		text += "\n" + fmt.Sprintf(textAnnounceUnreached.to(in), sent.Unreached)
	}
	return say(text)
}

// runAnnounce runs send_to_groups on the owner's turn and gives its
// result, or the tool's refusal, worded for the owner.
func (s *Service) runAnnounce(ctx context.Context, in incoming, args string) (json.RawMessage, string) {
	ctx = tools.WithTurn(ctx, s.caller(in), &tools.Sink{})
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	raw := s.opts.Tools.Execute(ctx, in.userID, "send_to_groups", args)
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil || !envelope.OK {
		s.opts.Logger.Warn("an owner's announcement failed", "user_id", in.userID, "result", truncateLog(raw))
		return nil, fmt.Sprintf(textAnnounceRefused.to(in), envelope.Error.Message)
	}
	return envelope.Result, ""
}

var (
	textAnnounceUsage = localText{
		uz: "📢 E'lon: /elon matn — barcha guruhlarga; /elon hammaga matn — guruhlarga va botga yozgan hammaning lichkasiga; /elon lichkaga matn — faqat lichkalarga. Rasm yoki videoga reply qilib yozsangiz — o'sha video shu tavsif bilan. Avval namunasi keladi, keyin /elon ha — yuborish, /elon bekor — bekor qilish.",
		ru: "📢 Объявление: /elon текст — во все группы; /elon всем текст — в группы и в личку всем, кто писал боту; /elon личка текст — только в личку. Ответом на фото или видео — оно с этой подписью. Сначала придёт образец, потом /elon да — отправить, /elon нет — отменить.",
		en: "📢 Announcement: /elon text — to every group; /elon everyone text — to the groups and the private chat of everyone who wrote to the bot; /elon private text — to private chats only. In reply to a photo or a video — it under that caption. A preview comes first, then /elon yes sends it, /elon no drops it.",
	}
	textAnnounceDrafted = localText{
		uz: "👆 Namuna shu.\n%s\n\nYuborish: /elon ha · Bekor: /elon bekor",
		ru: "👆 Вот образец.\n%s\n\nОтправить: /elon да · Отменить: /elon нет",
		en: "👆 That is the preview.\n%s\n\nSend: /elon yes · Drop: /elon no",
	}
	textAnnounceDraftedButtons = localText{
		uz: "👆 Gemini so'rovni to'xtatgani uchun e'lonni o'zim yozdim, namuna shu.\n%s",
		ru: "👆 Gemini остановил запрос, поэтому объявление я написала сама, вот образец.\n%s",
		en: "👆 Gemini stopped the request, so I wrote the announcement myself; that is the preview.\n%s",
	}
	textAnnounceSendButton = localText{uz: "✅ Yuborish", ru: "✅ Отправить", en: "✅ Send"}
	textAnnounceDropButton = localText{uz: "❌ Bekor", ru: "❌ Отмена", en: "❌ Drop"}
	textAnnounceGroups     = localText{
		uz: "%d ta guruhga ketadi: %s",
		ru: "Уйдёт в %d групп: %s",
		en: "It goes to %d groups: %s",
	}
	textAnnouncePrivate = localText{
		uz: "👤 %d ta lichkaga ketadi (botga yozganlar).",
		ru: "👤 Уйдёт в %d личных чатов (тем, кто писал боту).",
		en: "👤 It goes to %d private chats (those who wrote to the bot).",
	}
	textAnnounceUnreached = localText{
		uz: "👤 %d ta lichkaga yetmadi (botni bloklagan).",
		ru: "👤 Не дошло в %d личных чатов (бот заблокирован).",
		en: "👤 %d private chats were not reached (the bot is blocked).",
	}
	textAnnounceNoPreview = localText{
		uz: "Namunani yubora olmadim: %s\nVideo yoki rasmga reply qilganingizni tekshiring.",
		ru: "Не смогла отправить образец: %s\nПроверьте, что это ответ на фото или видео.",
		en: "I could not send the preview: %s\nCheck that it replies to a photo or a video.",
	}
	textAnnounceSent = localText{
		uz: "✅ %d ta guruhga yuborildi.",
		ru: "✅ Отправлено в %d групп.",
		en: "✅ Sent to %d groups.",
	}
	textAnnounceFailed = localText{
		uz: "Yuborilmadi: %s",
		ru: "Не отправлено: %s",
		en: "Not sent: %s",
	}
	textAnnounceCancelled = localText{
		uz: "E'lon bekor qilindi.",
		ru: "Объявление отменено.",
		en: "The announcement is dropped.",
	}
	textAnnounceRefused = localText{
		uz: "E'lon bo'lmadi: %s",
		ru: "Объявление не вышло: %s",
		en: "The announcement failed: %s",
	}
)
