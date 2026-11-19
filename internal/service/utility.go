package service

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Everyday commands other group bots have: /ping, /id, /calc, and /weather,
// which the model answers through its weather tool.

// utilityCommand answers /ping, /id and /calc in code; ok is false for any
// other message.
func (s *Service) utilityCommand(in incoming) (outcome, bool) {
	switch commandName(in.text) {
	case "/ping":
		took := time.Since(time.Unix(in.msg.Date, 0))
		if in.msg.Date == 0 || took < 0 {
			took = 0
		}
		return say(fmt.Sprintf("🏓 Pong · %d ms", took.Milliseconds())), true
	case "/id":
		return say(idText(in)), true
	case "/calc":
		expr := strings.TrimSpace(commandArgument(in.text))
		if expr == "" {
			return say("Masalan: /calc (12+8)*3/4"), true
		}
		v, err := calc(expr)
		if err != nil {
			return say("🧮 Hisoblab bo'lmadi: " + err.Error()), true
		}
		return say("🧮 " + expr + " = " + formatNumber(v)), true
	}
	return outcome{}, false
}

// idText is the asker's ID and the chat's, or the ID of the member replied
// to.
func idText(in incoming) string {
	if r := in.msg.ReplyToMessage; r != nil && r.From != nil {
		return fmt.Sprintf("🆔 %s: `%d`", cardMarks.Replace(displayName(r.From)), r.From.ID)
	}
	lines := []string{fmt.Sprintf("🆔 Sizning ID: `%d`", in.userID)}
	if in.group {
		lines = append(lines, fmt.Sprintf("💬 Chat ID: `%d`", in.msg.Chat.ID))
	}
	return strings.Join(lines, "\n")
}

// weatherQuestion is "/weather Toshkent" asked as the model is asked about
// the weather, to answer through its tool; ok is false for any other.
func weatherQuestion(text string) (string, bool) {
	if commandName(text) != "/weather" {
		return "", false
	}
	place := strings.TrimSpace(commandArgument(text))
	if place == "" {
		place = "Toshkent"
	}
	return place + " ob-havosi qanday?", true
}

// formatNumber writes v without a needless fraction, its thousands apart.
func formatNumber(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return spaced(int64(v))
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

var errCalc = errors.New("faqat sonlar, + - * / ^ % va qavslar")

// calc works out an arithmetic expression: + - * / ^, % as a remainder,
// parentheses, "x" and "×" for times, "," for a decimal point.
func calc(expr string) (float64, error) {
	r := strings.NewReplacer("×", "*", "x", "*", "х", "*", "÷", "/", ",", ".", " ", "")
	p := &calcParser{s: r.Replace(strings.ToLower(expr))}
	v, err := p.sum()
	if err != nil {
		return 0, err
	}
	if p.i != len(p.s) {
		return 0, errCalc
	}
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, errors.New("nolga bo'lib bo'lmaydi")
	}
	return v, nil
}

type calcParser struct {
	s string
	i int
}

func (p *calcParser) peek() byte {
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *calcParser) sum() (float64, error) {
	v, err := p.product()
	for err == nil && (p.peek() == '+' || p.peek() == '-') {
		op := p.peek()
		p.i++
		var w float64
		if w, err = p.product(); op == '+' {
			v += w
		} else {
			v -= w
		}
	}
	return v, err
}

func (p *calcParser) product() (float64, error) {
	v, err := p.power()
	for err == nil && (p.peek() == '*' || p.peek() == '/' || p.peek() == '%') {
		op := p.peek()
		p.i++
		var w float64
		w, err = p.power()
		switch op {
		case '*':
			v *= w
		case '/':
			v /= w
		default:
			v = math.Mod(v, w)
		}
	}
	return v, err
}

func (p *calcParser) power() (float64, error) {
	v, err := p.unary()
	if err == nil && p.peek() == '^' {
		p.i++
		var w float64
		if w, err = p.power(); err == nil {
			v = math.Pow(v, w)
		}
	}
	return v, err
}

func (p *calcParser) unary() (float64, error) {
	if p.peek() == '-' {
		p.i++
		v, err := p.unary()
		return -v, err
	}
	if p.peek() == '(' {
		p.i++
		v, err := p.sum()
		if err != nil {
			return 0, err
		}
		if p.peek() != ')' {
			return 0, errCalc
		}
		p.i++
		return v, nil
	}
	start := p.i
	for p.i < len(p.s) && (unicode.IsDigit(rune(p.s[p.i])) || p.s[p.i] == '.') {
		p.i++
	}
	if start == p.i {
		return 0, errCalc
	}
	return strconv.ParseFloat(p.s[start:p.i], 64)
}
