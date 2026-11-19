// Package game is the bot's own HTML5 games, played in Telegram. O'tinchi,
// in otinchi.go, is a woodcutter at an old plane tree; Dev jangi's and
// Mafia's plays are internal/raid's and internal/mafia's, and this package
// only opens their pages with a session.
//
// The pages in public/game draw them; this package deals O'tinchi's plays
// and scores them. A score is never taken from a page: the page sends what
// the player did and when, and the server, which dealt the play, counts it,
// so a player can only cheat by playing faster than a person could, which
// the timing limits refuse.
package game

import (
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
)

// BossShortName is Dev jangi's short name, as it must be registered with
// @BotFather's /newgame. Its plays are internal/raid's: this package only
// opens its page with a session.
const BossShortName = "boss"

// MafiaShortName is Mafia's short name, as it is registered with
// @BotFather's /newgame. Its plays are internal/mafia's: this package only
// opens its page with a session.
const MafiaShortName = "mafiya"

// OtinchiPath, BossPath and MafiaPath are where O'tinchi's, Dev jangi's
// and Mafia's pages are, under the games' address.
const (
	OtinchiPath = "otinchi/"
	BossPath    = "boss/"
	MafiaPath   = "mafiya/"
)

const (
	// skew is how far the page's clock may run ahead of the server's
	// between the start of a play and its end.
	skew = 2 * time.Second
	// sessionAge is how long the link a Play press opens can start plays.
	sessionAge = time.Hour
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
// have made them (its label is the first game's, Tarozi's, which is gone;
// changing it would only void the sessions open now); nil now is time.Now.
func New(botToken, pageURL string, now func() time.Time) *Game {
	if now == nil {
		now = time.Now
	}
	return &Game{secret: mac([]byte("TaroziSessions"), []byte(botToken)), page: pageURL, now: now}
}

// session is what a Play press opens the page with: the player, which
// game, and when.
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
	case OtinchiShortName:
		page += OtinchiPath
	case BossShortName:
		page += BossPath
	case MafiaShortName:
		page += MafiaPath
	default:
		return "", false
	}
	token := g.sign(kindSession, session{Player: p, Game: shortName, Issued: g.now().Unix()})
	return page + "?s=" + url.QueryEscape(token), true
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
	return s.Player, s.Game, nil
}

// session reads a current session.
func (g *Game) session(token string) (session, error) {
	var s session
	if !g.verify(kindSession, token, &s) || s.Game == "" || g.now().Sub(time.Unix(s.Issued, 0)) > sessionAge {
		return session{}, ErrToken
	}
	return s, nil
}

// Result is a play scored.
type Result struct {
	Player Player
	Score  int
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
