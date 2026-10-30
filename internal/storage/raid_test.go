package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

const raidChat = -100500

// testRaidRules make a boss of 100 HP times its level, 1000 more for each
// active member past the first, each tap 10 damage times the sword, at most
// 5 taps counted, and a reward of the damage.
var testRaidRules = RaidRules{
	MaxHP: func(level, active int) int64 { return 100*int64(level) + 1000*int64(active-1) },
	Strike: func(hero *RaidHero, taps int, since time.Duration, now time.Time) (int, int64) {
		counted := min(taps, 5)
		return counted, int64(counted) * 10 * int64(hero.Sword)
	},
	Reward: func(level int, damage, maxHP int64) int64 { return damage },
	Day: func(now time.Time) time.Time {
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	},
	ScoreEvery: time.Minute,
	Active:     time.Hour,
}

// Members hit the chat's boss together; the hit that takes its last HP
// kills it, rewards everyone who hit it by their damage, and brings the
// next level's boss.
func TestRaidBossIsHitAndKilledTogether(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	boss, hero, me, top, err := store.RaidState(ctx, raidChat, alice, testRaidRules, 7, 5)
	if err != nil || boss.Level != 1 || boss.HP != 100 || hero.Seed != 7 || hero.Sword != 1 || me.Damage != 0 || len(top) != 0 {
		t.Fatalf("RaidState = %+v %+v %+v %+v, %v", boss, hero, me, top, err)
	}
	hit, err := store.HitRaid(ctx, raidChat, alice, "Alice", 3, now, testRaidRules, 9)
	if err != nil || hit.Damage != 30 || hit.Boss.HP != 70 || hit.Hero.Coins != 3 || hit.Hero.Taps != 3 || hit.Hero.Seed != 7 || !hit.ScoreDue {
		t.Fatalf("Alice's hit = %+v, %v", hit, err)
	}
	if hit, err = store.HitRaid(ctx, raidChat, bob, "Bob", 4, now, testRaidRules, 11); err != nil || hit.Boss.HP != 30 || hit.Member.Damage != 40 {
		t.Fatalf("Bob's hit = %+v, %v", hit, err)
	}
	if hit, err = store.HitRaid(ctx, raidChat, alice, "Alice", 1, now.Add(time.Second), testRaidRules, 9); err != nil || hit.ScoreDue || hit.Member.Damage != 40 {
		t.Fatalf("Alice's second hit = %+v, %v; her score was set a second ago", hit, err)
	}
	_, _, _, top, _ = store.RaidState(ctx, raidChat, alice, testRaidRules, 7, 5)
	if len(top) != 2 || top[0].Damage != 40 || top[1].Damage != 40 {
		t.Fatalf("top = %+v", top)
	}

	hit, err = store.HitRaid(ctx, raidChat, bob, "Bob", 5, now.Add(2*time.Second), testRaidRules, 11)
	if err != nil || hit.Killed == nil || hit.Damage != 20 {
		t.Fatalf("the killing hit = %+v, %v; it does only the 20 HP left", hit, err)
	}
	if hit.Boss.Level != 2 || hit.Boss.HP != 200+1000 || hit.Member.Damage != 0 {
		t.Fatalf("after the kill the boss is %+v and Bob's damage %d", hit.Boss, hit.Member.Damage)
	}
	fighters := hit.Killed.Fighters
	if len(fighters) != 2 || fighters[0].UserID != bob || fighters[0].Damage != 60 || fighters[0].Reward != 60 || fighters[1].Reward != 40 {
		t.Fatalf("fighters = %+v", fighters)
	}
	if hit.Hero.Coins != 4+5+60 {
		t.Errorf("Bob's coins = %d, want his taps and his reward", hit.Hero.Coins)
	}
	_, hero, me, top, _ = store.RaidState(ctx, raidChat, alice, testRaidRules, 7, 5)
	if hero.Coins != 4+40 || me.Damage != 0 || me.Total != 40 || len(top) != 0 {
		t.Errorf("Alice after the kill: hero %+v, member %+v, top %+v", hero, me, top)
	}
}

// Many hits at once kill each boss once: 20 hits of 50 kill the bosses of
// 100, 200, 300 and 400 HP, no more and none twice.
func TestRaidBossIsKilledOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	rules := testRaidRules
	rules.MaxHP = func(level, _ int) int64 { return 100 * int64(level) }
	var wg sync.WaitGroup
	kills := make(chan int, 20)
	for i := range 20 {
		wg.Go(func() {
			hit, err := store.HitRaid(ctx, raidChat, alice+int64(i), "P", 5, time.Now(), rules, 3)
			if err != nil {
				t.Error(err)
				return
			}
			if hit.Killed != nil {
				kills <- hit.Killed.Level
			}
		})
	}
	wg.Wait()
	close(kills)
	seen := map[int]bool{}
	for level := range kills {
		if seen[level] {
			t.Fatalf("level %d killed twice", level)
		}
		seen[level] = true
	}
	if len(seen) != 4 || !seen[1] || !seen[4] {
		t.Fatalf("bosses killed at levels %v, want 1 to 4", seen)
	}
}

// An upgrade costs coins and stops at its highest level.
func TestRaidHeroIsUpgraded(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	cost := func(level int) int64 { return 2 }
	if _, err := store.UpgradeRaidHero(ctx, alice, "sword", cost, 3, 1); !errors.Is(err, ErrRaidCoins) {
		t.Fatalf("an upgrade without coins = %v", err)
	}
	if _, err := store.HitRaid(ctx, raidChat, alice, "Alice", 5, time.Now(), testRaidRules, 1); err != nil {
		t.Fatal(err)
	}
	hero, err := store.UpgradeRaidHero(ctx, alice, "sword", cost, 3, 1)
	if err != nil || hero.Sword != 2 || hero.Coins != 3 {
		t.Fatalf("UpgradeRaidHero = %+v, %v", hero, err)
	}
	if hero, err = store.UpgradeRaidHero(ctx, alice, "sword", cost, 3, 1); err != nil || hero.Sword != 3 {
		t.Fatalf("the second upgrade = %+v, %v", hero, err)
	}
	if _, err := store.UpgradeRaidHero(ctx, alice, "sword", cost, 3, 1); !errors.Is(err, ErrRaidMax) {
		t.Fatalf("an upgrade past the highest level = %v", err)
	}
	if _, err := store.UpgradeRaidHero(ctx, alice, "coins", cost, 3, 1); err == nil {
		t.Fatal("an upgrade of something that is not one")
	}
}

// A player's place is by damage on the boss and in all, 0 before they hit.
func TestRaidRanks(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	if levelRank, totalRank, top, err := store.RaidRanks(ctx, raidChat, alice, 1, 5); err != nil || levelRank != 0 || totalRank != 0 || len(top) != 0 {
		t.Fatalf("before any hit: %d %d %+v, %v", levelRank, totalRank, top, err)
	}
	for _, h := range []struct {
		user int64
		taps int
	}{{alice, 2}, {bob, 5}, {bob + 1, 3}} {
		if _, err := store.HitRaid(ctx, raidChat, h.user, "P", h.taps, now, testRaidRules, 1); err != nil {
			t.Fatal(err)
		}
	}
	levelRank, totalRank, top, err := store.RaidRanks(ctx, raidChat, alice, 1, 2)
	if err != nil || levelRank != 3 || totalRank != 3 || len(top) != 2 || top[0].UserID != bob || top[0].Total != 50 {
		t.Fatalf("Alice's ranks = %d %d %+v, %v", levelRank, totalRank, top, err)
	}
	if levelRank, _, _, _ := store.RaidRanks(ctx, raidChat, bob, 2, 2); levelRank != 0 {
		t.Errorf("Bob ranks %d on a boss he never hit", levelRank)
	}
}

// The next boss is as strong as the chat's members who hit lately, and the
// kill counts in each fighter's quests for the day.
func TestRaidKillCountsInQuestsAndActiveMembers(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	if _, err := store.HitRaid(ctx, raidChat, bob, "Bob", 5, now, testRaidRules, 1); err != nil {
		t.Fatal(err)
	}
	hit, err := store.HitRaid(ctx, raidChat, alice, "Alice", 5, now, testRaidRules, 1)
	if err != nil || hit.Killed == nil {
		t.Fatalf("the kill = %+v, %v", hit, err)
	}
	if hit.Boss.MaxHP != 200+1000 {
		t.Errorf("the next boss has %d HP, want level 2's for two active members", hit.Boss.MaxHP)
	}
	if hit.Hero.QuestKills != 1 || !hit.Hero.QuestDay.Equal(testRaidRules.Day(now)) {
		t.Errorf("Alice's quests = %+v", hit.Hero)
	}
	bobs, err := store.ChangeRaidHero(ctx, bob, 1, func(h *RaidHero) error { return nil })
	if err != nil || bobs.QuestKills != 1 || bobs.Coins != 5+50 || !bobs.QuestDay.Equal(testRaidRules.Day(now)) {
		t.Errorf("Bob after the kill = %+v, %v", bobs, err)
	}
}

// A change to a hero is saved whole, skills and quests too; one refused
// leaves the hero as it was.
func TestChangeRaidHero(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	at := time.Now().Truncate(time.Second)
	day := testRaidRules.Day(at)
	hero, err := store.ChangeRaidHero(ctx, alice, 9, func(h *RaidHero) error {
		h.StormUntil, h.FireLeft, h.FireReady = at, 7, at.Add(time.Minute)
		h.QuestDay, h.QuestSkills, h.QuestClaimed, h.Streak, h.StreakDay = day, 2, 5, 3, day
		h.Coins = 40
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("no")
	if _, err := store.ChangeRaidHero(ctx, alice, 9, func(h *RaidHero) error { h.Coins = 0; return refused }); !errors.Is(err, refused) {
		t.Fatalf("a refused change = %v", err)
	}
	_, saved, _, _, _ := store.RaidState(ctx, raidChat, alice, testRaidRules, 9, 5)
	if !saved.StormUntil.Equal(at) || saved.FireLeft != 7 || !saved.FireReady.Equal(at.Add(time.Minute)) || saved.Coins != 40 ||
		!saved.QuestDay.Equal(day) || saved.QuestSkills != 2 || saved.QuestClaimed != 5 || saved.Streak != 3 || !saved.StreakDay.Equal(day) {
		t.Errorf("saved %+v, changed %+v", saved, hero)
	}
}
