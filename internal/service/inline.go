package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"telegram-ai-bot/internal/agent"
	"telegram-ai-bot/internal/ai"
	"telegram-ai-bot/internal/storage"
	"telegram-ai-bot/internal/telegram"
)

// InlineAnswerer answers inline queries, "@bot words" typed in any chat.
type InlineAnswerer interface {
	AnswerInlineQuery(ctx context.Context, answer telegram.InlineAnswer) error
}

const (
	// inlineDeadline bounds everything before the answer: the checks and
	// the model's turn. Telegram drops an answer to a query older than
	// about ten seconds, and inlineSendTimeout must still fit after it.
	inlineDeadline    = 7 * time.Second
	inlineSendTimeout = 2500 * time.Millisecond
	// Results are cached briefly, per person, so that a question typed
	// again is not asked again; a failure is not cached at all, so that
	// trying again asks again.
	inlineAnswerCache = 30
	inlineHintCache   = 300
	// inlineSlots bounds the inline questions answered at once while
	// polling: each holds a goroutine and a model call.
	inlineSlots = 4
)

// inlineEnds are the marks a question must end with to be answered: the
// app sends a query at nearly every keystroke, and answering each would
// spend a model call, and the asker's daily answers, on every unfinished
// word.
const inlineEnds = "?!.…"

// The texts of inline results. Each article has a title, shown in the
// list, and a text, posted when it is picked.
var (
	textInlineTitleHint = localText{
		uz: "Savolingizni yozing va oxiriga ? qo'ying",
		ru: "Напишите вопрос и поставьте в конце ?",
		en: "Type your question and end it with ?",
	}
	textInlineHint = localText{
		uz: "Inline rejimda menga savol bering: @%s, keyin savol va oxirida ? yoki ! — javob shu yerda chiqadi.",
		ru: "Спросите меня в инлайн-режиме: @%s, затем вопрос и в конце ? или ! — ответ появится здесь.",
		en: "Ask me inline: @%s, then your question, ending with ? or ! — the answer shows up right here.",
	}
	textInlineRetry = localText{
		uz: "Javob ulgurmadi — qayta urinib ko'ring",
		ru: "Ответ не успел — попробуйте ещё раз",
		en: "The answer didn't make it in time — try again",
	}
	textInlineNeedsTools = localText{
		uz: "Bunga bu yerda javob berolmayman: buni bilish uchun qidirishim kerak. Menga shaxsiyda yozing.",
		ru: "Здесь я на это не отвечу: нужно поискать. Напишите мне в личку.",
		en: "I can't answer that here: it needs looking up. Write to me in a private chat.",
	}
	textInlineClosed = localText{
		uz: "Inline rejimda javob bermayman.",
		ru: "В инлайн-режиме я не отвечаю.",
		en: "I don't answer in inline mode.",
	}
)

// handleInline answers an inline query off the polling loop, which must not
// wait for a model. When too many are already being answered it is dropped:
// the asker sees no results and may type again.
func (s *Service) handleInline(q *telegram.InlineQuery) {
	select {
	case s.inlines <- struct{}{}:
	default:
		s.opts.Logger.Warn("too many inline queries at once; dropping one")
		return
	}
	s.background.Go(func() {
		defer func() { <-s.inlines }()
		s.answerInline(q)
	})
}

// answerInline answers one inline query with one article: Maria's answer
// to a finished question, as a private chat would get it but without its
// history or tools, and counted against the same daily answers; otherwise
// a hint or why there is no answer. The asker is who Telegram says sent the
// query, as for a message.
func (s *Service) answerInline(q *telegram.InlineQuery) {
	if s.opts.Inline == nil || q.From == nil || q.From.IsBot {
		return
	}
	in := inlineIncoming(q)
	ctx, cancel := context.WithTimeout(context.Background(), s.inlineDeadline)
	article, cache := s.inlineResult(ctx, in)
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), inlineSendTimeout)
	defer cancel()
	answer := telegram.InlineAnswer{QueryID: q.ID, Articles: []telegram.InlineArticle{article}, CacheSeconds: cache}
	if err := s.opts.Inline.AnswerInlineQuery(ctx, answer); err != nil {
		s.opts.Logger.Warn("could not answer an inline query", "user_id", in.userID, "error", err)
	}
}

// inlineIncoming is an inline query as the private chat with its asker,
// which is what PRIVATE_CHATS, the channels and the daily answers count.
func inlineIncoming(q *telegram.InlineQuery) incoming {
	text := strings.TrimSpace(q.Query)
	return incoming{
		msg:    &telegram.Message{From: q.From, Chat: telegram.Chat{ID: q.From.ID, Type: telegram.ChatTypePrivate}, Text: text},
		scope:  storage.Scope{ChatID: q.From.ID, UserID: q.From.ID},
		userID: q.From.ID,
		text:   text,
	}
}

// inlineResult is the article to answer with and how long Telegram may
// keep it.
func (s *Service) inlineResult(ctx context.Context, in incoming) (telegram.InlineArticle, int) {
	switch {
	case !s.answersPrivately(in.msg.From):
		return s.inlineNotice(in, textInlineClosed.to(in)), inlineHintCache
	case !finished(in.text):
		hint := telegram.InlineArticle{ID: "hint", Title: textInlineTitleHint.to(in), Text: fmt.Sprintf(textInlineHint.to(in), s.opts.Bot.Username)}
		return hint, inlineHintCache
	}
	if _, limited := s.rateLimited(in); limited {
		return s.inlineNotice(in, textRateLimited.to(in)), 0
	}
	if notice, joined := s.joinedChannels(ctx, in); !joined {
		return s.inlineNotice(in, notice.text), 0
	}
	reservation, _, allowed := s.takePrivateAnswer(ctx, in)
	if !allowed {
		return s.inlineNotice(in, fmt.Sprintf(textDailyLimit.to(in), s.opts.PrivateDailyLimit)), 0
	}
	answer, err := s.opts.Replier.Reply(ctx, s.inlineRequest(in))
	if err != nil || answer.Text == "" {
		// The asker got no answer, whatever the model did.
		s.giveBackAnswer(in, reservation)
		return s.inlineNotice(in, s.inlineFailure(in, err)), 0
	}
	s.opts.Logger.Info("inline query answered", "user_id", in.userID, "route", answer.Route)
	return telegram.InlineArticle{ID: "answer", Title: s.opts.DisplayName, Text: answer.Text}, inlineAnswerCache
}

// inlineRequest is the question for the agent: the asker's words, with the
// owner's notice for an owner and the language to answer in.
func (s *Service) inlineRequest(in incoming) agent.Request {
	notice := ""
	if s.isOwner(in.msg.From) {
		notice = ownerNotice
	}
	return agent.Request{
		Scope:   in.scope,
		UserID:  in.userID,
		Text:    bracketless.Replace(in.text),
		Context: paragraphs(notice, languageHint(in.text)),
		Inline:  true,
	}
}

// inlineFailure says, for the asker, why there is no answer.
func (s *Service) inlineFailure(in incoming, err error) string {
	switch {
	case errors.Is(err, agent.ErrNeedsTools):
		return textInlineNeedsTools.to(in)
	case ai.IsOverloaded(err):
		return textOverloaded.to(in)
	case err == nil, errors.Is(err, context.DeadlineExceeded), errors.Is(err, agent.ErrSilent), errors.Is(err, ai.ErrEmptyResponse):
		return textInlineRetry.to(in)
	}
	s.opts.Logger.Error("inline turn failed", "user_id", in.userID, "error", err)
	return textError.to(in)
}

// inlineNotice is an article that says text, as its title and as what it
// posts.
func (s *Service) inlineNotice(in incoming, text string) telegram.InlineArticle {
	return telegram.InlineArticle{ID: "notice", Title: s.opts.DisplayName, Description: text, Text: text}
}

// finished reports a question that ends with one of inlineEnds.
func finished(question string) bool {
	last, _ := utf8.DecodeLastRuneInString(question)
	return question != "" && strings.ContainsRune(inlineEnds, last)
}
