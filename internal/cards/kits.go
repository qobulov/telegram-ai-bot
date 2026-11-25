package cards

import "telegram-ai-bot/internal/arena"

// kits are the Mythics' own Passive and Ultimate, as heroes have in Mobile
// Legends, as the owners asked on 2026-11-25, each after its character; a
// Mythic fights with its kit in place of its shared skill. Their numbers
// are balanced, with mythicBoost, by simulating random Mythic teams.
var kits = map[int64]arena.Kit{
	40882:  kit(ult("Founding Titan", 0.6, arena.TargetAll).provoke().shield(arena.WhoSelf, 0.25), arena.Regen, 0.05),  // Eren Yeager
	45627:  kit(ult("Spinning Slash", 2.0, arena.TargetBackWeakest).execute(0.5), arena.Crit, 0.2),                     // Levi
	40881:  kit(ult("Ackerman Guard", 1.6, arena.TargetNormal).shield(arena.WhoWeakest, 0.25), arena.Crit, 0.15),       // Mikasa Ackerman
	422:    kit(ult("Berserker Armor", 1.8, arena.TargetNormal).lifesteal(0.5), arena.Berserk, 0.3),                    // Guts
	123285: kit(ult("Black Asta", 1.5, arena.TargetNormal).shield(arena.WhoSelfWeakest, 0.25), arena.Armor, 0.15),      // Asta
	124435: kit(ult("Time Stop", 0.7, arena.TargetTwo).stun(1), arena.Swift, 40),                                       // Julius Novachrono
	124522: kit(ult("Calidos Brachium", 0.6, arena.TargetAll).poison(), arena.Berserk, 0.25),                           // Mereoleona Vermillion
	123283: kit(ult("Valkyrie Armor", 1.0, arena.TargetNormal).shield(arena.WhoAll, 0.15), arena.Armor, 0.1),           // Noelle Silva
	124440: kit(ult("Dimension Slash", 2.2, arena.TargetNormal).pierce(), arena.Crit, 0.2),                             // Yami Sukehiro
	123284: kit(ult("Spirit of Zephyr", 1.3, arena.TargetTwo), arena.Dodge, 0.15),                                      // Yuno
	5:      kit(ult("Getsuga Tensho", 1.5, arena.TargetFront), arena.Berserk, 0.25),                                    // Ichigo Kurosaki
	137080: kit(ult("Control", 0.6, arena.TargetTwo).stun(1), arena.Thorns, 0.2),                                       // Makima
	417:    kit(ult("Geass", 0.6, arena.TargetStrongest).stun(2), arena.Aura, 0.08),                                    // Lelouch Lamperouge
	1:      kit(ult("Bang", 2.0, arena.TargetBackWeakest).execute(0.5), arena.Dodge, 0.2),                              // Spike Spiegel
	124381: kit(ult("Strelizia", 1.8, arena.TargetNormal).lifesteal(0.3), arena.Berserk, 0.25),                         // Zero Two
	80:     kit(ult("Death Note", 1.2, arena.TargetBackWeakest).execute(2.0).poison(), arena.Executioner, 0.3),         // Light Yagami
	130050: kit(ult("Dead Calm", 1.0, arena.TargetNormal).shield(arena.WhoSelfWeakest, 0.25), arena.Dodge, 0.15),       // Giyuu Tomioka
	129132: kit(ult("Blood Demon Art", 0.5, arena.TargetAll).poison(), arena.Regen, 0.06),                              // Muzan Kibutsuji
	127518: kit(ult("Exploding Blood", 0.6, arena.TargetNormal).poison().heal(arena.WhoAll, 0.2), arena.Regen, 0.05),   // Nezuko Kamado
	126071: kit(ult("Hinokami Kagura", 1.8, arena.TargetNormal).poison(), arena.Berserk, 0.2),                          // Tanjirou Kamado
	129131: kit(ult("Thunderclap and Flash", 1.6, arena.TargetBackWeakest).stun(1), arena.Swift, 40),                   // Zenitsu Agatsuma
	149220: kit(ult("Sun Breathing", 0.9, arena.TargetAll), arena.Crit, 0.2),                                           // Yoriichi Tsugikuni
	141918: kit(ult("Moon Breathing", 1.4, arena.TargetFront), arena.Vampire, 0.15),                                    // Kokushibo
	141694: kit(ult("Compass Needle", 2.0, arena.TargetNormal), arena.Regen, 0.06),                                     // Akaza
	246:    kit(ult("Kamehameha", 2.6, arena.TargetNormal), arena.Berserk, 0.3),                                        // Gokuu Son
	298761: kit(ult("Firefly Light", 0, arena.TargetNormal).heal(arena.WhoAll, 0.25), arena.Regen, 0.04),               // Satoko Kirigaya
	176754: kit(ult("Zoltraak", 0.8, arena.TargetAll), arena.Executioner, 0.3),                                         // Frieren
	11:     kit(ult("Alchemy Wall", 1.0, arena.TargetNormal).shield(arena.WhoAll, 0.15), arena.Armor, 0.1),             // Edward Elric
	30:     kit(ult("Jajanken: Rock", 2.8, arena.TargetNormal), arena.Berserk, 0.25),                                   // Gon Freecss
	27:     kit(ult("Godspeed", 1.0, arena.TargetTwo).stun(1), arena.Dodge, 0.2),                                       // Killua Zoldyck
	28:     kit(ult("Chain Jail", 1.0, arena.TargetStrongest).stun(1), arena.Crit, 0.15),                               // Kurapika
	4003:   kit(ult("The World", 0.4, arena.TargetAll).stun(1), arena.Crit, 0.15),                                      // Joutarou Kuujou
	127691: kit(ult("Hollow Purple", 1.0, arena.TargetAll), arena.Dodge, 0.2),                                          // Satoru Gojou
	17:     kit(ult("Rasengan", 1.8, arena.TargetNormal).heal(arena.WhoSelf, 0.2), arena.Regen, 0.05),                  // Naruto Uzumaki
	128130: kit(ult("Domination", 0.5, arena.TargetAll).poison(), arena.Aura, 0.08),                                    // Imu
	341936: kit(ult("Sun God Nika", 0.6, arena.TargetAll).provoke(), arena.Thorns, 0.2),                                // Joy Boy
	40:     kit(ult("Gear 5", 2.4, arena.TargetNormal), arena.Armor, 0.1),                                              // Luffy Monkey
	73935:  kit(ult("Serious Punch", 3.0, arena.TargetNormal), arena.Armor, 0.15),                                      // Saitama
	127222: kit(ult("Unseen", 0.6, arena.TargetNormal).stun(2), arena.Dodge, 0.2),                                      // Mai Sakurajima
	203633: kit(ult("Chiyou Armor", 1.0, arena.TargetNormal).provoke().shield(arena.WhoSelf, 0.4), arena.Thorns, 0.25), // Qin Shi Huang
	88572:  kit(ult("Puck's Frost", 0.8, arena.TargetNormal).stun(1).heal(arena.WhoWeakest, 0.6), arena.Regen, 0.05),   // Emilia
	129928: kit(ult("Arise", 1.4, arena.TargetTwo), arena.Vampire, 0.15),                                               // Jin-U Seong
	34470:  kit(ult("Time Leap", 0, arena.TargetNormal).shield(arena.WhoAll, 0.15), arena.Swift, 40),                   // Kurisu Makise
	126824: kit(ult("Poison Test", 0.5, arena.TargetAll).poison(), arena.Armor, 0.15),                                  // Maomao
	72921:  kit(ult("Full Counter", 1.5, arena.TargetNormal).shield(arena.WhoSelf, 0.35), arena.Thorns, 0.25),          // Meliodas
	265209: kit(ult("Swift Strike", 2.0, arena.TargetBackWeakest), arena.Dodge, 0.2),                                   // Lancelot
	249281: kit(ult("Goddess Light", 0.8, arena.TargetNormal).heal(arena.WhoAll, 0.2), arena.Regen, 0.05),              // Tristan Liones
	90202:  kit(ult("Cruel Sun", 1.0, arena.TargetAll), arena.Berserk, 0.3),                                            // Escanor
	90169:  kit(ult("Auto Memory Doll", 1.8, arena.TargetNormal).execute(0.5), arena.Crit, 0.2),                        // Violet Evergarden
}

func kit(u ultimate, passive arena.PassiveKind, value float64) arena.Kit {
	return arena.Kit{Ult: arena.Ult(u), Passive: arena.Passive{Kind: passive, Value: value}}
}

// ultimate builds an arena.Ult step by step, for the table above.
type ultimate arena.Ult

func ult(name string, factor float64, target arena.UltTarget) ultimate {
	return ultimate{Name: name, Factor: factor, Target: target}
}

func (u ultimate) stun(turns int) ultimate      { u.Stun = turns; return u }
func (u ultimate) poison() ultimate             { u.Poison = true; return u }
func (u ultimate) pierce() ultimate             { u.Pierce = true; return u }
func (u ultimate) provoke() ultimate            { u.Provoke = true; return u }
func (u ultimate) execute(x float64) ultimate   { u.Execute = x; return u }
func (u ultimate) lifesteal(x float64) ultimate { u.Lifesteal = x; return u }
func (u ultimate) heal(who arena.Who, share float64) ultimate {
	u.Heal, u.HealShare = who, share
	return u
}
func (u ultimate) shield(who arena.Who, share float64) ultimate {
	u.Shield, u.ShieldShare = who, share
	return u
}

// kitOf is card id's kit, for a Mythic only.
func kitOf(id int64, rarity int) (arena.Kit, bool) {
	if Rarity(rarity) != Mythic {
		return arena.Kit{}, false
	}
	k, ok := kits[id]
	return k, ok
}

// kitView is a kit as the page shows it: the Ultimate's name and what it
// does, and the Passive's kind and value, which the page words.
type kitView struct {
	Ult     ultView     `json:"ult"`
	Passive passiveView `json:"passive"`
}

type ultView struct {
	Name        string  `json:"name"`
	Factor      float64 `json:"factor,omitempty"`
	Target      string  `json:"target,omitempty"`
	Stun        int     `json:"stun,omitempty"`
	Poison      bool    `json:"poison,omitempty"`
	Pierce      bool    `json:"pierce,omitempty"`
	Execute     float64 `json:"execute,omitempty"`
	Lifesteal   float64 `json:"lifesteal,omitempty"`
	Heal        string  `json:"heal,omitempty"`
	HealShare   float64 `json:"heal_share,omitempty"`
	Shield      string  `json:"shield,omitempty"`
	ShieldShare float64 `json:"shield_share,omitempty"`
	Provoke     bool    `json:"provoke,omitempty"`
}

type passiveView struct {
	Kind  string  `json:"kind"`
	Value float64 `json:"value"`
}

var (
	targetNames  = map[arena.UltTarget]string{arena.TargetNormal: "normal", arena.TargetAll: "all", arena.TargetBackWeakest: "back_weakest", arena.TargetFront: "front", arena.TargetTwo: "two", arena.TargetStrongest: "strongest"}
	whoNames     = map[arena.Who]string{arena.WhoNone: "", arena.WhoSelf: "self", arena.WhoWeakest: "weakest", arena.WhoSelfWeakest: "self_weakest", arena.WhoAll: "all"}
	passiveNames = map[arena.PassiveKind]string{arena.Berserk: "berserk", arena.Armor: "armor", arena.Vampire: "vampire", arena.Thorns: "thorns", arena.Dodge: "dodge",
		arena.Crit: "crit", arena.Regen: "regen", arena.Aura: "aura", arena.Executioner: "executioner", arena.Swift: "swift"}
)

func viewOfKit(k arena.Kit) *kitView {
	u := k.Ult
	v := &kitView{
		Ult: ultView{Name: u.Name, Factor: u.Factor, Stun: u.Stun, Poison: u.Poison, Pierce: u.Pierce, Execute: u.Execute,
			Lifesteal: u.Lifesteal, Heal: whoNames[u.Heal], HealShare: u.HealShare, Shield: whoNames[u.Shield], ShieldShare: u.ShieldShare, Provoke: u.Provoke},
		Passive: passiveView{Kind: passiveNames[k.Passive.Kind], Value: k.Passive.Value},
	}
	if u.Factor > 0 || u.Stun > 0 {
		v.Ult.Target = targetNames[u.Target]
	}
	return v
}

// kitViewOf is card id's kit for the page, nil for a card without one.
func kitViewOf(id int64, rarity int) *kitView {
	k, ok := kitOf(id, rarity)
	if !ok {
		return nil
	}
	return viewOfKit(k)
}
