// Package game is the bot's own HTML5 games, played in Telegram. Tarozi
// puts fruits on the two pans of a bazaar scale, each fruit's weight on the
// price board, which changes every few rounds, and the player says, against
// the clock, which pan goes down, or that they balance. Kosmik ilon, in
// ilon.go, is a snake in space, and Uchar gilam, in gilam.go, a flying
// carpet over Samarkand.
//
// The pages in public/game draw them; this package deals the plays and
// scores them. A score is never taken from a page: the page sends what the
// player did and when, and the server, which dealt the play, counts it, so
// a player can only cheat by playing faster than a person could, which the
// timing limits refuse.
package game

import (
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/url"
	"strings"
	"time"
)

// ShortName is Tarozi's short name, as it must be registered with
// @BotFather's /newgame.
const ShortName = "tarozi"

// BossShortName is Dev jangi's short name, as it must be registered with
// @BotFather's /newgame. Its plays are internal/raid's: this package only
// opens its page with a session.
const BossShortName = "boss"

// IlonPath, GilamPath and BossPath are where Kosmik ilon's, Uchar gilam's
// and Dev jangi's pages are, under Tarozi's.
const (
	IlonPath  = "ilon/"
	GilamPath = "gilam/"
	BossPath  = "boss/"
)

const (
	// Seconds is how long a play lasts.
	Seconds = 40
	// rounds are dealt for each play, more than anyone answers in Seconds;
	// fruits per pan grow by one every roundsPerStep rounds, to maxFruits,
	// and the price board changes every roundsPerDay.
	rounds        = 160
	roundsPerStep = 8
	maxFruits     = 4
	roundsPerDay  = 6
	boardFruits   = 4
	fruitKinds    = 8
	// balancedShare is one in how many rounds is dealt to balance.
	balancedShare = 5
	// minGap is the quickest answer taken for a person's: reading two pans
	// and adding takes longer.
	minGap = 300 * time.Millisecond
	// lateness is how long after Seconds a last answer may come, for a
	// slow page; skew how far the page's clock may run ahead of the
	// server's between the start of a play and its end.
	lateness = time.Second
	skew     = 2 * time.Second
	// sessionAge is how long the link a Play press opens can start plays,
	// and runAge how long a play can be sent in after it started.
	sessionAge = time.Hour
	runAge     = 10 * time.Minute
)

// Player is who plays, on which game message: the score is set there.
// Name is their first name as Telegram gave it with the press of Play.
type Player struct {
	UserID          int64  `json:"u"`
	Name            string `json:"n,omitempty"`
	ChatID          int64  `json:"c,omitempty"`
	MessageID       int64  `json:"m,omitempty"`
	InlineMessageID string `json:"i,omitempty"`
}

// Game deals and scores plays for the page at one address.
type Game struct {
	secret []byte
	page   string
	now    func() time.Time
}

// New returns the game whose page is at pageURL. Its tokens are signed with
// a key made from the bot's token, so that only this bot's server could
// have made them; nil now is time.Now.
func New(botToken, pageURL string, now func() time.Time) *Game {
	if now == nil {
		now = time.Now
	}
	return &Game{secret: mac([]byte("TaroziSessions"), []byte(botToken)), page: pageURL, now: now}
}

// session is what a Play press opens the page with: the player, which
// game, "" for Tarozi, and when.
type session struct {
	Player
	Game   string `json:"g,omitempty"`
	Issued int64  `json:"t"`
}

// run is one play: the session's player, when it started, in Unix
// milliseconds, and the seed its rounds were dealt from.
type run struct {
	Player
	Game    string `json:"g,omitempty"`
	Started int64  `json:"t"`
	Seed    uint64 `json:"s"`
}

// The kinds of token, signed apart so that one cannot pass for the other.
const (
	kindSession = "session"
	kindRun     = "run"
)

var (
	// ErrToken means a session or a run that was not this server's, or is
	// too old.
	ErrToken = errors.New("game: not a valid or current token")
	// ErrImplausible means answers nobody could have given: too many, too
	// fast, or after the time was up.
	ErrImplausible = errors.New("game: implausible answers")
)

// PlayURL is the page a player's press of shortName's Play opens, with
// their session; ok is false for a game that is not this package's.
func (g *Game) PlayURL(shortName string, p Player) (link string, ok bool) {
	page := g.page
	switch shortName {
	case ShortName:
		shortName = ""
	case IlonShortName:
		page += IlonPath
	case GilamShortName:
		page += GilamPath
	case BossShortName:
		page += BossPath
	default:
		return "", false
	}
	token := g.sign(kindSession, session{Player: p, Game: shortName, Issued: g.now().Unix()})
	return page + "?s=" + url.QueryEscape(token), true
}

// Round is one weighing: the price board, as each fruit kind and its
// weight, and the fruits on each pan, as places on the board.
type Round struct {
	Fruits  []int `json:"fruits"`
	Weights []int `json:"weights"`
	Left    []int `json:"left"`
	Right   []int `json:"right"`
}

// answer is which pan goes down: L, R, or E when they balance.
func (r Round) answer() byte {
	switch left, right := r.weigh(r.Left), r.weigh(r.Right); {
	case left > right:
		return 'L'
	case left < right:
		return 'R'
	}
	return 'E'
}

func (r Round) weigh(pan []int) int {
	sum := 0
	for _, place := range pan {
		sum += r.Weights[place]
	}
	return sum
}

// Player is whom a current session is for.
func (g *Game) Player(sessionToken string) (Player, error) {
	s, err := g.session(sessionToken)
	return s.Player, err
}

// Session is whom a current session is for, and the short name of its
// game.
func (g *Game) Session(token string) (Player, string, error) {
	s, err := g.session(token)
	if err != nil {
		return Player{}, "", err
	}
	if s.Game == "" {
		return s.Player, ShortName, nil
	}
	return s.Player, s.Game, nil
}

// session reads a current session.
func (g *Game) session(token string) (session, error) {
	var s session
	if !g.verify(kindSession, token, &s) || g.now().Sub(time.Unix(s.Issued, 0)) > sessionAge {
		return session{}, ErrToken
	}
	return s, nil
}

// Start begins a play of Tarozi for a session: its rounds, and the run
// token the answers are sent in with. Each play is dealt afresh, so that
// rounds learnt by heart are of no use.
func (g *Game) Start(sessionToken string) (string, []Round, error) {
	s, err := g.session(sessionToken)
	if err != nil || s.Game != "" {
		return "", nil, ErrToken
	}
	r := run{Player: s.Player, Started: g.now().UnixMilli(), Seed: newSeed()}
	return g.sign(kindRun, r), deal(r.Seed), nil
}

// Result is a play scored.
type Result struct {
	Player Player
	Score  int
}

// Finish scores a play's answers, one letter each, L, R or E, given at
// times, in milliseconds since its first round showed.
func (g *Game) Finish(runToken, answers string, times []int) (Result, error) {
	r, elapsed, err := g.run(runToken, "", runAge)
	if err != nil {
		return Result{}, err
	}
	if len(times) > 0 && time.Duration(times[len(times)-1])*time.Millisecond > elapsed+skew {
		// The page says it took longer than the server saw pass.
		return Result{}, ErrImplausible
	}
	score, err := Score(deal(r.Seed), answers, times)
	if err != nil {
		return Result{}, err
	}
	return Result{Player: r.Player, Score: score}, nil
}

// Score counts a play: 10 points for a right answer, 2 more for each right
// one before it in a row, up to 10 more, and 5 off for a wrong one, never
// below 0. Answers too many, too quick or after the time are refused.
func Score(dealt []Round, answers string, times []int) (int, error) {
	if len(answers) != len(times) || len(answers) > len(dealt) {
		return 0, ErrImplausible
	}
	score, streak, last := 0, 0, 0
	for i := range len(answers) {
		gap := time.Duration(times[i]-last) * time.Millisecond
		if gap < minGap || time.Duration(times[i])*time.Millisecond > Seconds*time.Second+lateness {
			return 0, ErrImplausible
		}
		last = times[i]
		switch answers[i] {
		case dealt[i].answer():
			score += 10 + 2*min(streak, 5)
			streak++
		case 'L', 'R', 'E':
			score, streak = max(score-5, 0), 0
		default:
			return 0, ErrImplausible
		}
	}
	return score, nil
}

// runGame is the game a run token is for, "" for Tarozi or a token that
// is not this server's.
func (g *Game) runGame(token string) string {
	var r run
	if !g.verify(kindRun, token, &r) {
		return ""
	}
	return r.Game
}

// run reads a run token of game that started at most age ago, and how
// long ago it did.
func (g *Game) run(token, game string, age time.Duration) (run, time.Duration, error) {
	var r run
	if !g.verify(kindRun, token, &r) || r.Game != game {
		return run{}, 0, ErrToken
	}
	elapsed := g.now().Sub(time.UnixMilli(r.Started))
	if elapsed > age {
		return run{}, 0, ErrToken
	}
	return r, elapsed, nil
}

// deal makes a play's rounds from its seed, the same each time.
func deal(seed uint64) []Round {
	random := rand.New(rand.NewPCG(seed, 0x7461726f7a69)) // "tarozi"
	dealt := make([]Round, 0, rounds)
	var fruits, weights []int
	for i := range rounds {
		if i%roundsPerDay == 0 {
			fruits = random.Perm(fruitKinds)[:boardFruits]
			weights = random.Perm(9)[:boardFruits]
			for j := range weights {
				weights[j]++
			}
		}
		round := Round{Fruits: fruits, Weights: weights}
		most := min(1+i/roundsPerStep, maxFruits)
		round.Left = pan(random, most)
		round.Right = pan(random, most)
		if random.IntN(balancedShare) == 0 {
			balance(random, &round, most)
		}
		dealt = append(dealt, round)
	}
	return dealt
}

// pan is up to most fruits, at least one fewer, as places on the board.
func pan(random *rand.Rand, most int) []int {
	n := most
	if most > 1 {
		n -= random.IntN(2)
	}
	fruits := make([]int, n)
	for i := range fruits {
		fruits[i] = random.IntN(boardFruits)
	}
	return fruits
}

// balance deals the right pan again until it weighs as the left does, a
// few times at most: the board may have no way to.
func balance(random *rand.Rand, round *Round, most int) {
	want := round.weigh(round.Left)
	for range 60 {
		right := pan(random, most)
		if round.weigh(right) == want {
			round.Right = right
			return
		}
	}
}

func newSeed() uint64 {
	var b [8]byte
	_, _ = crand.Read(b[:])
	return binary.LittleEndian.Uint64(b[:])
}

// sign is a token of kind: its value as JSON, and the HMAC of both, in
// URL-safe base64.
func (g *Game) sign(kind string, value any) string {
	payload, _ := json.Marshal(value)
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + base64.RawURLEncoding.EncodeToString(mac(g.secret, []byte(kind+"."+body)))
}

// verify reads a token of kind into value, and reports whether it is one
// this game signed.
func (g *Game) verify(kind, token string, value any) bool {
	body, tag, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	given, err := base64.RawURLEncoding.DecodeString(tag)
	if err != nil || !hmac.Equal(given, mac(g.secret, []byte(kind+"."+body))) {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	return err == nil && json.Unmarshal(payload, value) == nil
}

func mac(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
