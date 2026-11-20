package service

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"time"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// The anti-spam guards, on in every group unless its admins turn them off
// with /antispam: the flood guard, the spam lists, and the newcomer guard,
// which deletes the links, ads and channel posts of a member who has
// written fewer than newcomerMessages messages in the group, as its log
// keeps them.
const (
	antispamFlood     = "flood"
	antispamSpam      = "spam"
	antispamNewcomers = "newcomers"
	// newcomerMessages is how many logged messages make a member no longer
	// new. The log keeps about a month, so a member silent that long is new
	// again, as spammers' sleeping accounts are.
	newcomerMessages = 5
	// newcomerMemory is how long a newcomer's deleted link counts: a second
	// one within it mutes them for newcomerMute.
	newcomerMemory = time.Hour
	newcomerMute   = time.Hour
	// antispamData starts an /antispam button: "antispam:flood".
	antispamData = "antispam:"
)

// antispamGuards are the guards /antispam shows, in its order, with their
// buttons' names.
var antispamGuards = []struct{ key, name, says string }{
	{antispamFlood, "Flood", "Flood: 10 soniyada 10 ta, 30 soniyada 20 ta xabar yoki 1 daqiqada bir xil xabar/stiker 3 marta — xabarlari o'chiriladi va mut: 10 daqiqa, keyin 1 soat, keyin 1 kun"},
	{antispamSpam, "Spamerlar", "Spamerlar ro'yxati (CAS, lols.bot): ro'yxatdagilar guruhdan chiqariladi"},
	{antispamNewcomers, "Yangi a'zolar", "Yangi a'zolar: guruhda 5 tadan kam yozganlarning havola, reklama, tugmali xabar va kanal postlari o'chiriladi, ikkinchisida 1 soat mut"},
}

// antispamOff are the anti-spam guards a group turned off, and whether its
// setting could be read: when it cannot, the guards wait rather than act
// against an admin's off. Without a store none is off.
func (s *Service) antispamOff(ctx context.Context, chatID int64) (map[string]bool, bool) {
	if s.opts.Extras == nil {
		return nil, true
	}
	value, err := s.opts.Extras.GroupSetting(ctx, chatID, storage.SettingAntispamOff)
	if err != nil {
		s.opts.Logger.Warn("guards: could not read the anti-spam setting", "chat_id", chatID, "error", err)
		return nil, false
	}
	off := map[string]bool{}
	for _, key := range strings.Split(value, ",") {
		if key != "" {
			off[key] = true
		}
	}
	return off, true
}

// promotes reports a message a newcomer may not send: a link other than to
// the group itself, an @mention asking to subscribe, a post forwarded from
// a channel, or buttons.
func promotes(msg *telegram.Message) bool {
	if msg.HasButtons() {
		return true
	}
	if origin := msg.ForwardOrigin; origin != nil && origin.Type == "channel" && (origin.Chat == nil || origin.Chat.ID != msg.Chat.ID) {
		return true
	}
	text, entities := msg.Content()
	lower := strings.ToLower(text)
	asksToSubscribe := strings.Contains(lower, "obuna") || strings.Contains(lower, "обуна") || strings.Contains(lower, "подпис") || strings.Contains(lower, "subscribe")
	for _, e := range entities {
		switch e.Type {
		case "url", "text_link":
			link := e.URL
			if e.Type == "url" {
				link, _ = telegram.EntityText(text, e)
			}
			if link != "" && !ownGroupLink(link, msg.Chat.Username) {
				return true
			}
		case "mention":
			name, _ := telegram.EntityText(text, e)
			if asksToSubscribe && !strings.EqualFold(strings.TrimPrefix(name, "@"), msg.Chat.Username) {
				return true
			}
		}
	}
	return false
}

// ownGroupLink reports a link to the group itself, t.me/<its username>.
func ownGroupLink(link, username string) bool {
	if username == "" {
		return false
	}
	if !strings.Contains(link, "://") {
		link = "https://" + link
	}
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	first, _, _ := strings.Cut(strings.Trim(u.Path, "/"), "/")
	return (host == "t.me" || host == "telegram.me") && strings.EqualFold(first, username)
}

// isNewcomer reports a member with fewer than newcomerMessages messages in
// the group's log. When the log cannot say, they are not, as in a group
// without AI, whose messages are not logged.
func (s *Service) isNewcomer(ctx context.Context, msg *telegram.Message) bool {
	if !s.groupAIOn(ctx, msg.Chat.ID) {
		return false
	}
	n, err := s.ambient.log.AuthorMessageCount(ctx, msg.Chat.ID, msg.From.ID, newcomerMessages)
	if err != nil {
		s.opts.Logger.Warn("guards: could not count a member's messages", "chat_id", msg.Chat.ID, "error", err)
		return false
	}
	return n < newcomerMessages
}

// stopNewcomerLink deletes a newcomer's link and warns them; a second within
// newcomerMemory mutes them for newcomerMute, when the bot may mute.
func (s *Service) stopNewcomerLink(ctx context.Context, in incoming, mayMute bool) {
	msg := in.msg
	since := s.opts.Now().Add(-newcomerMemory)
	warned, muted := 0, 0
	if s.opts.Strikes != nil {
		warned, _ = s.opts.Strikes.SwearStrikes(ctx, msg.Chat.ID, msg.From.ID, storage.ActionNewcomerLink, since)
		muted, _ = s.opts.Strikes.SwearStrikes(ctx, msg.Chat.ID, msg.From.ID, storage.ActionNewcomerMute, since)
	}
	if !s.recordGuard(ctx, in, storage.ActionNewcomerLink, msg.From.ID) {
		return
	}
	if err := s.opts.GroupModerator.DeleteMessage(ctx, msg.Chat.ID, msg.MessageID); err != nil {
		s.opts.Logger.Warn("guards: could not delete a newcomer's link", "chat_id", msg.Chat.ID, "error", err)
		return
	}
	s.opts.Logger.Info("guards: newcomer link deleted", "chat_id", msg.Chat.ID, "user_id", msg.From.ID, "warned_before", warned)
	switch {
	case muted > 0:
		return
	case warned == 0 || !mayMute:
		s.postGuard(msg, "⚠️ "+mentionOf(msg.From)+", yangi a'zolar havola, reklama va kanal postlarini yubora olmaydi: xabaringiz o'chirildi. Avval guruhda suhbatlashing.")
		return
	}
	if !s.recordGuard(ctx, in, storage.ActionNewcomerMute, msg.From.ID) {
		return
	}
	err := s.opts.Restrictor.RestrictChatMember(ctx, telegram.Restriction{
		ChatID: msg.Chat.ID, UserID: msg.From.ID, Permissions: telegram.NoPermissions(), Until: s.opts.Now().Add(newcomerMute),
	})
	if err != nil {
		s.opts.Logger.Warn("guards: could not mute a newcomer", "chat_id", msg.Chat.ID, "error", err)
		return
	}
	s.postGuard(msg, "🔇 "+mentionOf(msg.From)+" yana havola yubordi: "+durationWords(newcomerMute)+" yozolmaydi.")
}

// antispamCommand answers /antispam in a group, switching the guards: with
// every guard on it turns them all off, and otherwise all on, so that it is
// sent once to turn it on and again to turn it off, with no word after it;
// the menu that follows has a button for each guard. "on" or "off" after it
// still sets them all so.
func (s *Service) antispamCommand(ctx context.Context, in incoming) outcome {
	if allowed, err := s.mayModerate(ctx, in, "group"); err != nil || !allowed {
		return say("⛔ Antispamni guruh adminlari yoki moderatorlar sozlaydi.")
	}
	chatID := in.scope.ChatID
	off, ok := s.antispamOff(ctx, chatID)
	if !ok {
		return say(textError.to(in))
	}
	turnOn := len(off) > 0
	switch strings.ToLower(strings.TrimSpace(commandArgument(in.text))) {
	case "on", "yoq", "ha":
		turnOn = true
	case "off", "o'chir", "ochir", "yo'q":
		turnOn = false
	}
	all := map[string]bool{antispamFlood: true, antispamSpam: true, antispamNewcomers: true}
	if turnOn {
		all = nil
	}
	if !s.setAntispamOff(ctx, chatID, all, in.userID) {
		return say(textError.to(in))
	}
	text, buttons, ok := s.antispamMenu(ctx, chatID)
	if !ok {
		return say(textError.to(in))
	}
	state := "❌ Antispam o'chirildi. Yana /antispam yuborsangiz, yoqiladi."
	if turnOn {
		state = "✅ Antispam yoqildi. Yana /antispam yuborsangiz, o'chadi."
	}
	return outcome{text: state + "\n\n" + text, buttons: buttons}
}

// setAntispamOff keeps the guards a group turned off and forgets its
// guards, so that the change holds at once in this instance.
func (s *Service) setAntispamOff(ctx context.Context, chatID int64, off map[string]bool, by int64) bool {
	var keys []string
	for _, g := range antispamGuards {
		if off[g.key] {
			keys = append(keys, g.key)
		}
	}
	if err := s.opts.Extras.SetGroupSetting(ctx, chatID, storage.SettingAntispamOff, strings.Join(keys, ","), by); err != nil {
		s.opts.Logger.Warn("could not keep the anti-spam setting", "chat_id", chatID, "error", err)
		return false
	}
	s.guardSets.forget(chatID)
	return true
}

// antispamMenu is /antispam's text and buttons for a group.
func (s *Service) antispamMenu(ctx context.Context, chatID int64) (string, [][]telegram.Button, bool) {
	off, ok := s.antispamOff(ctx, chatID)
	if !ok {
		return "", nil, false
	}
	lines := []string{"🛡 Antispam"}
	var row []telegram.Button
	for _, g := range antispamGuards {
		mark := "✅"
		if off[g.key] {
			mark = "❌"
		}
		lines = append(lines, mark+" "+g.says)
		row = append(row, telegram.Button{Text: mark + " " + g.name, Data: antispamData + g.key})
	}
	if restrict, del := s.botMay(ctx, chatID); !restrict || !del {
		lines = append(lines, "", "⚠️ Ishlashi uchun menga xabarlarni o'chirish va a'zolarni cheklash huquqlarini bering.")
	}
	lines = append(lines, "", "Tugmani bossangiz, o'sha himoya yoqiladi yoki o'chadi. Adminlarga tegilmaydi.")
	return strings.Join(lines, "\n"), [][]telegram.Button{row}, true
}

// pressedAntispam switches the guard pressed, for an admin or a moderator,
// and shows the menu again; anyone else gets a box.
func (s *Service) pressedAntispam(q *telegram.CallbackQuery) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	answer := func(text string, alert bool) { _ = s.opts.Buttons.AnswerCallbackQuery(ctx, q.ID, text, alert) }
	chatID, key := q.Message.Chat.ID, strings.TrimPrefix(q.Data, antispamData)
	known := slices.ContainsFunc(antispamGuards, func(g struct{ key, name, says string }) bool { return g.key == key })
	if !known {
		answer("Bu tugma eskirgan.", true)
		return
	}
	if allowed, err := s.userMayModerate(ctx, chatID, q.From, "group"); err != nil || !allowed {
		answer("Antispamni guruh adminlari yoki moderatorlar sozlaydi.", true)
		return
	}
	off, ok := s.antispamOff(ctx, chatID)
	if !ok {
		answer("Hozir bo'lmadi.", true)
		return
	}
	if off == nil {
		off = map[string]bool{}
	}
	off[key] = !off[key]
	if !s.setAntispamOff(ctx, chatID, off, q.From.ID) {
		answer("Hozir bo'lmadi.", true)
		return
	}
	if off[key] {
		answer("❌ O'chirildi", false)
	} else {
		answer("✅ Yoqildi", false)
	}
	if text, buttons, ok := s.antispamMenu(ctx, chatID); ok {
		_ = s.opts.Buttons.EditMessageMenu(ctx, chatID, q.Message.MessageID, text, buttons)
	}
}
