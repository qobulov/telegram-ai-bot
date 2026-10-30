package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

const raidChat = -100500

// testRaidRules make a boss of 100 HP times its level, each tap 10 damage
// times the sword, at most 5 taps counted, and a reward of the damage.
var testRaidRules = RaidRules{
	MaxHP: func(level int) int64 { return 100 * int64(level) },
	Strike: func(hero RaidHero, taps int, since time.Duration) (int, int64) {
		counted := min(taps, 5)
		return counted, int64(counted) * 10 * int64(hero.Sword)
	},
	Reward:     func(level int, damage, maxHP int64) int64 { return damage },
	ScoreEvery: time.Minute,
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
	if hit.Boss.Level != 2 || hit.Boss.HP != 200 || hit.Member.Damage != 0 {
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
	var wg sync.WaitGroup
	kills := make(chan int, 20)
	for i := range 20 {
		wg.Go(func() {
			hit, err := store.HitRaid(ctx, raidChat, alice+int64(i), "P", 5, time.Now(), testRaidRules, 3)
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
