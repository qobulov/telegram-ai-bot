package cards

import "telegram-ai-bot/internal/arena"

// tier is how strong a Mythic's character is in its story: its arena power
// (attack and a quarter of its health) is the tier's, shaped by its class.
type tier int

const (
	tierB tier = iota // the clever, the healers, the best loved
	tierA             // the great fighters
	tierS             // the strongest there are
)

// tierPower is each tier's arena power at ★1: close enough that a team of
// the lowest can still beat one of the highest, and on the whole as strong as
// the Mythics were before (9485 on average), so that they stand no further
// above the rest.
var tierPower = [...]int{tierB: 8760, tierA: 9460, tierS: 10160}

// classShape is each role's health to attack: a Tank much life and little
// attack, a Support the other way round, a Healer, standing long, between.
var classShape = map[arena.Class]float64{arena.ClassTank: 5.0, arena.ClassDealer: 3.4, arena.ClassSupport: 2.8, arena.ClassHealer: 3.8}

// classPower and skillPower scale a Mythic's power by its role, as the
// owners asked on 2026-11-22 that Mythics of one tier not all be as strong.
// They were found by simulating 40000 battles of random Mythic teams, so that
// each of Souls' four roles and each skill wins about half its battles (all
// within 49-51.5%): a Healer, dealing little, needs more power for it.
var (
	classPower = map[arena.Class]float64{arena.ClassTank: 1.02, arena.ClassDealer: 0.99, arena.ClassSupport: 0.99, arena.ClassHealer: 1.04}
	skillPower = map[arena.Skill]float64{
		arena.Stun: 1.02, arena.Heal: 1.03, arena.Shield: 1.00, arena.Provoke: 0.99, arena.Poison: 1.02, arena.Strike: 0.985,
	}
)

// mythicSpread is how far either way a Mythic's own power may lie from its
// tier and role's: those of one tier and class are spread evenly across it,
// by their ids, so that no two are alike.
const mythicSpread = 0.045

// mythicTiers are the Mythics' tiers, as the owners asked on 2026-11-22 that
// each be as strong as its character and no one by the luck of its id.
var mythicTiers = map[int64]tier{
	73935: tierS, 246: tierS, 127691: tierS, 149220: tierS, 128130: tierS, 341936: tierS, // Saitama, Goku, Gojo, Yoriichi, Imu, Joy Boy
	40: tierS, 17: tierS, 5: tierS, 129928: tierS, 90202: tierS, 72921: tierS, 40882: tierS, // Luffy, Naruto, Ichigo, Jinwoo, Escanor, Meliodas, Eren
	141918: tierA, 129132: tierA, 141694: tierA, 130050: tierA, 129131: tierA, 126071: tierA, // Kokushibo, Muzan, Akaza, Giyu, Zenitsu, Tanjiro
	203633: tierA, 265209: tierA, 249281: tierA, 422: tierA, 4003: tierA, 11: tierA, // Qin Shi Huang, Lancelot, Tristan, Guts, Jotaro, Edward
	28: tierA, 27: tierA, 30: tierA, 45627: tierA, 40881: tierA, 176754: tierA, 137080: tierA, // Kurapika, Killua, Gon, Levi, Mikasa, Frieren, Makima
	123285: tierA, 123284: tierA, 124435: tierA, 124522: tierA, 124440: tierA, // Asta, Yuno, Julius, Mereoleona, Yami
	417: tierB, 80: tierB, 1: tierB, 90169: tierB, 126824: tierB, 34470: tierB, 88572: tierB, // Lelouch, Light, Spike, Violet, Maomao, Kurisu, Emilia
	127222: tierB, 298761: tierB, 127518: tierB, 124381: tierB, 123283: tierB, // Mai, Satoko, Nezuko, Zero Two, Noelle
}

// mythicPower is a tiered Mythic's power at ★1: its tier's, scaled by its
// role and spread among those of its tier and class.
func mythicPower(id int64, t tier, r role) float64 {
	boost := mythicBoost[id]
	if boost == 0 {
		boost = 1
	}
	return float64(tierPower[t]) * classPower[r.class] * skillPower[r.skill] * (1 + mythicSpreadOf(id, t, r)) * boost
}

// mythicBoost scales each Mythic's power so that, with its kit (its own
// Passive and Ultimate, since 2026-11-25), it wins as much as the others:
// round by round of simulating 4000 random Mythic teams at ★5, 60 battles
// each, every Mythic's power was moved against how far its teams won above
// or below the average, until all were within 2% of it (they were 34% apart
// with the kits alone, Imu's Domination and Lelouch's Geass above, Light and
// Kurapika below). On the whole Mythics are as strong as before the kits.
// (Before the kits, on 2026-11-25 too, it raised only those below.)
var mythicBoost = map[int64]float64{
	34470:  1.464, // Kurisu Makise
	80:     1.236, // Light Yagami
	11:     1.224, // Edward Elric
	422:    1.218, // Guts
	1:      1.217, // Spike Spiegel
	90169:  1.194, // Violet Evergarden
	123283: 1.193, // Noelle Silva
	124381: 1.191, // Zero Two
	28:     1.176, // Kurapika
	88572:  1.155, // Emilia
	127222: 1.144, // Mai Sakurajima
	30:     1.143, // Gon Freecss
	298761: 1.129, // Satoko Kirigaya
	5:      1.125, // Ichigo Kurosaki
	45627:  1.112, // Levi
	176754: 1.112, // Frieren
	40881:  1.109, // Mikasa Ackerman
	126824: 1.084, // Maomao
	4003:   1.078, // Joutarou Kuujou
	124440: 1.076, // Yami Sukehiro
	40882:  1.071, // Eren Yeager
	127518: 1.070, // Nezuko Kamado
	123284: 1.070, // Yuno
	126071: 1.057, // Tanjirou Kamado
	246:    1.054, // Gokuu Son
	141918: 1.050, // Kokushibo
	137080: 1.048, // Makima
	40:     1.045, // Luffy Monkey
	27:     1.039, // Killua Zoldyck
	17:     1.038, // Naruto Uzumaki
	249281: 1.038, // Tristan Liones
	141694: 1.038, // Akaza
	124522: 1.031, // Mereoleona Vermillion
	127691: 1.016, // Satoru Gojou
	129928: 1.010, // Jin-U Seong
	123285: 1.010, // Asta
	341936: 1.009, // Joy Boy
	73935:  1.008, // Saitama
	203633: 1.007, // Qin Shi Huang
	265209: 1.005, // Lancelot
	90202:  1.004, // Escanor
	129132: 0.998, // Muzan Kibutsuji
	130050: 0.996, // Giyuu Tomioka
	129131: 0.993, // Zenitsu Agatsuma
	72921:  0.980, // Meliodas
	149220: 0.940, // Yoriichi Tsugikuni
	124435: 0.912, // Julius Novachrono
	417:    0.890, // Lelouch Lamperouge
	128130: 0.663, // Imu
}

// mythicSpreadOf is where id lies among the Mythics of tier t and r's class,
// from -mythicSpread for the lowest id to +mythicSpread for the highest.
func mythicSpreadOf(id int64, t tier, r role) float64 {
	below, all := 0, 0
	for other, ot := range mythicTiers {
		if ot != t || roles[other].class != r.class {
			continue
		}
		all++
		if other < id {
			below++
		}
	}
	if all < 2 {
		return 0
	}
	return mythicSpread * (2*float64(below)/float64(all-1) - 1)
}

// mythicStats are a tiered Mythic's attack and health at ★1, its power
// shaped by its hand-picked class; ok is false for any other card.
func mythicStats(id int64) (attack, health int, ok bool) {
	t, ok := mythicTiers[id]
	r, picked := roles[id]
	if !ok || !picked {
		return 0, 0, false
	}
	shape := classShape[r.class]
	a := mythicPower(id, t, r) / (1 + shape/4)
	return rounded(int(a)), rounded(int(a * shape)), true
}
