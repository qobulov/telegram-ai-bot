package webapi

import (
	"slices"
	"testing"
)

func TestSplitByBoth(t *testing.T) {
	rone := []mlbbMatchup{{Hero: "Minsitthar"}, {Hero: "X.Borg"}}
	both, dexOnly := splitByBoth([]string{"Minsitthar", "X.Borg", "Franco"}, rone)
	if !slices.Equal(both, []string{"Minsitthar", "X.Borg"}) || !slices.Equal(dexOnly, []string{"Franco"}) {
		t.Errorf("both %v, MLBBDex only %v", both, dexOnly)
	}
}

func TestItemsInBoth(t *testing.T) {
	dex := []mlbbBuild{{Items: []string{"Golden Staff", "War Axe", "Endless Battle"}}, {Items: []string{"Golden Staff", "Rose Gold Meteor"}}}
	rone := []mlbbBuild{{Items: []string{"war axe", "Golden Staff", "Blade of Despair"}}}
	if got := itemsInBoth(dex, rone); !slices.Equal(got, []string{"Golden Staff", "War Axe"}) {
		t.Errorf("items in both = %v", got)
	}
}

func TestRatesDiffer(t *testing.T) {
	if rateValue("52.3%") != 52.3 || rateValue("") != 0 {
		t.Errorf("rateValue = %v", rateValue("52.3%"))
	}
}
