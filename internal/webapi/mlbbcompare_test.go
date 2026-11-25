package webapi

import (
	"slices"
	"testing"
)

// Heroes both sources name come first, then Rone Arena's, then MLBBDex's.
func TestMergeMatchups(t *testing.T) {
	rone := []mlbbMatchup{{Hero: "Baxia", WinRateChange: "+2.0%"}, {Hero: "X.Borg", WinRateChange: "+1.0%"}}
	dex := []mlbbMatchup{{Hero: "X.Borg"}, {Hero: "Franco", WinRateChange: "+1.9%"}}
	var got []string
	for _, m := range mergeMatchups(rone, dex) {
		got = append(got, m.Hero)
	}
	if !slices.Equal(got, []string{"X.Borg", "Baxia", "Franco"}) {
		t.Errorf("merged = %v", got)
	}
}

// A build both give is kept once, and the most played comes first.
func TestMergeBuilds(t *testing.T) {
	rone := []mlbbBuild{
		{Spell: "Vengeance", Emblem: "Fighter", Items: []string{"War Axe", "Golden Staff"}, PickRate: "4.0%"},
		{Spell: "Retribution", Emblem: "Assassin", Items: []string{"Hunter Strike"}, PickRate: "1.0%"},
	}
	dex := []mlbbBuild{
		{Spell: "Vengeance", Emblem: "Fighter", Items: []string{"Golden Staff", "War Axe"}, PickRate: "6.5%"},
		{Spell: "Vengeance", Emblem: "Fighter", Items: []string{"Golden Staff", "Rose Gold Meteor"}, PickRate: "2.0%"},
	}
	got := mergeBuilds(rone, dex)
	if len(got) != 3 || got[0].PickRate != "6.5%" || got[1].Items[1] != "Rose Gold Meteor" || got[2].Spell != "Retribution" {
		t.Errorf("merged = %+v", got)
	}
}

func TestRateValue(t *testing.T) {
	if rateValue("52.3%") != 52.3 || rateValue("") != 0 {
		t.Errorf("rateValue = %v", rateValue("52.3%"))
	}
}
