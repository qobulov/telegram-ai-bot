package cards

import "telegram-ai-bot/internal/arena"

// role is a card's class and skill picked by hand for its character.
type role struct {
	class arena.Class
	skill arena.Skill
}

// roles are the Legendary and Mythic cards' roles by card id, chosen to suit
// each character, as the owners asked on 2026-11-21. Other cards take the
// class their health to attack ratio gives and the skill their id gives
// (arena.FighterOf).
var roles = map[int64]role{
	40882:  {arena.ClassTank, arena.Provoke},   // Eren Yeager
	45627:  {arena.ClassFighter, arena.Strike}, // Levi
	40881:  {arena.ClassFighter, arena.Shield}, // Mikasa Ackerman
	422:    {arena.ClassTank, arena.Provoke},   // Guts
	5:      {arena.ClassFighter, arena.Strike}, // Ichigo Kurosaki
	137080: {arena.ClassMage, arena.Stun},      // Makima
	417:    {arena.ClassMage, arena.Stun},      // Lelouch Lamperouge
	1:      {arena.ClassFighter, arena.Strike}, // Spike Spiegel
	124381: {arena.ClassFighter, arena.Poison}, // Zero Two
	80:     {arena.ClassMage, arena.Poison},    // Light Yagami
	141694: {arena.ClassFighter, arena.Strike}, // Akaza
	130050: {arena.ClassFighter, arena.Shield}, // Giyuu Tomioka
	129132: {arena.ClassMage, arena.Poison},    // Muzan Kibutsuji
	127518: {arena.ClassTank, arena.Heal},      // Nezuko Kamado
	126071: {arena.ClassFighter, arena.Strike}, // Tanjirou Kamado
	129131: {arena.ClassFighter, arena.Stun},   // Zenitsu Agatsuma
	149220: {arena.ClassFighter, arena.Strike}, // Yoriichi Tsugikuni
	141918: {arena.ClassFighter, arena.Strike}, // Kokushibo
	246:    {arena.ClassFighter, arena.Strike}, // Gokuu Son
	298761: {arena.ClassMage, arena.Heal},      // Satoko Kirigaya
	176754: {arena.ClassMage, arena.Strike},    // Frieren
	11:     {arena.ClassMage, arena.Shield},    // Edward Elric
	30:     {arena.ClassFighter, arena.Strike}, // Gon Freecss
	27:     {arena.ClassFighter, arena.Stun},   // Killua Zoldyck
	28:     {arena.ClassMage, arena.Stun},      // Kurapika
	4003:   {arena.ClassFighter, arena.Stun},   // Joutarou Kuujou
	127691: {arena.ClassMage, arena.Shield},    // Satoru Gojou
	17:     {arena.ClassFighter, arena.Heal},   // Naruto Uzumaki
	128130: {arena.ClassMage, arena.Poison},    // Imu
	341936: {arena.ClassTank, arena.Provoke},   // Joy Boy
	40:     {arena.ClassFighter, arena.Strike}, // Luffy Monkey
	73935:  {arena.ClassFighter, arena.Strike}, // Saitama
	127222: {arena.ClassMage, arena.Stun},      // Mai Sakurajima
	88572:  {arena.ClassMage, arena.Heal},      // Emilia
	203633: {arena.ClassTank, arena.Shield},    // Qin Shi Huang
	129928: {arena.ClassFighter, arena.Strike}, // Jin-U Seong
	34470:  {arena.ClassMage, arena.Heal},      // Kurisu Makise
	126824: {arena.ClassMage, arena.Poison},    // Maomao
	72921:  {arena.ClassFighter, arena.Shield}, // Meliodas
	265209: {arena.ClassFighter, arena.Strike}, // Lancelot
	249281: {arena.ClassMage, arena.Heal},      // Tristan Liones
	90202:  {arena.ClassTank, arena.Strike},    // Escanor
	90169:  {arena.ClassFighter, arena.Strike}, // Violet Evergarden
	46494:  {arena.ClassMage, arena.Stun},      // Armin Arlert
	46496:  {arena.ClassTank, arena.Provoke},   // Erwin Smith
	22037:  {arena.ClassMage, arena.Poison},    // Hitagi Senjougahara
	123285: {arena.ClassTank, arena.Shield},    // Asta
	257562: {arena.ClassMage, arena.Stun},      // Hitori Gotou
	89198:  {arena.ClassMage, arena.Shield},    // Osamu Dazai
	130102: {arena.ClassFighter, arena.Heal},   // Denji
	137079: {arena.ClassFighter, arena.Strike}, // Power
	123212: {arena.ClassMage, arena.Stun},      // Kiyotaka Ayanokouji
	71:     {arena.ClassMage, arena.Stun},      // L Lawliet
	75:     {arena.ClassMage, arena.Poison},    // Ryuk
	129130: {arena.ClassFighter, arena.Strike}, // Inosuke Hashibira
	129133: {arena.ClassFighter, arena.Shield}, // Kyoujurou Rengoku
	124142: {arena.ClassMage, arena.Heal},      // Senkuu Ishigami
	913:    {arena.ClassFighter, arena.Strike}, // Vegeta
	5187:   {arena.ClassFighter, arena.Poison}, // Natsu Dragneel
	497:    {arena.ClassFighter, arena.Strike}, // Artoria Pendragon
	12:     {arena.ClassTank, arena.Shield},    // Alphonse Elric
	68:     {arena.ClassMage, arena.Poison},    // Roy Mustang
	672:    {arena.ClassFighter, arena.Strike}, // Gintoki Sakata
	2075:   {arena.ClassTank, arena.Provoke},   // Kamina
	2257:   {arena.ClassFighter, arena.Strike}, // Simon
	64769:  {arena.ClassFighter, arena.Strike}, // Shouyou Hinata
	64771:  {arena.ClassMage, arena.Heal},      // Tobio Kageyama
	601:    {arena.ClassTank, arena.Heal},      // Alucard
	31:     {arena.ClassMage, arena.Stun},      // Hisoka Morow
	4004:   {arena.ClassMage, arena.Stun},      // Dio Brando
	6356:   {arena.ClassFighter, arena.Strike}, // Joseph Joestar
	10529:  {arena.ClassMage, arena.Heal},      // Giorno Giovanna
	133704: {arena.ClassFighter, arena.Strike}, // Kento Nanami
	126635: {arena.ClassMage, arena.Shield},    // Megumi Fushiguro
	133700: {arena.ClassMage, arena.Poison},    // Nobara Kugisaki
	133701: {arena.ClassMage, arena.Strike},    // Sukuna
	127212: {arena.ClassFighter, arena.Strike}, // Yuuji Itadori
	120649: {arena.ClassMage, arena.Stun},      // Kaguya Shinomiya
	89361:  {arena.ClassMage, arena.Strike},    // Megumin
	89334:  {arena.ClassMage, arena.Stun},      // Arataka Reigen
	89616:  {arena.ClassMage, arena.Shield},    // Shigeo Kageyama
	133676: {arena.ClassMage, arena.Heal},      // Marin Kitagawa
	89028:  {arena.ClassFighter, arena.Strike}, // Izuku Midoriya
	88892:  {arena.ClassMage, arena.Strike},    // Katsuki Bakugou
	89220:  {arena.ClassMage, arena.Stun},      // Shouto Todoroki
	67065:  {arena.ClassTank, arena.Provoke},   // Hachiman Hikigaya
	14:     {arena.ClassMage, arena.Stun},      // Itachi Uchiha
	2423:   {arena.ClassMage, arena.Strike},    // Jiraiya
	85:     {arena.ClassFighter, arena.Strike}, // Kakashi Hatake
	2535:   {arena.ClassFighter, arena.Strike}, // Minato Namikaze
	13:     {arena.ClassMage, arena.Poison},    // Sasuke Uchiha
	53901:  {arena.ClassMage, arena.Strike},    // Madara Uchiha
	94:     {arena.ClassFighter, arena.Strike}, // Asuka Langley Souryuu
	86:     {arena.ClassTank, arena.Shield},    // Rei Ayanami
	89:     {arena.ClassTank, arena.Provoke},   // Shinji Ikari
	84677:  {arena.ClassFighter, arena.Strike}, // Yato
	2072:   {arena.ClassMage, arena.Poison},    // Ace Portgas
	13767:  {arena.ClassMage, arena.Heal},      // Law Trafalgar
	723:    {arena.ClassMage, arena.Stun},      // Nami
	61:     {arena.ClassMage, arena.Stun},      // Robin Nico
	305:    {arena.ClassFighter, arena.Strike}, // Sanji
	727:    {arena.ClassFighter, arena.Stun},   // Shanks
	62:     {arena.ClassFighter, arena.Strike}, // Zoro Roronoa
	38005:  {arena.ClassMage, arena.Stun},      // Homura Akemi
	88575:  {arena.ClassFighter, arena.Heal},   // Rem
	88573:  {arena.ClassTank, arena.Provoke},   // Subaru Natsuki
	252554: {arena.ClassMage, arena.Poison},    // Beelzebub
	140636: {arena.ClassMage, arena.Shield},    // Brunhilde
	252553: {arena.ClassTank, arena.Shield},    // Hades
	156052: {arena.ClassTank, arena.Provoke},   // Heracles
	140646: {arena.ClassFighter, arena.Strike}, // Kojirou Sasaki
	341135: {arena.ClassTank, arena.Shield},    // Leonidas
	140645: {arena.ClassFighter, arena.Strike}, // Lu Bu
	284632: {arena.ClassMage, arena.Stun},      // Nikola Tesla
	140641: {arena.ClassFighter, arena.Strike}, // Poseidon
	168999: {arena.ClassTank, arena.Provoke},   // Raiden Tameemon
	140650: {arena.ClassFighter, arena.Strike}, // Shiva
	140638: {arena.ClassFighter, arena.Stun},   // Thor
	140640: {arena.ClassFighter, arena.Strike}, // Zeus
	310:    {arena.ClassTank, arena.Provoke},   // Hanamichi Sakuragi
	138100: {arena.ClassMage, arena.Heal},      // Anya Forger
	138101: {arena.ClassFighter, arena.Strike}, // Loid Forger
	138102: {arena.ClassFighter, arena.Poison}, // Yor Forger
	35252:  {arena.ClassMage, arena.Shield},    // Rintarou Okabe
	36828:  {arena.ClassFighter, arena.Heal},   // Asuna Yuuki
	123962: {arena.ClassMage, arena.Poison},    // Rimuru Tempest
	90107:  {arena.ClassMage, arena.Stun},      // Kusuo Saiki
	77605:  {arena.ClassTank, arena.Heal},      // Ban
	282753: {arena.ClassMage, arena.Poison},    // Gawain
	206212: {arena.ClassFighter, arena.Heal},   // Percival
	126851: {arena.ClassFighter, arena.Shield}, // Mael (Estarossa)
	131559: {arena.ClassFighter, arena.Stun},   // Zeldris
	87275:  {arena.ClassFighter, arena.Heal},   // Ken Kaneki
	162:    {arena.ClassMage, arena.Shield},    // Vash the Stampede
	13020:  {arena.ClassFighter, arena.Stun},   // Askeladd
	10138:  {arena.ClassFighter, arena.Strike}, // Thorfinn Karlsefni
	729:    {arena.ClassMage, arena.Strike},    // Yuusuke Urameshi
}

// fighterOf is arena.FighterOf with the card's hand-picked role, if it has one.
func fighterOf(id int64, name string, rarity, stars, attack, health int) arena.Fighter {
	f := arena.FighterOf(id, name, rarity, stars, attack, health)
	if r, ok := roles[id]; ok {
		f.Class = r.class
		if rarity >= arena.MinSkillRarity {
			f.Skill = r.skill
		}
	}
	return f
}

// roleNames is the hand-picked role of card id as the page names it; both
// are empty for a card without one.
func roleNames(id int64, rarity int) (class, skill string) {
	r, ok := roles[id]
	if !ok {
		return "", ""
	}
	if rarity >= arena.MinSkillRarity {
		skill = skillNames[r.skill]
	}
	return classNames[r.class], skill
}
