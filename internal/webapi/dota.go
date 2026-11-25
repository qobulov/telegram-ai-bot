package webapi

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
)

// A Dota 2 hero's skills and the game's items, from OpenDota's copy of the
// game's own texts, as the owners asked on 2026-11-25: asked a hero's
// skills or an item's stats, the bot searched the web five times over.

const (
	shownSkillText = 220
	shownItemText  = 300
	shownAttribs   = 4
	// shownSkills fits Invoker's, the most, within the output limit.
	shownSkills = 10
	// dotaConstantsLimit holds the abilities, the largest at 1.4 MB.
	dotaConstantsLimit = 4 << 20
)

// dotaValue is OpenDota's "1 / 2 / 3" values, given as a string, a list of
// them, a number, false or null.
type dotaValue string

func (v *dotaValue) UnmarshalJSON(data []byte) error {
	var list []json.RawMessage
	if json.Unmarshal(data, &list) == nil {
		var parts []string
		for _, raw := range list {
			parts = append(parts, plainValue(raw))
		}
		*v = dotaValue(strings.Join(parts, " / "))
		return nil
	}
	*v = dotaValue(plainValue(data))
	return nil
}

func plainValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	switch text := string(raw); text {
	case "false", "null", "true":
		return ""
	default:
		return text
	}
}

type dotaAttrib struct {
	Header  string    `json:"header"`
	Display string    `json:"display"`
	Value   dotaValue `json:"value"`
}

type dotaAbility struct {
	Name     string       `json:"dname"`
	Desc     string       `json:"desc"`
	Behavior dotaValue    `json:"behavior"`
	DmgType  dotaValue    `json:"dmg_type"`
	Damage   dotaValue    `json:"dmg"`
	Cooldown dotaValue    `json:"cd"`
	Mana     dotaValue    `json:"mc"`
	Attribs  []dotaAttrib `json:"attrib"`
}

type dotaItemAbility struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	Desc  string `json:"description"`
}

type dotaItem struct {
	Name       string            `json:"dname"`
	Cost       int               `json:"cost"`
	Cooldown   dotaValue         `json:"cd"`
	Mana       dotaValue         `json:"mc"`
	Notes      string            `json:"notes"`
	Attribs    []dotaAttrib      `json:"attrib"`
	Abilities  []dotaItemAbility `json:"abilities"`
	Components []string          `json:"components"`
}

func dotaConstants[T any](ctx context.Context, a *apis, name string) (T, error) {
	link := a.urls.OpenDota + "/api/constants/" + name
	return remember(a.cache, "dota:"+link, gamesTTL, func() (T, error) {
		var out T
		err := a.patient.getJSON(ctx, call{service: "OpenDota", url: link, limit: dotaConstantsLimit}, &out)
		return out, err
	})
}

// dotaSkills are a hero's skills by its key, as "npc_dota_hero_sven"; nil
// when OpenDota does not give them.
func (a *apis) dotaSkills(ctx context.Context, key string) []map[string]any {
	heroes, err := dotaConstants[map[string]struct {
		Abilities []string `json:"abilities"`
	}](ctx, a, "hero_abilities")
	if err != nil {
		return nil
	}
	abilities, err := dotaConstants[map[string]dotaAbility](ctx, a, "abilities")
	if err != nil {
		return nil
	}
	var skills []map[string]any
	for _, id := range heroes[key].Abilities {
		ability, ok := abilities[id]
		if !ok || ability.Name == "" || strings.HasPrefix(id, "generic_") {
			continue
		}
		skill := map[string]any{"name": ability.Name, "what": cleanText(ability.Desc, shownSkillText)}
		addValues(skill, map[string]dotaValue{"kind": ability.Behavior, "damage_type": ability.DmgType, "damage": ability.Damage, "cooldown": ability.Cooldown, "mana": ability.Mana})
		if values := attribLines(ability.Attribs); len(values) > 0 {
			skill["values"] = values
		}
		skills = append(skills, skill)
		if len(skills) == shownSkills {
			break
		}
	}
	return skills
}

// dotaItemNicknames are the short names players give items.
var dotaItemNicknames = map[string]string{
	"bkb": "Black King Bar", "aghs": "Aghanim's Scepter", "aghanim": "Aghanim's Scepter", "bf": "Battle Fury", "mkb": "Monkey King Bar",
	"deso": "Desolator", "euls": "Eul's Scepter of Divinity", "sny": "Sange and Yasha", "midas": "Hand of Midas", "blink": "Blink Dagger",
	"lotus": "Lotus Orb", "pipe": "Pipe of Insight", "vlads": "Vladmir's Offering", "shivas": "Shiva's Guard", "basher": "Skull Basher",
}

// dotaItemInfo is one item's cost, parts, stats and effects.
func (a *apis) dotaItemInfo(ctx context.Context, name string) (any, error) {
	items, err := dotaConstants[map[string]dotaItem](ctx, a, "items")
	if err != nil {
		return nil, err
	}
	keys := slices.DeleteFunc(slices.Sorted(maps.Keys(items)), func(k string) bool {
		return strings.HasPrefix(k, "recipe_") || items[k].Name == ""
	})
	if full, ok := dotaItemNicknames[lettersOf(name)]; ok {
		name = full
	}
	key, ok := findByName(keys, name, func(k string) string { return items[k].Name })
	if !ok {
		return nil, argumentError("no Dota 2 item is called %q; give its English name", name)
	}
	item := items[key]
	out := map[string]any{"item": item.Name, "cost": item.Cost, "source": "OpenDota, the game's own texts"}
	var parts []string
	for _, c := range item.Components {
		if part, ok := items[c]; ok && part.Name != "" {
			parts = append(parts, part.Name)
		}
	}
	if _, recipe := items["recipe_"+key]; recipe && len(parts) > 0 {
		parts = append(parts, "Recipe")
	}
	if len(parts) > 0 {
		out["built_from"] = parts
	}
	if stats := attribLines(item.Attribs); len(stats) > 0 {
		out["stats"] = stats
	}
	var effects []string
	for _, e := range item.Abilities {
		effects = append(effects, cleanText(strings.TrimSpace(e.Type+" "+e.Title+": "+e.Desc), shownItemText))
	}
	if len(effects) > 0 {
		out["effects"] = effects
	}
	addValues(out, map[string]dotaValue{"cooldown": item.Cooldown, "mana": item.Mana})
	if item.Notes != "" {
		out["notes"] = cleanText(item.Notes, shownItemText)
	}
	return out, nil
}

// zero is a value the game leaves at nothing, as "0" or "0%".
func (v dotaValue) zero() bool {
	return v == "" || strings.Trim(string(v), "0%. /") == ""
}

func addValues(out map[string]any, values map[string]dotaValue) {
	for key, v := range values {
		if !v.zero() {
			out[key] = string(v)
		}
	}
}

// attribLines are the first stats as the game words them: "+ 10 Strength"
// or "STUN DURATION: 1 / 1.25 / 1.5".
func attribLines(attribs []dotaAttrib) []string {
	var lines []string
	for _, at := range attribs {
		var line string
		switch {
		case at.Value.zero():
		case at.Display != "":
			line = strings.ReplaceAll(at.Display, "{value}", string(at.Value))
		case at.Header != "":
			line = strings.TrimSpace(at.Header) + " " + string(at.Value)
		}
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) == shownAttribs {
			break
		}
	}
	return lines
}
