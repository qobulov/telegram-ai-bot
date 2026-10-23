package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"telegram-ai-bot/internal/storage"
)

// heroMemory is how long a group's open round, or the lack of one, is
// trusted before the database is asked again: every message of a group is
// checked against it.
const heroMemory = 10 * time.Second

// HeroGuesses checks guesses in "guess the hero": the round's answer is in
// the database, never with the model.
type HeroGuesses interface {
	OpenHeroGame(ctx context.Context, chatID int64, now time.Time) (storage.HeroGame, bool, error)
	WinHeroGame(ctx context.Context, chatID, userID int64, name string, now time.Time) (int, bool, error)
}

// guessedHero answers a group message that names the hero of the group's
// open round, the first one to do so wins the point, and reports whether
// it did: such a message needs no other answer.
func (s *Service) guessedHero(in incoming) bool {
	if s.opts.HeroGuesses == nil || in.fromBot || in.msg.From == nil {
		return false
	}
	text, _ := in.msg.Content()
	if text = strings.TrimSpace(text); text == "" || strings.HasPrefix(text, "/") || s.isMutedLately(in.scope.ChatID) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), logTimeout)
	defer cancel()
	game, ok := s.heroRounds.get(in.scope.ChatID)
	if !ok {
		open, found, err := s.opts.HeroGuesses.OpenHeroGame(ctx, in.scope.ChatID, s.opts.Now())
		if err != nil {
			s.opts.Logger.Warn("could not read the hero game", "chat_id", in.scope.ChatID, "error", err)
			return false
		}
		if !found {
			open = storage.HeroGame{}
		}
		game = open
		s.heroRounds.set(in.scope.ChatID, game)
	}
	if game.Hero == "" || !s.opts.Now().Before(game.EndsAt) || !namesHero(text, game.Hero) {
		return false
	}
	name := plainLabel(in.msg.From)
	points, won, err := s.opts.HeroGuesses.WinHeroGame(ctx, in.scope.ChatID, in.userID, name, s.opts.Now())
	if err != nil {
		s.opts.Logger.Warn("could not settle the hero game", "chat_id", in.scope.ChatID, "error", err)
		return false
	}
	s.heroRounds.set(in.scope.ChatID, storage.HeroGame{})
	if !won {
		return false
	}
	s.reply(in, fmt.Sprintf(textHeroWon.to(in), in.msg.From.FirstName, game.Hero, points))
	if s.ambient.enabled() {
		s.remember(in.msg)
	}
	return true
}

// namesHero reports a message that names the hero, in any case, with or
// without the hero's punctuation and spaces: "x borg", "xborg" and "Bu
// X.Borg" all name X.Borg.
func namesHero(text, hero string) bool {
	words, heroWords := alnumWords(text), alnumWords(hero)
	if len(heroWords) == 0 {
		return false
	}
	joined := strings.Join(heroWords, "")
	for i := range words {
		for j := i + 1; j <= len(words) && j-i <= len(heroWords)+1; j++ {
			if strings.Join(words[i:j], "") == joined {
				return true
			}
		}
	}
	return slices.Equal(words, heroWords)
}

// alnumWords are text's runs of letters and digits, in lower case.
func alnumWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
