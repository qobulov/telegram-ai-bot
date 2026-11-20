package service

import (
	"context"
	"sync"
	"time"
)

// AILimitLog counts, per chat, the replies lost to the AI's credits or
// quota running out, so that a chat is told about it a few times and not
// at every message.
type AILimitLog interface {
	NoteAILimit(ctx context.Context, chatID int64, now time.Time, quiet time.Duration) (int, error)
}

const (
	// aiLimitNotices is how often a chat is told, in one outage, that the
	// AI cannot answer, as the owners asked on 2026-11-20; after that the
	// bot stays quiet until the outage ends.
	aiLimitNotices = 3
	// aiLimitQuiet ends an outage for a chat: a failure after this long
	// without one counts from the first notice again.
	aiLimitQuiet = 6 * time.Hour
)

// textsAILimit are the notices in turn: the first plain, the second and
// third joking.
var textsAILimit = [aiLimitNotices]localText{
	{
		uz: "Hozir javob bera olmayman: AI limitim tugadi. Egalarim tez orada tuzatishadi.",
		ru: "Сейчас не могу ответить: у меня закончился лимит ИИ. Мои хозяева скоро это исправят.",
		en: "I can't answer right now: my AI limit has run out. My owners will fix it soon.",
	},
	{
		uz: "Hali ham limit yo'q 😅 Miyamning batareyasi nolda, egalarim quvvatlagich qidirishyapti.",
		ru: "Лимита всё ещё нет 😅 Батарейка в голове на нуле, хозяева ищут зарядку.",
		en: "Still no limit 😅 My brain's battery is at zero, my owners are looking for the charger.",
	},
	{
		uz: "Uchinchi marta aytyapman, limit tugagan 🙃 Endi egalarim meni \"ovqatlantirmaguncha\" jim turaman.",
		ru: "Говорю в третий раз: лимит закончился 🙃 Теперь молчу, пока хозяева меня не «покормят».",
		en: "Third time telling you: the limit is gone 🙃 I'll stay quiet now until my owners \"feed\" me.",
	},
}

// memoryAILimits counts in this process when no AILimitLog is set.
type memoryAILimits struct {
	mu    sync.Mutex
	chats map[int64]aiLimitCount
}

type aiLimitCount struct {
	warned int
	last   time.Time
}

func (m *memoryAILimits) NoteAILimit(_ context.Context, chatID int64, now time.Time, quiet time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.chats == nil {
		m.chats = map[int64]aiLimitCount{}
	}
	c := m.chats[chatID]
	if c.last.Before(now.Add(-quiet)) {
		c.warned = 0
	}
	c.warned, c.last = min(c.warned+1, 1000), now
	m.chats[chatID] = c
	return c.warned, nil
}

// aiLimitNotice is the notice for a reply lost to the AI's limit in this
// chat, or "" once the chat was told aiLimitNotices times.
func (s *Service) aiLimitNotice(in incoming) string {
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	n, err := s.aiLimits.NoteAILimit(ctx, in.scope.ChatID, s.opts.Now(), aiLimitQuiet)
	if err != nil {
		s.opts.Logger.Warn("could not count an AI limit notice", "chat_id", in.scope.ChatID, "error", err)
		n = 1
	}
	if n > aiLimitNotices {
		return ""
	}
	return textsAILimit[n-1].to(in)
}
