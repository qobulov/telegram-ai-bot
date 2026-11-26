package webapi

import (
	"context"
	"strconv"
)

// The game's emblems and battle spells, and its latest skins, from MLBBDex,
// as the owners asked on 2026-11-26.

const (
	shownNewSkins = 20
	newSkinsNote  = "The latest skins released, newest first, as the wiki lists them; the newest may be missing for some weeks."
)

type mlbbEmblem struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Gives   string `json:"gives"`
	BestFor string `json:"best_for,omitempty"`
}

type mlbbSpell struct {
	Name     string   `json:"name"`
	Does     string   `json:"does"`
	Cooldown string   `json:"cooldown,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	BestFor  string   `json:"best_for,omitempty"`
}

type mlbbNewSkin struct {
	Hero     string `json:"hero"`
	Skin     string `json:"skin"`
	Released string `json:"released,omitempty"`
	Tier     string `json:"tier,omitempty"`
	Series   string `json:"series,omitempty"`
}

func (a *apis) mlbbEmblems(ctx context.Context) (any, error) {
	emblems, err := dexGet[[]struct {
		Name        string `json:"name"`
		Role        string `json:"role"`
		Description string `json:"description"`
		BestFor     string `json:"bestFor"`
	}](ctx, a, "/api/v1/emblems")
	if err != nil {
		return nil, err
	}
	out := make([]mlbbEmblem, 0, len(emblems))
	for _, e := range emblems {
		out = append(out, mlbbEmblem{Name: cleanText(e.Name, maxHeroName), Role: cleanText(e.Role, maxHeroName),
			Gives: cleanText(e.Description, shownTip), BestFor: cleanText(e.BestFor, shownTip)})
	}
	return map[string]any{"emblems": out, "source": dexSource}, nil
}

func (a *apis) mlbbSpells(ctx context.Context) (any, error) {
	spells, err := dexGet[[]struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Cooldown    int      `json:"cooldown"`
		Roles       []string `json:"roles"`
		BestFor     string   `json:"bestFor"`
	}](ctx, a, "/api/v1/spells")
	if err != nil {
		return nil, err
	}
	out := make([]mlbbSpell, 0, len(spells))
	for _, s := range spells {
		spell := mlbbSpell{Name: cleanText(s.Name, maxHeroName), Does: cleanText(s.Description, shownTip),
			Roles: nonEmpty(s.Roles), BestFor: cleanText(s.BestFor, shownTip)}
		if s.Cooldown > 0 {
			spell.Cooldown = strconv.Itoa(s.Cooldown) + "s"
		}
		out = append(out, spell)
	}
	return map[string]any{"spells": out, "source": dexSource}, nil
}

func (a *apis) mlbbNewSkins(ctx context.Context) (any, error) {
	skins, err := dexGet[[]struct {
		Hero    string `json:"hero"`
		Name    string `json:"name"`
		Release string `json:"release"`
		Rarity  string `json:"rarity"`
		Series  string `json:"series"`
	}](ctx, a, "/api/v1/skins?limit="+strconv.Itoa(shownNewSkins))
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	if heroes, err := a.dexHeroes(ctx); err == nil {
		for _, h := range heroes {
			names[h.Slug] = h.Name
		}
	}
	out := make([]mlbbNewSkin, 0, len(skins))
	for _, s := range skins {
		hero := names[s.Hero]
		if hero == "" {
			hero = s.Hero
		}
		out = append(out, mlbbNewSkin{Hero: cleanText(hero, maxHeroName), Skin: cleanText(s.Name, maxHeroName),
			Released: cleanText(s.Release, maxHeroName), Tier: skinTier(s.Rarity), Series: cleanText(s.Series, maxHeroName)})
	}
	return map[string]any{"skins": out, "source": dexSource, "note": newSkinsNote}, nil
}
