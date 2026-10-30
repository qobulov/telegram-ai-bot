package raid

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"telegram-ai-bot/internal/storage"
)

// Taps count up to what a person taps in the time since the last hit, at
// most two seconds' worth and a few more; Barchinoy's arrows add for the
// time too.
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
		if counted, _ := Strike(hero, tc.taps, tc.since); counted != tc.want {
			t.Errorf("%d taps after %v counted %d, want %d", tc.taps, tc.since, counted, tc.want)
		}
	}
	hero.Helper = 1
	if _, damage := Strike(hero, 0, time.Second); damage != HelperDPS(1) {
		t.Errorf("Barchinoy alone did %d in a second, want %d", damage, HelperDPS(1))
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
	_, damage := Strike(hero, 4, time.Second)
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

// Bosses grow with each level, and upgrades cost more each level.
func TestRulesGrow(t *testing.T) {
	for level := 1; level < 60; level++ {
		if MaxHP(level+1) <= MaxHP(level) {
			t.Fatalf("level %d's boss is no stronger than level %d's", level+1, level)
		}
		for _, item := range []string{"sword", "crit", "helper"} {
			if level < Most(item) && Cost(item, level+1) <= Cost(item, level) {
				t.Fatalf("%s at %d costs no more than at %d", item, level+1, level)
			}
		}
	}
	if MaxHP(500) != MaxHP(80) || MaxHP(80) <= 0 {
		t.Errorf("a boss past level 80 has %d HP, want level 80's %d", MaxHP(500), MaxHP(80))
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
