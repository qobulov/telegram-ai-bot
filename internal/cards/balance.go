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
var tierPower = [...]int{tierB: 8800, tierA: 9500, tierS: 10200}

// classShape is each class's health to attack: a Tank much life and little
// attack, a Mage the other way round.
var classShape = map[arena.Class]float64{arena.ClassTank: 5.0, arena.ClassFighter: 3.6, arena.ClassMage: 2.6}

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

// mythicStats are a tiered Mythic's attack and health at ★1, by its tier
// and its hand-picked class; ok is false for any other card.
func mythicStats(id int64) (attack, health int, ok bool) {
	t, ok := mythicTiers[id]
	r, picked := roles[id]
	if !ok || !picked {
		return 0, 0, false
	}
	shape := classShape[r.class]
	a := float64(tierPower[t]) / (1 + shape/4)
	return rounded(int(a)), rounded(int(a * shape)), true
}
