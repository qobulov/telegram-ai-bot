package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"telegram-ai-bot/internal/tools"
)

// announceCommand is an owner's /elon: send_to_groups run in code, without
// the model, whose provider may refuse a sale's words or a video's picture.
// "/elon <text>" drafts a post for every group, in reply to a photo or a
// video that post under the text as its caption, the owner seeing a
// preview first; "/elon ha" sends it, "/elon bekor" drops it.
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
	if rest == "" && in.msg.ReplyToMessage == nil {
		return say(textAnnounceUsage.to(in))
	}
	args, _ := json.Marshal(map[string]any{"action": "prepare", "text": rest, "copy": in.msg.ReplyToMessage != nil})
	raw, failure := s.runAnnounce(ctx, in, string(args))
	if failure != "" {
		return say(failure)
	}
	var draft struct {
		Groups       []string `json:"groups"`
		Text         string   `json:"text"`
		PreviewError string   `json:"preview_error"`
	}
	_ = json.Unmarshal(raw, &draft)
	if draft.PreviewError != "" {
		return say(fmt.Sprintf(textAnnounceNoPreview.to(in), draft.PreviewError))
	}
	text := fmt.Sprintf(textAnnounceDrafted.to(in), len(draft.Groups), strings.Join(draft.Groups, ", "))
	if draft.Text != "" {
		text = draft.Text + "\n\n———\n" + text
	}
	return say(text)
}

func (s *Service) sendAnnouncement(ctx context.Context, in incoming) outcome {
	raw, failure := s.runAnnounce(ctx, in, `{"action":"confirm"}`)
	if failure != "" {
		return say(failure)
	}
	var sent struct {
		Sent   int      `json:"sent"`
		Failed []string `json:"failed"`
	}
	_ = json.Unmarshal(raw, &sent)
	text := fmt.Sprintf(textAnnounceSent.to(in), sent.Sent)
	if len(sent.Failed) > 0 {
		text += "\n" + fmt.Sprintf(textAnnounceFailed.to(in), strings.Join(sent.Failed, ", "))
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
		uz: "📢 E'lon: /elon matn — barcha guruhlarga. Rasm yoki videoga reply qilib /elon tavsif — o'sha video shu tavsif bilan. Avval namunasi keladi, keyin /elon ha — yuborish, /elon bekor — bekor qilish.",
		ru: "📢 Объявление: /elon текст — во все группы. Ответом на фото или видео /elon описание — оно с этой подписью. Сначала придёт образец, потом /elon да — отправить, /elon нет — отменить.",
		en: "📢 Announcement: /elon text — to every group. In reply to a photo or a video, /elon caption — it under that caption. A preview comes first, then /elon yes sends it, /elon no drops it.",
	}
	textAnnounceDrafted = localText{
		uz: "👆 Namuna shu. %d ta guruhga ketadi: %s\n\nYuborish: /elon ha · Bekor: /elon bekor",
		ru: "👆 Вот образец. Уйдёт в %d групп: %s\n\nОтправить: /elon да · Отменить: /elon нет",
		en: "👆 That is the preview. It goes to %d groups: %s\n\nSend: /elon yes · Drop: /elon no",
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
