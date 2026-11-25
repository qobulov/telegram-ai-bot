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

// mythicBoost raises the Mythics that lost more than the rest, as the
// owners asked on 2026-11-25: in 3000 random Mythic teams at ★5 Spike's won
// 21% below the average and Violet's, Lelouch's, Edward's, Light's and
// Noelle's 13-15% below. Every Mythic below the average was raised, round by
// round of the same simulation (4000 teams, 60 battles each), until each won
// within 5% of it; none was lowered, so Mythics are some 7% stronger on the
// whole than before. Yoriichi, 15% above, was left as he is.
var mythicBoost = map[int64]float64{
	417:    1.258, // Lelouch Lamperouge
	34470:  1.243, // Kurisu Makise
	1:      1.216, // Spike Spiegel
	11:     1.183, // Edward Elric
	422:    1.176, // Guts
	90169:  1.163, // Violet Evergarden
	88572:  1.156, // Emilia
	123283: 1.152, // Noelle Silva
	80:     1.141, // Light Yagami
	127222: 1.141, // Mai Sakurajima
	124381: 1.137, // Zero Two
	30:     1.116, // Gon Freecss
	28:     1.101, // Kurapika
	45627:  1.093, // Levi
	40881:  1.083, // Mikasa Ackerman
	4003:   1.085, // Joutarou Kuujou
	27:     1.078, // Killua Zoldyck
	126824: 1.076, // Maomao
	40882:  1.066, // Eren Yeager
	123284: 1.065, // Yuno
	298761: 1.063, // Satoko Kirigaya
	127691: 1.054, // Satoru Gojou
	137080: 1.051, // Makima
	124440: 1.050, // Yami Sukehiro
	203633: 1.050, // Qin Shi Huang
	5:      1.049, // Ichigo Kurosaki
	127518: 1.048, // Nezuko Kamado
	176754: 1.041, // Frieren
	124435: 1.040, // Julius Novachrono
	123285: 1.035, // Asta
	40:     1.026, // Luffy Monkey
	126071: 1.027, // Tanjirou Kamado
	265209: 1.022, // Lancelot
	246:    1.015, // Gokuu Son
	249281: 1.013, // Tristan Liones
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
