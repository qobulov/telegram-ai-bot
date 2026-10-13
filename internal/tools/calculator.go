package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	maxExpressionLength = 200
	maxExpressionDepth  = 32
)

// Errors returned by Evaluate.
var (
	ErrDivisionByZero     = errors.New("division by zero")
	ErrEmptyExpression    = errors.New("the expression is empty")
	ErrExpressionTooLong  = fmt.Errorf("the expression is longer than %d characters", maxExpressionLength)
	ErrExpressionTooDeep  = fmt.Errorf("the expression is nested deeper than %d levels", maxExpressionDepth)
	ErrResultOutOfRange   = errors.New("the result is too large to represent")
	errUnexpectedEndOfExp = errors.New("the expression ends unexpectedly")
)

// Evaluate computes an arithmetic expression made of numbers, + - * /, and
// parentheses. It is a small hand-written parser: the input is never passed
// to an interpreter, a shell or any other evaluator.
//
// Grammar:
//
//	expr   = term   { ("+" | "-") term }
//	term   = factor { ("*" | "/") factor }
//	factor = ("+" | "-") factor | number | "(" expr ")"
func Evaluate(expression string) (float64, error) {
	if len(expression) > maxExpressionLength {
		return 0, ErrExpressionTooLong
	}
	if strings.TrimSpace(expression) == "" {
		return 0, ErrEmptyExpression
	}
	p := &parser{input: expression}
	value, err := p.expr()
	if err != nil {
		return 0, err
	}
	if p.skipSpaces(); p.pos < len(p.input) {
		return 0, p.unexpected()
	}
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return 0, ErrResultOutOfRange
	}
	return value, nil
}

type parser struct {
	input string
	pos   int
	depth int
}

func (p *parser) expr() (float64, error) {
	left, err := p.term()
	for err == nil {
		switch p.peek() {
		case '+':
			left, err = p.combine(left, p.term, add)
		case '-':
			left, err = p.combine(left, p.term, subtract)
		default:
			return left, nil
		}
	}
	return 0, err
}

func (p *parser) term() (float64, error) {
	left, err := p.factor()
	for err == nil {
		switch p.peek() {
		case '*':
			left, err = p.combine(left, p.factor, multiply)
		case '/':
			left, err = p.combine(left, p.factor, divide)
		default:
			return left, nil
		}
	}
	return 0, err
}

// combine consumes the operator under the cursor, parses the right operand
// and applies the operation.
func (p *parser) combine(left float64, operand func() (float64, error), apply func(a, b float64) (float64, error)) (float64, error) {
	p.pos++
	right, err := operand()
	if err != nil {
		return 0, err
	}
	return apply(left, right)
}

func (p *parser) factor() (float64, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxExpressionDepth {
		return 0, ErrExpressionTooDeep
	}

	switch c := p.peek(); {
	case c == '-':
		p.pos++
		value, err := p.factor()
		return -value, err
	case c == '+':
		p.pos++
		return p.factor()
	case c == '(':
		return p.group()
	case isNumberChar(c):
		return p.number()
	case c == 0:
		return 0, errUnexpectedEndOfExp
	default:
		return 0, p.unexpected()
	}
}

func (p *parser) group() (float64, error) {
	p.pos++
	value, err := p.expr()
	if err != nil {
		return 0, err
	}
	if p.peek() != ')' {
		return 0, errors.New("a closing parenthesis is missing")
	}
	p.pos++
	return value, nil
}

func (p *parser) number() (float64, error) {
	start := p.pos
	for p.pos < len(p.input) && isNumberChar(p.input[p.pos]) {
		p.pos++
	}
	literal := p.input[start:p.pos]
	value, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a valid number", literal)
	}
	return value, nil
}

// peek skips spaces and returns the next byte, or 0 at the end of input.
func (p *parser) peek() byte {
	p.skipSpaces()
	if p.pos >= len(p.input) {
		return 0
	}
	return p.input[p.pos]
}

func (p *parser) skipSpaces() {
	for p.pos < len(p.input) && (p.input[p.pos] == ' ' || p.input[p.pos] == '\t') {
		p.pos++
	}
}

func (p *parser) unexpected() error {
	return fmt.Errorf("unexpected %q at position %d; only numbers, + - * / and parentheses are supported", p.input[p.pos:p.pos+1], p.pos+1)
}

func isNumberChar(c byte) bool {
	return (c >= '0' && c <= '9') || c == '.'
}

func add(a, b float64) (float64, error)      { return a + b, nil }
func subtract(a, b float64) (float64, error) { return a - b, nil }
func multiply(a, b float64) (float64, error) { return a * b, nil }

func divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, ErrDivisionByZero
	}
	return a / b, nil
}

type calculatorArgs struct {
	Expression string `json:"expression"`
}

type calculatorResult struct {
	Expression string  `json:"expression"`
	Result     float64 `json:"result"`
}

func calculatorTool() Tool {
	return Tool{
		Name:        "calculator",
		Description: "Evaluate an arithmetic expression with numbers, + - * / and parentheses. Use it for any calculation instead of working it out yourself.",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"expression": {"type": "string", "description": "For example: (12.5 + 7) * 3 / 2"}
			},
			"required": ["expression"],
			"additionalProperties": false
		}`),
		Run: runCalculator,
	}
}

func runCalculator(_ context.Context, raw json.RawMessage) (any, error) {
	var args calculatorArgs
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	value, err := Evaluate(args.Expression)
	if err != nil {
		return nil, &ArgumentError{Message: err.Error()}
	}
	return calculatorResult{Expression: args.Expression, Result: value}, nil
}
