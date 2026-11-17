package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// GroupExtras keeps what a group's members and admins set up for it:
// reputation, saved answers, its log chat and its other settings.
type GroupExtras interface {
	GroupSetting(ctx context.Context, chatID int64, key string) (string, error)
	SetGroupSetting(ctx context.Context, chatID int64, key, value string, by int64) error
	GiveRep(ctx context.Context, chatID, from, to int64) (int, error)
	RepOf(ctx context.Context, chatID, userID int64) (points, place int, err error)
	RepTop(ctx context.Context, chatID int64, limit int) ([]storage.MemberRep, error)
	SaveNote(ctx context.Context, n storage.GroupNote) error
	Note(ctx context.Context, chatID int64, name string) (storage.GroupNote, bool, error)
	NoteNames(ctx context.Context, chatID int64) ([]string, error)
	DeleteNote(ctx context.Context, chatID int64, name string) (bool, error)
}

// Copier copies a message, as a saved answer with a picture is sent back.
type Copier interface {
	CopyMessage(ctx context.Context, chatID, fromChatID, messageID int64, caption string) (int64, error)
}

// JournalStore reads where a group's log goes, and the group's title.
type JournalStore interface {
	GroupSetting(ctx context.Context, chatID int64, key string) (string, error)
	GroupTitle(ctx context.Context, chatID int64) (string, error)
}

// GroupJournal writes a line to a group's log chat, when it has one: what
// a moderator or the bot did there. A failure is logged and nothing more.
func GroupJournal(store JournalStore, sender Sender, logger *slog.Logger) func(ctx context.Context, chatID int64, line string) {
	return func(ctx context.Context, chatID int64, line string) {
		logChat, err := store.GroupSetting(ctx, chatID, storage.SettingLogChat)
		if err != nil || logChat == "" {
			return
		}
		to, err := strconv.ParseInt(logChat, 10, 64)
		if err != nil {
			return
		}
		title, _ := store.GroupTitle(ctx, chatID)
		if title == "" {
			title = strconv.FormatInt(chatID, 10)
		}
		if _, err := sender.SendMessage(ctx, telegram.Outgoing{ChatID: to, Text: "📋 " + cardMarks.Replace(title) + "\n" + line}); err != nil {
			logger.Warn("could not write to a group's log", "chat_id", chatID, "error", err)
		}
	}
}

// journal writes a line to the group's log, if the service has one.
func (s *Service) journal(chatID int64, line string) {
	if s.opts.Journal == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	s.opts.Journal(ctx, chatID, line)
}

// repWords thank the member replied to, and give them a point.
var repWords = map[string]bool{
	"+": true, "++": true, "+++": true, "+1": true, "+rep": true, "+реп": true,
	"rahmat": true, "raxmat": true, "rahmet": true, "raxmet": true, "rakhmat": true, "rahmatt": true, "raxmatt": true,
	"рахмат": true, "раҳмат": true, "рахмет": true, "рахматт": true,
	"спасибо": true, "спасиб": true, "спасибки": true, "спс": true, "спсб": true, "пасиба": true, "благодарю": true, "spasibo": true, "spс": true, "sps": true,
	"thanks": true, "thank": true, "thx": true, "tnx": true, "ty": true, "respect": true, "респект": true, "barakalla": true, "баракалла": true,
}

// repFillers may follow a thanks: "rahmat aka", "katta rahmat", "спс бро".
var repFillers = map[string]bool{
	"katta": true, "kotta": true, "катта": true, "aka": true, "ака": true, "uka": true, "ука": true, "opa": true, "опа": true,
	"bro": true, "бро": true, "brat": true, "брат": true, "you": true, "a lot": true, "большое": true, "огромное": true, "sizga": true, "сизга": true,
	"senga": true, "сенга": true, "jon": true, "жон": true, "do'stim": true, "dostim": true, "mari": true, "мари": true,
}

// isThanks reports a short thanks: a thanks word, with at most two
// fillers, punctuation and emoji around it.
func isThanks(text string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r == ' ' || r == ',' || r == '!' || r == '.' || r == ')' || r == '(' || unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r)
	})
	if len(words) == 0 || len(words) > 3 {
		return false
	}
	thanked := false
	for _, w := range words {
		switch {
		case repWords[w]:
			thanked = true
		case !repFillers[w]:
			return false
		}
	}
	return thanked
}

// thanked gives a point to the member a "+" or "rahmat" replies to, marking
// the thanks with a reaction; reports whether the message was one.
func (s *Service) thanked(in incoming) bool {
	replied := in.msg.ReplyToMessage
	text, _ := in.msg.Content()
	if s.opts.Extras == nil || replied == nil || replied.From == nil || replied.From.IsBot || replied.SenderChat != nil ||
		replied.From.ID == in.userID || in.fromBot || !isThanks(text) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	_, err := s.opts.Extras.GiveRep(ctx, in.msg.Chat.ID, in.userID, replied.From.ID)
	switch {
	case errors.Is(err, storage.ErrRepTooSoon):
		return true
	case err != nil:
		s.opts.Logger.Warn("could not give reputation", "chat_id", in.msg.Chat.ID, "error", err)
		return true
	}
	if s.opts.Reactor != nil && !s.isMutedLately(in.msg.Chat.ID) {
		// A muted bot still counts the thanks, without a word or a mark.
		_ = s.opts.Reactor.SetMessageReaction(ctx, in.msg.Chat.ID, in.msg.MessageID, "👍")
	}
	return true
}

// repShown is how many members /toprep lists.
const repShown = 10

// reputation answers /rep, of the asker or the member replied to.
func (s *Service) reputation(ctx context.Context, in incoming) outcome {
	who := in.msg.From
	if r := in.msg.ReplyToMessage; r != nil && r.From != nil && !r.From.IsBot {
		who = r.From
	}
	points, place, err := s.opts.Extras.RepOf(ctx, in.scope.ChatID, who.ID)
	if err != nil {
		return say(textError.to(in))
	}
	name := cardMarks.Replace(doozName(who))
	if points == 0 {
		return say("⭐ " + name + ": hali reputatsiya yo'q. Foydali xabarga reply qilib \"+\" yoki \"rahmat\" yozilsa, +1 bo'ladi.")
	}
	return say(fmt.Sprintf("⭐ %s: %d reputatsiya, guruhda %d-o'rin.", name, points, place))
}

// reputationTop answers /toprep: the group's most thanked members.
func (s *Service) reputationTop(ctx context.Context, in incoming) outcome {
	top, err := s.opts.Extras.RepTop(ctx, in.scope.ChatID, repShown)
	if err != nil {
		return say(textError.to(in))
	}
	if len(top) == 0 {
		return say("⭐ Hali hech kimda reputatsiya yo'q. Foydali xabarga reply qilib \"+\" yoki \"rahmat\" yozing.")
	}
	lines := []string{"⭐ Reputatsiya reytingi"}
	for i, m := range top {
		lines = append(lines, fmt.Sprintf("%d. %s — %d", i+1, memberLabel(m.Name, m.Username, m.UserID), m.Points))
	}
	return say(strings.Join(lines, "\n"))
}

// noteName is a saved answer's name: letters, digits and _, up to 32.
var noteName = regexp.MustCompile(`^[\p{L}\p{N}_]{1,32}$`)

// noteCalled is the note a "#qoidalar" or "!donat" message calls, "" for
// any other message.
func noteCalled(text string) string {
	text = strings.TrimSpace(text)
	if len(text) < 2 || (text[0] != '#' && text[0] != '!') {
		return ""
	}
	name := strings.ToLower(text[1:])
	if !noteName.MatchString(name) {
		return ""
	}
	return name
}

// callsNote reports a message calling a note the group has, made into
// "/note name" for respond.
func (s *Service) callsNote(in incoming) (incoming, bool) {
	text, _ := in.msg.Content()
	name := noteCalled(text)
	if s.opts.Extras == nil || name == "" || in.fromBot {
		return in, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if _, found, err := s.opts.Extras.Note(ctx, in.msg.Chat.ID, name); err != nil || !found {
		return in, false
	}
	in.text, in.trigger = "/note "+name, triggerCommand
	return in, true
}

// showNote sends a saved answer: its message copied when it had a picture
// or the like, else its text.
func (s *Service) showNote(ctx context.Context, in incoming) outcome {
	name := strings.ToLower(strings.TrimLeft(strings.TrimSpace(commandArgument(in.text)), "#!"))
	note, found, err := s.opts.Extras.Note(ctx, in.scope.ChatID, name)
	switch {
	case err != nil:
		return say(textError.to(in))
	case !found:
		return say("📭 Bunday saqlangan javob yo'q. Ro'yxat: /notes")
	}
	if note.MediaKind == "copy" && s.opts.Copier != nil {
		if source, err := strconv.ParseInt(note.FileID, 10, 64); err == nil {
			if _, err := s.opts.Copier.CopyMessage(ctx, in.scope.ChatID, in.scope.ChatID, source, ""); err == nil {
				return outcome{}
			}
		}
		if note.Text == "" {
			return say("Bu javobning asl xabari o'chirilgan: uni /save " + name + " bilan qayta saqlang.")
		}
	}
	return say(note.Text)
}

// saveNote answers /save name [text]: the text, or the message replied to,
// is kept under name, by those whose rank opens notes.
func (s *Service) saveNote(ctx context.Context, in incoming) outcome {
	if allowed, err := s.mayModerate(ctx, in, "notes"); err != nil || !allowed {
		return say("⛔ Javob saqlash adminlar va moderatorlar uchun.")
	}
	name, text, _ := strings.Cut(strings.TrimSpace(commandArgument(in.text)), " ")
	name = strings.ToLower(strings.TrimLeft(name, "#!"))
	if !noteName.MatchString(name) {
		return say("Masalan: /save qoidalar Reklama taqiqlanadi. Yoki xabarga reply qilib: /save qoidalar")
	}
	note := storage.GroupNote{ChatID: in.scope.ChatID, Name: name, Text: strings.TrimSpace(text), SavedBy: in.userID}
	if r := in.msg.ReplyToMessage; r != nil && note.Text == "" {
		content, _ := r.Content()
		note.Text = strings.TrimSpace(content)
		if r.Text == "" {
			// A picture, sticker, GIF, video or file: the message itself is
			// copied back.
			note.MediaKind, note.FileID = "copy", strconv.FormatInt(r.MessageID, 10)
		}
	}
	if note.Text == "" && note.MediaKind == "" {
		return say("Nimani saqlay? Matnini yozing yoki xabarga reply qiling: /save " + name)
	}
	switch err := s.opts.Extras.SaveNote(ctx, note); {
	case errors.Is(err, storage.ErrTooManyNotes):
		return say(fmt.Sprintf("⛔ Guruhda ko'pi bilan %d ta saqlangan javob bo'ladi: keraksizini /clear bilan o'chiring.", storage.MaxNotes))
	case err != nil:
		return say(textError.to(in))
	}
	s.journal(in.scope.ChatID, "📝 "+plainLabel(in.msg.From)+": #"+name+" javobi saqlandi")
	return say("✅ Saqlandi: #" + name + " yoki !" + name + " deb yozilsa chiqaraman.")
}

// listNotes answers /notes: the group's saved answers.
func (s *Service) listNotes(ctx context.Context, in incoming) outcome {
	names, err := s.opts.Extras.NoteNames(ctx, in.scope.ChatID)
	if err != nil {
		return say(textError.to(in))
	}
	if len(names) == 0 {
		return say("📭 Saqlangan javoblar yo'q. Admin saqlaydi: /save qoidalar Reklama taqiqlanadi")
	}
	for i, n := range names {
		names[i] = "#" + n
	}
	return say("📝 Saqlangan javoblar (yozing, chiqaraman):\n" + strings.Join(names, "  "))
}

// clearNote answers /clear name.
func (s *Service) clearNote(ctx context.Context, in incoming) outcome {
	if allowed, err := s.mayModerate(ctx, in, "notes"); err != nil || !allowed {
		return say("⛔ Javobni adminlar va moderatorlar o'chiradi.")
	}
	name := strings.ToLower(strings.TrimLeft(strings.TrimSpace(commandArgument(in.text)), "#!"))
	removed, err := s.opts.Extras.DeleteNote(ctx, in.scope.ChatID, name)
	switch {
	case err != nil:
		return say(textError.to(in))
	case !removed:
		return say("📭 #" + name + " degan saqlangan javob yo'q. Ro'yxat: /notes")
	}
	s.journal(in.scope.ChatID, "🗑 "+plainLabel(in.msg.From)+": #"+name+" javobi o'chirildi")
	return say("🗑 #" + name + " o'chirildi.")
}

// setLog answers /setlog: the chat whose ID is given, or that a forwarded
// message came from, gets the group's log; "/setlog off" stops it.
func (s *Service) setLog(ctx context.Context, in incoming) outcome {
	if allowed, err := s.mayModerate(ctx, in, "group"); err != nil || !allowed {
		return say("⛔ Jurnalni guruh egasi yoki 4-darajali moderator ulaydi.")
	}
	arg := strings.ToLower(strings.TrimSpace(commandArgument(in.text)))
	if arg == "off" || arg == "o'chir" || arg == "ochir" {
		if err := s.opts.Extras.SetGroupSetting(ctx, in.scope.ChatID, storage.SettingLogChat, "", in.userID); err != nil {
			return say(textError.to(in))
		}
		return say("📋 Jurnal o'chirildi.")
	}
	target, ok := logTarget(in.msg, arg)
	if !ok {
		return say("📋 Jurnal: botni kanal yoki guruhga admin qilib qo'shing, u yerda /id yozib ID'ni oling, keyin bu yerda /setlog -100… yozing. Yoki o'sha kanaldan bir xabarni forward qilib, unga reply qilib /setlog yozing. O'chirish: /setlog off")
	}
	title := in.msg.Chat.Title
	if _, err := s.opts.Sender.SendMessage(ctx, telegram.Outgoing{ChatID: target, Text: "📋 Bu chat «" + cardMarks.Replace(title) + "» guruhining jurnali bo'ldi: ban, mut, shikoyat, kirgan-chiqqanlar shu yerga yoziladi."}); err != nil {
		return say("⛔ U chatga yoza olmadim: botni o'sha kanal yoki guruhga qo'shib, yozish huquqini bering.")
	}
	if err := s.opts.Extras.SetGroupSetting(ctx, in.scope.ChatID, storage.SettingLogChat, strconv.FormatInt(target, 10), in.userID); err != nil {
		return say(textError.to(in))
	}
	return say("✅ Jurnal ulandi: endi moderatsiya amallari u chatga yoziladi.")
}

// logTarget is the chat a /setlog names: an ID, or the origin of the
// forwarded message it replies to.
func logTarget(msg *telegram.Message, arg string) (int64, bool) {
	if id, err := strconv.ParseInt(arg, 10, 64); err == nil && id < 0 && id != msg.Chat.ID {
		return id, true
	}
	if r := msg.ReplyToMessage; r != nil && r.ForwardOrigin != nil {
		if c := r.ForwardOrigin.Chat; c != nil && c.ID != msg.Chat.ID {
			return c.ID, true
		}
		if c := r.ForwardOrigin.SenderChat; c != nil && c.ID != msg.Chat.ID {
			return c.ID, true
		}
	}
	return 0, false
}

// setCleanService answers "/cleanservice on|off": whether the group's
// service messages ("X joined", "Y pinned") are deleted.
func (s *Service) setCleanService(ctx context.Context, in incoming) outcome {
	if allowed, err := s.mayModerate(ctx, in, "group"); err != nil || !allowed {
		return say("⛔ Buni guruh egasi yoki 4-darajali moderator sozlaydi.")
	}
	switch strings.ToLower(strings.TrimSpace(commandArgument(in.text))) {
	case "on", "yoq", "ha":
		if err := s.opts.Extras.SetGroupSetting(ctx, in.scope.ChatID, storage.SettingCleanService, "on", in.userID); err != nil {
			return say(textError.to(in))
		}
		return say("🧹 Endi \"kirdi\", \"chiqdi\", \"qadadi\" kabi xizmat xabarlarini o'chiraman. Menga xabarlarni o'chirish huquqi kerak.")
	case "off", "o'chir", "yo'q":
		if err := s.opts.Extras.SetGroupSetting(ctx, in.scope.ChatID, storage.SettingCleanService, "", in.userID); err != nil {
			return say(textError.to(in))
		}
		return say("🧹 Xizmat xabarlari endi o'chirilmaydi.")
	}
	value, _ := s.opts.Extras.GroupSetting(ctx, in.scope.ChatID, storage.SettingCleanService)
	state := "o'chiq"
	if value == "on" {
		state = "yoqilgan"
	}
	return say("🧹 Xizmat xabarlarini tozalash: " + state + ". Yoqish: /cleanservice on, o'chirish: /cleanservice off")
}

// cleanedService deletes a service message in a group that asked for it,
// reporting whether it was one: joins, leaves, pins, new titles and photos.
func (s *Service) cleanedService(msg *telegram.Message) bool {
	if s.opts.Extras == nil || s.opts.GroupModerator == nil || !msg.IsService() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	value, err := s.opts.Extras.GroupSetting(ctx, msg.Chat.ID, storage.SettingCleanService)
	if err != nil || value != "on" {
		return false
	}
	if err := s.opts.GroupModerator.DeleteMessage(ctx, msg.Chat.ID, msg.MessageID); err != nil {
		s.opts.Logger.Debug("could not delete a service message", "chat_id", msg.Chat.ID, "error", err)
		return false
	}
	return true
}

// journalMembers writes joins and leaves to the group's log.
func (s *Service) journalMembers(msg *telegram.Message) {
	if s.opts.Journal == nil {
		return
	}
	for i := range msg.NewChatMembers {
		joined := &msg.NewChatMembers[i]
		line := "➕ Kirdi: " + plainLabel(joined) + " (" + strconv.FormatInt(joined.ID, 10) + ")"
		if msg.From != nil && msg.From.ID != joined.ID {
			line += ", qo'shgan: " + plainLabel(msg.From)
		}
		s.journal(msg.Chat.ID, line)
	}
	if left := msg.LeftChatMember; left != nil {
		s.journal(msg.Chat.ID, "➖ Chiqdi: "+plainLabel(left)+" ("+strconv.FormatInt(left.ID, 10)+")")
	}
}
