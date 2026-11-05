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
	Reward: func(level int, damage, maxHP int64, sword int) int64 { return damage * int64(sword) },
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

	// Alice's sword is better by the kill: her reward follows it.
	if _, err := store.pool.Exec(ctx, `UPDATE raid_heroes SET sword = 3 WHERE user_id = $1`, alice); err != nil {
		t.Fatal(err)
	}
	hit, err = store.HitRaid(ctx, raidChat, bob, "Bob", 5, now.Add(2*time.Second), testRaidRules, 11)
	if err != nil || hit.Killed == nil || hit.Damage != 20 {
		t.Fatalf("the killing hit = %+v, %v; it does only the 20 HP left", hit, err)
	}
	if hit.Boss.Level != 2 || hit.Boss.HP != 200+1000 || hit.Member.Damage != 0 {
		t.Fatalf("after the kill the boss is %+v and Bob's damage %d", hit.Boss, hit.Member.Damage)
	}
	fighters := hit.Killed.Fighters
	if len(fighters) != 2 || fighters[0].UserID != bob || fighters[0].Damage != 60 || fighters[0].Reward != 60 || fighters[1].Reward != 40*3 {
		t.Fatalf("fighters = %+v", fighters)
	}
	if hit.Hero.Coins != 4+5+60 {
		t.Errorf("Bob's coins = %d, want his taps and his reward", hit.Hero.Coins)
	}
	_, hero, me, top, _ = store.RaidState(ctx, raidChat, alice, testRaidRules, 7, 5)
	if hero.Coins != 4+40*3 || me.Damage != 0 || me.Total != 40 || len(top) != 0 {
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
		h.BuffUntil, h.BuffReady, h.UltCharge, h.UltArmed = at, at.Add(time.Minute), 7, true
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
	if !saved.BuffUntil.Equal(at) || !saved.BuffReady.Equal(at.Add(time.Minute)) || saved.UltCharge != 7 || !saved.UltArmed || saved.Coins != 40 ||
		!saved.QuestDay.Equal(day) || saved.QuestSkills != 2 || saved.QuestClaimed != 5 || saved.Streak != 3 || !saved.StreakDay.Equal(day) {
		t.Errorf("saved %+v, changed %+v", saved, hero)
	}
}

// Migration 56 keeps coins the same part of a sword's next level at its new
// price: the strongest lose the most, and the first sword nothing.
func TestRaidPricesMigration(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	prices, err := migrationFiles.ReadFile("migrations/0056_raid_prices.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO raid_heroes (user_id, seed, sword, coins) VALUES (1, 1, 1, 500), (2, 2, 26, 1338250)`,
		string(prices),
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	var first, strong int64
	err = store.pool.QueryRow(ctx, `SELECT (SELECT coins FROM raid_heroes WHERE user_id = 1), (SELECT coins FROM raid_heroes WHERE user_id = 2)`).Scan(&first, &strong)
	if err != nil || first != 500 || strong != 266575 {
		t.Errorf("coins = %d and %d, %v", first, strong, err)
	}
}

// Migration 54 sets a total no sword could reach to what the taps do on
// average, the boss back to the level its active members can kill, which
// they start afresh, and Patrick's coins to a tenth of his sword's next
// level; a total that could be reached is kept.
func TestRaidRecalculateMigration(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	recalculate, err := migrationFiles.ReadFile("migrations/0054_raid_recalculate.sql")
	if err != nil {
		t.Fatal(err)
	}
	const patrick = 862862027
	for _, q := range []string{
		`INSERT INTO raid_heroes (user_id, seed, sword, crit, coins, taps) VALUES
			(1, 1, 22, 15, 261224, 12133), (862862027, 2, 58, 15, 7246756405316, 70071), (3, 3, 20, 12, 14152, 36661)`,
		`INSERT INTO raid_members (chat_id, user_id, name, level, damage, total) VALUES
			(-100, 1, 'owner', 68, 35142, 99996563953848), (-100, 862862027, 'Patrick', 68, 14941564498, 2138043371981),
			(-100, 3, 'Kafka', 68, 5000, 300000000)`,
		`INSERT INTO raid_bosses (chat_id, level, hp, max_hp) VALUES (-100, 68, 452274748421, 467216312919)`,
		string(recalculate),
	} {
		if _, err := store.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	totals := map[int64]int64{}
	rows, _ := store.pool.Query(ctx, `SELECT user_id, total, level, damage FROM raid_members`)
	for rows.Next() {
		var id, total int64
		var level, damage int
		if err := rows.Scan(&id, &total, &level, &damage); err != nil {
			t.Fatal(err)
		}
		totals[id] = total
		if level >= 68 || damage != 0 {
			t.Errorf("member %d at level %d with damage %d after the boss was set back", id, level, damage)
		}
	}
	if totals[1] != 142120572 || totals[patrick] != 2138043371981 || totals[3] != 300000000 {
		t.Errorf("totals = %v", totals)
	}
	var level int
	var hp, maxHP, coins int64
	if err := store.pool.QueryRow(ctx, `SELECT level, hp, max_hp FROM raid_bosses WHERE chat_id = -100`).Scan(&level, &hp, &maxHP); err != nil || level >= 68 || level < 20 || hp != maxHP {
		t.Errorf("boss = level %d, %d/%d, %v", level, hp, maxHP, err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT coins FROM raid_heroes WHERE user_id = $1`, patrick).Scan(&coins); err != nil || coins >= 1_000_000_000_000 || coins < 800_000_000_000 {
		t.Errorf("Patrick's coins = %d, %v", coins, err)
	}
}
