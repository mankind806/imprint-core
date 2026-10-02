package main

import (
	"fmt"
	"strconv"
	"strings"
)

// The `if` of a registry rule (jev-kern.md section 4): score IDs compared with a
// number, code flags, &&, ||, ! and parentheses. No functions, no arithmetic,
// no strings: whatever code can compute is a named Go predicate that sets a flag.
//
//	expr    := and { "||" and }
//	and     := unary { "&&" unary }
//	unary   := "!" unary | primary
//	primary := "(" expr ")" | ident op number | ident
//	op      := ">=" | "<=" | ">" | "<" | "==" | "!="
//
// A bare ident is a code flag; an ident with a comparison is a score.

type ruleEnv struct {
	scores map[string]float64
	flags  map[string]bool
}

type ruleExpr interface {
	eval(env ruleEnv) bool
	refs(scores, flags map[string]bool)
}

type exprOr struct{ l, r ruleExpr }
type exprAnd struct{ l, r ruleExpr }
type exprNot struct{ x ruleExpr }
type exprFlag struct{ id string }
type exprCmp struct {
	id  string
	op  string
	num float64
}

func (e exprOr) eval(env ruleEnv) bool  { return e.l.eval(env) || e.r.eval(env) }
func (e exprAnd) eval(env ruleEnv) bool { return e.l.eval(env) && e.r.eval(env) }
func (e exprNot) eval(env ruleEnv) bool { return !e.x.eval(env) }
func (e exprFlag) eval(env ruleEnv) bool {
	return env.flags[e.id]
}
func (e exprCmp) eval(env ruleEnv) bool {
	v := env.scores[e.id]
	switch e.op {
	case ">=":
		return v >= e.num
	case "<=":
		return v <= e.num
	case ">":
		return v > e.num
	case "<":
		return v < e.num
	case "==":
		return v == e.num
	case "!=":
		return v != e.num
	}
	return false
}

func (e exprOr) refs(s, f map[string]bool)   { e.l.refs(s, f); e.r.refs(s, f) }
func (e exprAnd) refs(s, f map[string]bool)  { e.l.refs(s, f); e.r.refs(s, f) }
func (e exprNot) refs(s, f map[string]bool)  { e.x.refs(s, f) }
func (e exprFlag) refs(_, f map[string]bool) { f[e.id] = true }
func (e exprCmp) refs(s, _ map[string]bool)  { s[e.id] = true }

// ruleApplies reports whether every score and flag the expression names is
// present; a rule that names a missing one does not fire.
func ruleApplies(x ruleExpr, env ruleEnv) bool {
	s, f := map[string]bool{}, map[string]bool{}
	x.refs(s, f)
	for id := range s {
		if _, ok := env.scores[id]; !ok {
			return false
		}
	}
	for id := range f {
		if _, ok := env.flags[id]; !ok {
			return false
		}
	}
	return true
}

type ruleToken struct {
	kind string // ident, num, op, end
	text string
}

func lexRule(src string) ([]ruleToken, error) {
	var toks []ruleToken
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case isIdentStart(c):
			j := i + 1
			for j < len(src) && (isIdentStart(src[j]) || (src[j] >= '0' && src[j] <= '9')) {
				j++
			}
			toks = append(toks, ruleToken{"ident", src[i:j]})
			i = j
		case c >= '0' && c <= '9' || c == '.':
			j := i + 1
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.') {
				j++
			}
			toks = append(toks, ruleToken{"num", src[i:j]})
			i = j
		default:
			two := ""
			if i+1 < len(src) {
				two = src[i : i+2]
			}
			switch two {
			case ">=", "<=", "==", "!=", "&&", "||":
				toks = append(toks, ruleToken{"op", two})
				i += 2
				continue
			}
			switch c {
			case '>', '<', '!', '(', ')':
				toks = append(toks, ruleToken{"op", string(c)})
				i++
			default:
				return nil, fmt.Errorf("unexpected character %q at %d", c, i)
			}
		}
	}
	return append(toks, ruleToken{"end", ""}), nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

type ruleParser struct {
	toks []ruleToken
	pos  int
}

func (p *ruleParser) peek() ruleToken { return p.toks[p.pos] }
func (p *ruleParser) next() ruleToken {
	t := p.toks[p.pos]
	if t.kind != "end" {
		p.pos++
	}
	return t
}

// parseRule parses a rule's `if`.
func parseRule(src string) (ruleExpr, error) {
	if strings.TrimSpace(src) == "" {
		return nil, fmt.Errorf("empty rule")
	}
	toks, err := lexRule(src)
	if err != nil {
		return nil, err
	}
	p := &ruleParser{toks: toks}
	x, err := p.or()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != "end" {
		return nil, fmt.Errorf("unexpected %q", t.text)
	}
	return x, nil
}

func (p *ruleParser) or() (ruleExpr, error) {
	l, err := p.and()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == "op" && p.peek().text == "||" {
		p.next()
		r, err := p.and()
		if err != nil {
			return nil, err
		}
		l = exprOr{l, r}
	}
	return l, nil
}

func (p *ruleParser) and() (ruleExpr, error) {
	l, err := p.unary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == "op" && p.peek().text == "&&" {
		p.next()
		r, err := p.unary()
		if err != nil {
			return nil, err
		}
		l = exprAnd{l, r}
	}
	return l, nil
}

func (p *ruleParser) unary() (ruleExpr, error) {
	if t := p.peek(); t.kind == "op" && t.text == "!" {
		p.next()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return exprNot{x}, nil
	}
	return p.primary()
}

func (p *ruleParser) primary() (ruleExpr, error) {
	t := p.next()
	switch {
	case t.kind == "op" && t.text == "(":
		x, err := p.or()
		if err != nil {
			return nil, err
		}
		if c := p.next(); c.kind != "op" || c.text != ")" {
			return nil, fmt.Errorf("missing )")
		}
		return x, nil
	case t.kind == "ident":
		op := p.peek()
		if op.kind == "op" {
			switch op.text {
			case ">=", "<=", ">", "<", "==", "!=":
				p.next()
				n := p.next()
				if n.kind != "num" {
					return nil, fmt.Errorf("%s %s needs a number", t.text, op.text)
				}
				f, err := strconv.ParseFloat(n.text, 64)
				if err != nil {
					return nil, fmt.Errorf("bad number %q", n.text)
				}
				return exprCmp{t.text, op.text, f}, nil
			}
		}
		return exprFlag{t.text}, nil
	case t.kind == "end":
		return nil, fmt.Errorf("unexpected end")
	default:
		return nil, fmt.Errorf("unexpected %q", t.text)
	}
}
