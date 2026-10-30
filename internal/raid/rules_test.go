package raid

import (
	"encoding/json"
	"errors"
	"math"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

// Taps count up to what a person taps in the time since the last hit, at
// most two seconds' worth and a few more.
func TestStrikeCountsWhatAPersonTaps(t *testing.T) {
	hero := storage.RaidHero{Sword: 1, Crit: 0, Seed: 12345}
	for _, tc := range []struct {
		taps  int
		since time.Duration
		want  int
	}{
		{3, time.Second, 3},
		{50, 500 * time.Millisecond, TapsPerSecond/2 + tapBurst},
		{50, time.Hour, 2*TapsPerSecond + tapBurst},
		{50, -time.Second, tapBurst},
		{-5, time.Second, 0},
	} {
		h := hero
		if counted, _ := Strike(&h, tc.taps, tc.since, time.Now()); counted != tc.want {
			t.Errorf("%d taps after %v counted %d, want %d", tc.taps, tc.since, counted, tc.want)
		}
	}
	if _, damage := Strike(&hero, 0, time.Second, time.Now()); damage != 0 {
		t.Errorf("no taps did %d", damage)
	}
}

// About the chance's share of taps are critical, each three blows' damage.
func TestCriticalBlows(t *testing.T) {
	hero := storage.RaidHero{Sword: 5, Crit: 5, Seed: 987654321}
	crits := 0
	for tap := range int64(10000) {
		if Critical(hero.Seed, tap, CritChance(hero.Crit)) {
			crits++
		}
	}
	if crits < 1800 || crits > 2200 {
		t.Errorf("%d of 10000 taps critical at %d%%", crits, CritChance(hero.Crit))
	}
	_, damage := Strike(&hero, 4, time.Second, time.Now())
	var want int64
	for tap := range int64(4) {
		blow := TapDamage(5)
		if Critical(hero.Seed, tap, CritChance(5)) {
			blow *= critTimes
		}
		want += blow
	}
	if damage != want {
		t.Errorf("4 taps did %d, want %d", damage, want)
	}
}

// Bosses grow with each level and with the chat's active members, chiefs
// thrice; upgrades cost more each level.
func TestRulesGrow(t *testing.T) {
	for level := 1; level < 60; level++ {
		if !Chief(level+1) && !Chief(level) && MaxHP(level+1, 3) <= MaxHP(level, 3) {
			t.Fatalf("level %d's boss is no stronger than level %d's", level+1, level)
		}
		if MaxHP(level, 5) <= MaxHP(level, 1) {
			t.Fatalf("level %d's boss is no stronger for five members than for one", level)
		}
		for _, item := range []string{"sword", "crit"} {
			if level < Most(item) && Cost(item, level+1) <= Cost(item, level) {
				t.Fatalf("%s at %d costs no more than at %d", item, level+1, level)
			}
		}
	}
	if MaxHP(10, 2) != int64(math.Round(1500*math.Pow(1.32, 9)*1.4*3)) || Reward(10, 0, 1) != 3*Reward(9, 0, 1)*10/9 {
		t.Errorf("a chief's HP %d and reward %d are not thrice", MaxHP(10, 2), Reward(10, 0, 1))
	}
	if MaxHP(500, 1000) != MaxHP(120, mostActive) || MaxHP(1, 0) != MaxHP(1, 1) {
		t.Errorf("the bounds on level and members are not kept")
	}
}

// The page's crit.js says the same taps are critical as the server.
func TestPageCountsCriticalBlowsAsTheServer(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	type tap struct {
		Seed   uint32 `json:"seed"`
		Tap    int64  `json:"tap"`
		Chance int    `json:"chance"`
	}
	var taps []tap
	var want []bool
	for _, seed := range []uint32{1, 7, 123456789, 4294967295} {
		for _, n := range []int64{0, 1, 2, 99, 12345, 4294967295, 4294967296, 1 << 40} {
			taps = append(taps, tap{seed, n, 35})
			want = append(want, Critical(seed, n, 35))
		}
	}
	input, _ := json.Marshal(taps)
	crit, _ := filepath.Abs("../../public/game/boss/crit.js")
	script := `const critical = require(process.argv[1]);
console.log(JSON.stringify(JSON.parse(process.argv[2]).map(t => critical(t.seed, t.tap, t.chance))));`
	out, err := exec.Command(node, "-e", script, crit, string(input)).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []bool
	if err := json.Unmarshal(out, &got); err != nil || len(got) != len(want) {
		t.Fatalf("node said %s", out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("seed %d tap %d: the page says %v, the server %v", taps[i].Seed, taps[i].Tap, got[i], want[i])
		}
	}
}

// Titles come with damage in all, Alpomish's at a hundred million.
func TestTitles(t *testing.T) {
	for _, tc := range []struct {
		total int64
		want  int
	}{{0, 0}, {9_999, 0}, {10_000, 1}, {999_999, 2}, {1_000_000, 3}, {5e8, 5}} {
		if got := Title(tc.total); got != tc.want {
			t.Errorf("Title(%d) = %d, want %d", tc.total, got, tc.want)
		}
	}
}

// Bo'ron zarbasi multiplies blows while it lasts, Olovli qilich makes the
// next ten critical; each is ready again only after its time, and counts
// in the day's quests.
func TestSkills(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	hero := storage.RaidHero{Sword: 1, Seed: 5}
	for _, skill := range []string{"storm", "fire"} {
		if err := UseSkill(skill, now)(&hero); err != nil {
			t.Fatalf("%s: %v", skill, err)
		}
		if err := UseSkill(skill, now.Add(time.Second))(&hero); !errors.Is(err, ErrCooldown) {
			t.Errorf("%s used again a second later: %v", skill, err)
		}
	}
	if hero.QuestSkills != 2 || hero.FireLeft != fireTaps {
		t.Fatalf("after the skills: %+v", hero)
	}
	counted, damage := Strike(&hero, 4, time.Second, now.Add(time.Second))
	want := 4 * TapDamage(1) * critTimes * stormTimes
	if counted != 4 || damage != want || hero.FireLeft != fireTaps-4 || hero.QuestTaps != 4 {
		t.Errorf("a hit in both did %d of %d taps, want %d; %+v", damage, counted, want, hero)
	}
	_, after := Strike(&hero, 1, time.Second, now.Add(stormFor+skillGrace+time.Second))
	if after != TapDamage(1)*critTimes {
		t.Errorf("after the storm a fiery blow did %d", after)
	}
	for _, skill := range []string{"rain", "magic"} {
		if err := UseSkill(skill, now)(&hero); !errors.Is(err, ErrNoSkill) {
			t.Errorf("%s: %v", skill, err)
		}
	}
}

// A quest's reward is taken once, when it is done, and the quests start
// over the next day in Uzbekistan.
func TestQuests(t *testing.T) {
	now := time.Date(2026, 10, 30, 18, 0, 0, 0, time.UTC) // 23:00 in Tashkent
	hero := storage.RaidHero{Sword: 5}
	if err := Claim(0, now)(&hero); !errors.Is(err, ErrNotDone) {
		t.Fatalf("an undone quest: %v", err)
	}
	hero.OnDay(Day(now))
	hero.QuestTaps = 300
	if err := Claim(0, now)(&hero); err != nil || hero.Coins != 150*2 {
		t.Fatalf("claiming the taps quest: %v, coins %d", err, hero.Coins)
	}
	if err := Claim(0, now)(&hero); !errors.Is(err, ErrClaimed) {
		t.Errorf("claimed twice: %v", err)
	}
	tomorrow := now.Add(2 * time.Hour) // past midnight in Tashkent
	if QuestProgress(hero, 0, tomorrow) != 0 || QuestClaimed(hero, 0, tomorrow) {
		t.Errorf("the next day's quests did not start over")
	}
	if err := Claim(7, now)(&hero); !errors.Is(err, ErrNoQuest) {
		t.Errorf("an unknown quest: %v", err)
	}
}

// The daily gift comes once a day and grows with a streak of days, which
// a day missed starts over.
func TestDailyStreak(t *testing.T) {
	day := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	hero := storage.RaidHero{Sword: 1}
	var gifts []int64
	for _, at := range []time.Time{day, day.Add(time.Hour), day.AddDate(0, 0, 1), day.AddDate(0, 0, 2), day.AddDate(0, 0, 4)} {
		var gift int64
		if err := Daily(at, &gift)(&hero); err != nil {
			t.Fatal(err)
		}
		gifts = append(gifts, gift)
	}
	if want := []int64{50, 0, 100, 150, 50}; !slices.Equal(gifts, want) || hero.Streak != 1 {
		t.Errorf("gifts = %v, streak %d; want %v and a streak started over", gifts, hero.Streak, want)
	}
}
