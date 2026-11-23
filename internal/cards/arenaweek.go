package cards

import (
	"context"
	"fmt"
	"time"

	"telegram-ai-bot/internal/storage"
)

// ArenaWeekPrizes are the arena's weekly prizes in card tokens, as the
// owners asked on 2026-11-19: the top 100 who attacked in the week, Monday
// to Monday in Tashkent, by rating. The top 3 get more since 2026-11-21, as
// the owners asked: 100, 75 and 50 tokens with 3, 2 and 1 diamond boxes,
// and the fourth and fifth 25 tokens.
var ArenaWeekPrizes = []storage.ArenaPrize{
	{From: 1, To: 1, Tokens: 100, Boxes: 3, Box: arenaWeekBox},
	{From: 2, To: 2, Tokens: 75, Boxes: 2, Box: arenaWeekBox},
	{From: 3, To: 3, Tokens: 50, Boxes: 1, Box: arenaWeekBox},
	{From: 4, To: 5, Tokens: 25},
	{From: 6, To: 10, Tokens: 20},
	{From: 11, To: 30, Tokens: 15},
	{From: 31, To: 50, Tokens: 10},
	{From: 51, To: 100, Tokens: 5},
}

// ArenaEventWeek is the week of the arena's box event, as the owners asked
// on 2026-11-20: that week its top 5 get more diamond boxes, 4 to the first,
// 3 to the second, 2 to the third and 1 each to the fourth and fifth; the
// tokens are the week's as ever.
var ArenaEventWeek = time.Date(2026, 11, 16, 0, 0, 0, 0, Location)

// arenaEventPrizes are ArenaWeekPrizes with the event's boxes.
var arenaEventPrizes = []storage.ArenaPrize{
	{From: 1, To: 1, Tokens: 100, Boxes: 4, Box: arenaWeekBox},
	{From: 2, To: 2, Tokens: 75, Boxes: 3, Box: arenaWeekBox},
	{From: 3, To: 3, Tokens: 50, Boxes: 2, Box: arenaWeekBox},
	{From: 4, To: 5, Tokens: 25, Boxes: 1, Box: arenaWeekBox},
	{From: 6, To: 10, Tokens: 20},
	{From: 11, To: 30, Tokens: 15},
	{From: 31, To: 50, Tokens: 10},
	{From: 51, To: 100, Tokens: 5},
}

// arenaWeekPrizes are the prizes of the week that began at week.
func arenaWeekPrizes(week time.Time) []storage.ArenaPrize {
	if week.Equal(ArenaEventWeek) {
		return arenaEventPrizes
	}
	return ArenaWeekPrizes
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
	since := until.AddDate(0, 0, -7)
	players, _, err := store.PayArenaWeek(ctx, since, until, arenaWeekPrizes(since), arenaWeekNote)
	return players, err
}

func arenaWeekNote(place int) string {
	return fmt.Sprintf("🏆 Arena haftalik mukofoti: %d-o'rin", place)
}

// arenaWeekBox is the box the event's top places get: the diamond box.
const arenaWeekBox = "box"

// arenaWeekPrize is place's prize among prizes, none for no place.
func arenaWeekPrize(prizes []storage.ArenaPrize, place int) storage.ArenaPrize {
	for _, p := range prizes {
		if place >= p.From && place <= p.To {
			return p
		}
	}
	return storage.ArenaPrize{}
}

// payArenaWeek pays the week that ended, the first time any player opens the
// arena or attacks after Monday's midnight, before the new week's battles
// move a rating, as the owners asked on 2026-11-22 that prizes go at 00:00;
// the daily cron pays it too, if no one came.
func (h *handler) payArenaWeek(ctx context.Context) {
	week := arenaWeekStart(h.Now()).Unix()
	if h.paidWeek.Load() == week {
		return
	}
	if _, err := PayArenaWeek(ctx, h.Store, h.Now()); err != nil {
		h.Logger.Warn("could not pay the arena week", "error", err)
		return
	}
	h.paidWeek.Store(week)
}
