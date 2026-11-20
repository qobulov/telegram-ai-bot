package cards

import (
	"context"
	"fmt"
	"time"

	"telegram-ai-bot/internal/storage"
)

// ArenaWeekPrizes are the arena's weekly prizes, as the owners asked on
// 2026-11-19: card tokens to the top 100 who attacked in the week, Monday
// to Monday in Tashkent, by rating; and since 2026-11-20 diamond boxes to
// the top 5: 3 to the first, 2 to the second, 1 to the third to fifth.
var ArenaWeekPrizes = []storage.ArenaPrize{
	{From: 1, To: 1, Tokens: 50, Boxes: 3, Box: arenaWeekBox},
	{From: 2, To: 2, Tokens: 40, Boxes: 2, Box: arenaWeekBox},
	{From: 3, To: 3, Tokens: 30, Boxes: 1, Box: arenaWeekBox},
	{From: 4, To: 5, Tokens: 20, Boxes: 1, Box: arenaWeekBox},
	{From: 6, To: 10, Tokens: 20},
	{From: 11, To: 30, Tokens: 15},
	{From: 31, To: 50, Tokens: 10},
	{From: 51, To: 100, Tokens: 5},
}

// ArenaWeekPayer pays an arena week.
type ArenaWeekPayer interface {
	PayArenaWeek(ctx context.Context, since, until time.Time, prizes []storage.ArenaPrize, note func(place int) string) (int, bool, error)
}

// arenaWeekStart is the Monday midnight in Tashkent that began now's week.
func arenaWeekStart(now time.Time) time.Time {
	day := DayStart(now)
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

// PayArenaWeek pays the last week ended by now, if it is not paid yet, and
// gives the players paid.
func PayArenaWeek(ctx context.Context, store ArenaWeekPayer, now time.Time) (int, error) {
	until := arenaWeekStart(now)
	players, _, err := store.PayArenaWeek(ctx, until.AddDate(0, 0, -7), until, ArenaWeekPrizes, arenaWeekNote)
	return players, err
}

func arenaWeekNote(place int) string {
	return fmt.Sprintf("🏆 Arena haftalik mukofoti: %d-o'rin", place)
}

// arenaWeekBox is the box the week's top places get: the diamond box.
const arenaWeekBox = "box"

// arenaWeekBoxes is the diamond boxes for place this week, 0 for none.
func arenaWeekBoxes(place int) int {
	for _, p := range ArenaWeekPrizes {
		if place >= p.From && place <= p.To {
			return p.Boxes
		}
	}
	return 0
}

// arenaWeekPrize is the tokens for place this week, 0 for none.
func arenaWeekPrize(place int) int {
	for _, p := range ArenaWeekPrizes {
		if place >= p.From && place <= p.To {
			return p.Tokens
		}
	}
	return 0
}
