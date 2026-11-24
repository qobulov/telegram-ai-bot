package service

import (
	"context"
	"time"
)

// FailureLog counts, per chat, the replies lost to an error, a timeout or
// an overloaded model.
type FailureLog interface {
	NoteFailure(ctx context.Context, chatID int64, now time.Time, quiet time.Duration) (int, error)
}

const (
	// failureNotices is how often a chat is told, in a run of failures,
	// that the bot could not answer, each time with a joke, as the owners
	// asked on 2026-11-24; after that it stays quiet until the run ends.
	failureNotices = 3
	// failureQuiet ends a run of failures for a chat.
	failureQuiet = time.Hour
)

// The kinds of failure a reply is lost to.
const (
	failureTimeout = iota
	failureOverloaded
	failureError
)

// textsFailure are the jokes each kind of failure is told with, in turn.
var textsFailure = [3][failureNotices]localText{
	failureTimeout: {
		{
			uz: "Uzoq o'ylab, miyam qotib qoldi 😅 Yana bir marta yozing.",
			ru: "Задумалась так, что зависла 😅 Напишите ещё раз.",
			en: "Thought so hard I froze 😅 Write it once more.",
		},
		{
			uz: "Fikrlarim tirbandlikda qolib ketyapti 🚗 Birozdan keyin qayta yozing.",
			ru: "Мысли застряли в пробке 🚗 Напишите чуть позже.",
			en: "My thoughts are stuck in traffic 🚗 Try again in a bit.",
		},
		{
			uz: "Uchinchi marta qotdim, biroz dam olib kelaman 😴 Keyinroq yozing.",
			ru: "Зависла в третий раз, пойду отдохну 😴 Напишите позже.",
			en: "Froze for the third time, taking a little nap 😴 Write later.",
		},
	},
	failureOverloaded: {
		{
			uz: "Hamma birdan menga yozyapti, navbat uzun 😅 Bir daqiqadan keyin yozing.",
			ru: "Мне пишут все сразу, очередь длинная 😅 Напишите через минуту.",
			en: "Everyone's writing to me at once, the queue is long 😅 Try in a minute.",
		},
		{
			uz: "Serverlarim ter to'kyapti 🥵 Bir daqiqa sabr qiling.",
			ru: "Мои серверы вспотели 🥵 Минутку терпения.",
			en: "My servers are sweating 🥵 Give me a minute.",
		},
		{
			uz: "Hali ham band, kofe ichib kelaman ☕ Keyinroq yozing.",
			ru: "Всё ещё занята, схожу за кофе ☕ Напишите позже.",
			en: "Still busy, going for a coffee ☕ Write later.",
		},
	},
	failureError: {
		{
			uz: "Simlarim chalkashib ketdi 🤯 Qayta yozib ko'ring.",
			ru: "Провода запутались 🤯 Попробуйте ещё раз.",
			en: "My wires got tangled 🤯 Try again.",
		},
		{
			uz: "Yana xato, bugun texnika menga qarshi 😤 Birozdan keyin yozing.",
			ru: "Опять ошибка, техника сегодня против меня 😤 Напишите чуть позже.",
			en: "Another error, the tech is against me today 😤 Try again in a bit.",
		},
		{
			uz: "Uchinchi xato, endi jim turib o'zimni ta'mirlayman 🔧 Keyinroq yozing.",
			ru: "Третья ошибка, теперь молча чиню себя 🔧 Напишите позже.",
			en: "Third error, I'll quietly fix myself now 🔧 Write later.",
		},
	},
}

// memoryFailures counts in this process when no FailureLog is set.
type memoryFailures struct{ memoryAILimits }

func (m *memoryFailures) NoteFailure(ctx context.Context, chatID int64, now time.Time, quiet time.Duration) (int, error) {
	return m.NoteAILimit(ctx, chatID, now, quiet)
}

// failureNotice is the joke a reply lost to kind is told with in this chat,
// or "" once the chat was told failureNotices times in this run.
func (s *Service) failureNotice(in incoming, kind int) string {
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	n, err := s.failures.NoteFailure(ctx, in.scope.ChatID, s.opts.Now(), failureQuiet)
	if err != nil {
		s.opts.Logger.Warn("could not count a failure notice", "chat_id", in.scope.ChatID, "error", err)
		n = 1
	}
	if n > failureNotices {
		return ""
	}
	return textsFailure[kind][n-1].to(in)
}
