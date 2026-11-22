package cards

import "telegram-ai-bot/internal/arena"

// role is a card's class and skill picked by hand for its character.
type role struct {
	class arena.Class
	skill arena.Skill
}

// roles are the Legendary and Mythic cards' roles by card id, chosen to suit
// each character, as the owners asked on 2026-11-21, and on 2026-11-22 made
// Souls' four: the Mages that heal Healers (and Nezuko), those that stun or
// shield Supports, the rest Dealers with the Fighters. Other cards take the
// class their health to attack ratio gives and the skill their id gives
// (arena.FighterOf).
var roles = map[int64]role{
	40882:  {arena.ClassTank, arena.Provoke},   // Eren Yeager
	45627:  {arena.ClassDealer, arena.Strike},  // Levi
	40881:  {arena.ClassDealer, arena.Shield},  // Mikasa Ackerman
	422:    {arena.ClassTank, arena.Provoke},   // Guts
	5:      {arena.ClassDealer, arena.Strike},  // Ichigo Kurosaki
	137080: {arena.ClassSupport, arena.Stun},   // Makima
	417:    {arena.ClassSupport, arena.Stun},   // Lelouch Lamperouge
	1:      {arena.ClassDealer, arena.Strike},  // Spike Spiegel
	124381: {arena.ClassDealer, arena.Poison},  // Zero Two
	80:     {arena.ClassDealer, arena.Poison},  // Light Yagami
	141694: {arena.ClassDealer, arena.Strike},  // Akaza
	130050: {arena.ClassDealer, arena.Shield},  // Giyuu Tomioka
	129132: {arena.ClassDealer, arena.Poison},  // Muzan Kibutsuji
	127518: {arena.ClassHealer, arena.Heal},    // Nezuko Kamado
	126071: {arena.ClassDealer, arena.Strike},  // Tanjirou Kamado
	129131: {arena.ClassDealer, arena.Stun},    // Zenitsu Agatsuma
	149220: {arena.ClassDealer, arena.Strike},  // Yoriichi Tsugikuni
	141918: {arena.ClassDealer, arena.Strike},  // Kokushibo
	246:    {arena.ClassDealer, arena.Strike},  // Gokuu Son
	298761: {arena.ClassHealer, arena.Heal},    // Satoko Kirigaya
	176754: {arena.ClassDealer, arena.Strike},  // Frieren
	11:     {arena.ClassSupport, arena.Shield}, // Edward Elric
	30:     {arena.ClassDealer, arena.Strike},  // Gon Freecss
	27:     {arena.ClassDealer, arena.Stun},    // Killua Zoldyck
	28:     {arena.ClassSupport, arena.Stun},   // Kurapika
	4003:   {arena.ClassDealer, arena.Stun},    // Joutarou Kuujou
	127691: {arena.ClassSupport, arena.Shield}, // Satoru Gojou
	17:     {arena.ClassDealer, arena.Heal},    // Naruto Uzumaki
	128130: {arena.ClassDealer, arena.Poison},  // Imu
	341936: {arena.ClassTank, arena.Provoke},   // Joy Boy
	40:     {arena.ClassDealer, arena.Strike},  // Luffy Monkey
	73935:  {arena.ClassDealer, arena.Strike},  // Saitama
	127222: {arena.ClassSupport, arena.Stun},   // Mai Sakurajima
	88572:  {arena.ClassHealer, arena.Heal},    // Emilia
	203633: {arena.ClassTank, arena.Shield},    // Qin Shi Huang
	129928: {arena.ClassDealer, arena.Strike},  // Jin-U Seong
	34470:  {arena.ClassHealer, arena.Heal},    // Kurisu Makise
	126824: {arena.ClassDealer, arena.Poison},  // Maomao
	72921:  {arena.ClassDealer, arena.Shield},  // Meliodas
	265209: {arena.ClassDealer, arena.Strike},  // Lancelot
	249281: {arena.ClassHealer, arena.Heal},    // Tristan Liones
	90202:  {arena.ClassTank, arena.Strike},    // Escanor
	90169:  {arena.ClassDealer, arena.Strike},  // Violet Evergarden
	46494:  {arena.ClassSupport, arena.Stun},   // Armin Arlert
	46496:  {arena.ClassTank, arena.Provoke},   // Erwin Smith
	22037:  {arena.ClassDealer, arena.Poison},  // Hitagi Senjougahara
	123285: {arena.ClassTank, arena.Shield},    // Asta
	257562: {arena.ClassSupport, arena.Stun},   // Hitori Gotou
	89198:  {arena.ClassSupport, arena.Shield}, // Osamu Dazai
	130102: {arena.ClassDealer, arena.Heal},    // Denji
	137079: {arena.ClassDealer, arena.Strike},  // Power
	123212: {arena.ClassSupport, arena.Stun},   // Kiyotaka Ayanokouji
	71:     {arena.ClassSupport, arena.Stun},   // L Lawliet
	75:     {arena.ClassDealer, arena.Poison},  // Ryuk
	129130: {arena.ClassDealer, arena.Strike},  // Inosuke Hashibira
	129133: {arena.ClassDealer, arena.Shield},  // Kyoujurou Rengoku
	124142: {arena.ClassHealer, arena.Heal},    // Senkuu Ishigami
	913:    {arena.ClassDealer, arena.Strike},  // Vegeta
	5187:   {arena.ClassDealer, arena.Poison},  // Natsu Dragneel
	497:    {arena.ClassDealer, arena.Strike},  // Artoria Pendragon
	12:     {arena.ClassTank, arena.Shield},    // Alphonse Elric
	68:     {arena.ClassDealer, arena.Poison},  // Roy Mustang
	672:    {arena.ClassDealer, arena.Strike},  // Gintoki Sakata
	2075:   {arena.ClassTank, arena.Provoke},   // Kamina
	2257:   {arena.ClassDealer, arena.Strike},  // Simon
	64769:  {arena.ClassDealer, arena.Strike},  // Shouyou Hinata
	64771:  {arena.ClassHealer, arena.Heal},    // Tobio Kageyama
	601:    {arena.ClassTank, arena.Heal},      // Alucard
	31:     {arena.ClassSupport, arena.Stun},   // Hisoka Morow
	4004:   {arena.ClassSupport, arena.Stun},   // Dio Brando
	6356:   {arena.ClassDealer, arena.Strike},  // Joseph Joestar
	10529:  {arena.ClassHealer, arena.Heal},    // Giorno Giovanna
	133704: {arena.ClassDealer, arena.Strike},  // Kento Nanami
	126635: {arena.ClassSupport, arena.Shield}, // Megumi Fushiguro
	133700: {arena.ClassDealer, arena.Poison},  // Nobara Kugisaki
	133701: {arena.ClassDealer, arena.Strike},  // Sukuna
	127212: {arena.ClassDealer, arena.Strike},  // Yuuji Itadori
	120649: {arena.ClassSupport, arena.Stun},   // Kaguya Shinomiya
	89361:  {arena.ClassDealer, arena.Strike},  // Megumin
	89334:  {arena.ClassSupport, arena.Stun},   // Arataka Reigen
	89616:  {arena.ClassSupport, arena.Shield}, // Shigeo Kageyama
	133676: {arena.ClassHealer, arena.Heal},    // Marin Kitagawa
	89028:  {arena.ClassDealer, arena.Strike},  // Izuku Midoriya
	88892:  {arena.ClassDealer, arena.Strike},  // Katsuki Bakugou
	89220:  {arena.ClassSupport, arena.Stun},   // Shouto Todoroki
	67065:  {arena.ClassTank, arena.Provoke},   // Hachiman Hikigaya
	14:     {arena.ClassSupport, arena.Stun},   // Itachi Uchiha
	2423:   {arena.ClassDealer, arena.Strike},  // Jiraiya
	85:     {arena.ClassDealer, arena.Strike},  // Kakashi Hatake
	2535:   {arena.ClassDealer, arena.Strike},  // Minato Namikaze
	13:     {arena.ClassDealer, arena.Poison},  // Sasuke Uchiha
	53901:  {arena.ClassDealer, arena.Strike},  // Madara Uchiha
	94:     {arena.ClassDealer, arena.Strike},  // Asuka Langley Souryuu
	86:     {arena.ClassTank, arena.Shield},    // Rei Ayanami
	89:     {arena.ClassTank, arena.Provoke},   // Shinji Ikari
	84677:  {arena.ClassDealer, arena.Strike},  // Yato
	2072:   {arena.ClassDealer, arena.Poison},  // Ace Portgas
	13767:  {arena.ClassHealer, arena.Heal},    // Law Trafalgar
	723:    {arena.ClassSupport, arena.Stun},   // Nami
	61:     {arena.ClassSupport, arena.Stun},   // Robin Nico
	305:    {arena.ClassDealer, arena.Strike},  // Sanji
	727:    {arena.ClassDealer, arena.Stun},    // Shanks
	62:     {arena.ClassDealer, arena.Strike},  // Zoro Roronoa
	38005:  {arena.ClassSupport, arena.Stun},   // Homura Akemi
	88575:  {arena.ClassDealer, arena.Heal},    // Rem
	88573:  {arena.ClassTank, arena.Provoke},   // Subaru Natsuki
	252554: {arena.ClassDealer, arena.Poison},  // Beelzebub
	140636: {arena.ClassSupport, arena.Shield}, // Brunhilde
	252553: {arena.ClassTank, arena.Shield},    // Hades
	156052: {arena.ClassTank, arena.Provoke},   // Heracles
	140646: {arena.ClassDealer, arena.Strike},  // Kojirou Sasaki
	341135: {arena.ClassTank, arena.Shield},    // Leonidas
	140645: {arena.ClassDealer, arena.Strike},  // Lu Bu
	284632: {arena.ClassSupport, arena.Stun},   // Nikola Tesla
	140641: {arena.ClassDealer, arena.Strike},  // Poseidon
	168999: {arena.ClassTank, arena.Provoke},   // Raiden Tameemon
	140650: {arena.ClassDealer, arena.Strike},  // Shiva
	140638: {arena.ClassDealer, arena.Stun},    // Thor
	140640: {arena.ClassDealer, arena.Strike},  // Zeus
	310:    {arena.ClassTank, arena.Provoke},   // Hanamichi Sakuragi
	138100: {arena.ClassHealer, arena.Heal},    // Anya Forger
	138101: {arena.ClassDealer, arena.Strike},  // Loid Forger
	138102: {arena.ClassDealer, arena.Poison},  // Yor Forger
	35252:  {arena.ClassSupport, arena.Shield}, // Rintarou Okabe
	36828:  {arena.ClassDealer, arena.Heal},    // Asuna Yuuki
	123962: {arena.ClassDealer, arena.Poison},  // Rimuru Tempest
	90107:  {arena.ClassSupport, arena.Stun},   // Kusuo Saiki
	77605:  {arena.ClassTank, arena.Heal},      // Ban
	282753: {arena.ClassDealer, arena.Poison},  // Gawain
	206212: {arena.ClassDealer, arena.Heal},    // Percival
	126851: {arena.ClassDealer, arena.Shield},  // Mael (Estarossa)
	131559: {arena.ClassDealer, arena.Stun},    // Zeldris
	87275:  {arena.ClassDealer, arena.Heal},    // Ken Kaneki
	162:    {arena.ClassSupport, arena.Shield}, // Vash the Stampede
	13020:  {arena.ClassDealer, arena.Stun},    // Askeladd
	10138:  {arena.ClassDealer, arena.Strike},  // Thorfinn Karlsefni
	729:    {arena.ClassDealer, arena.Strike},  // Yuusuke Urameshi
	123284: {arena.ClassDealer, arena.Strike},  // Yuno: his wind's blades
	124435: {arena.ClassSupport, arena.Stun},   // Julius Novachrono: time stopped
	124522: {arena.ClassDealer, arena.Poison},  // Mereoleona Vermillion: her fire burning on
	124440: {arena.ClassDealer, arena.Strike},  // Yami Sukehiro: his dimension slash
	123283: {arena.ClassSupport, arena.Shield}, // Noelle Silva: her Valkyrie armour
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
