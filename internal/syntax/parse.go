package syntax

import (
	"fmt"
)

// ParseError describes a grammar failure at a byte offset.
type ParseError struct {
	Offset int
	Msg    string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("pattern error at byte %d: %s", e.Offset, e.Msg)
}

// Parse converts source text into an AST. It also returns the number of
// capturing groups and the name->index table.
func Parse(src string) (Node, int, map[string]int, error) {
	p := &parser{src: src, names: map[string]int{}}
	root := p.parseAlternation(Flags{})
	if p.err != nil {
		return nil, 0, nil, p.err
	}
	if p.i < len(p.src) {
		return nil, 0, nil, &ParseError{Offset: p.i, Msg: fmt.Sprintf("unexpected %q", p.src[p.i])}
	}
	return root, p.groups, p.names, nil
}

type parser struct {
	src    string
	i      int
	groups int
	names  map[string]int
	err    *ParseError
}

func (p *parser) fail(at int, msg string) {
	if p.err == nil {
		p.err = &ParseError{Offset: at, Msg: msg}
	}
}

func (p *parser) parseAlternation(fl Flags) Node {
	start := p.i
	alts := []Node{p.parseConcat(fl)}
	if p.err != nil {
		return nil
	}
	for p.i < len(p.src) && p.src[p.i] == '|' {
		p.i++
		alts = append(alts, p.parseConcat(fl))
		if p.err != nil {
			return nil
		}
	}
	if len(alts) == 1 {
		return alts[0]
	}
	return &AlternationNode{Offset: start, Alternatives: alts}
}

func (p *parser) parseConcat(fl Flags) Node {
	start := p.i
	var items []Node
	for p.i < len(p.src) {
		c := p.src[p.i]
		if c == '|' || c == ')' {
			break
		}
		atom := p.parseAtom(fl)
		if p.err != nil {
			return nil
		}
		atom = p.parseQuantifier(atom)
		if p.err != nil {
			return nil
		}
		if atom != nil {
			items = append(items, atom)
		}
	}
	if len(items) == 1 {
		return items[0]
	}
	return &ConcatNode{Offset: start, Items: items}
}

type bounds struct {
	min    int
	max    int
	pos    int
	valid  bool
	lazy   bool
	greedy bool
}

func (p *parser) parseQuantifier(atom Node) Node {
	if p.i >= len(p.src) {
		return atom
	}
	b := bounds{greedy: true, max: -1}
	switch c := p.src[p.i]; {
	case c == '*':
		b.pos, b.min, b.max = p.i, 0, -1
		p.i++
	case c == '+':
		b.pos, b.min, b.max = p.i, 1, -1
		p.i++
	case c == '?':
		b.pos, b.min, b.max = p.i, 0, 1
		p.i++
	case c == '{':
		b = p.tryBounded()
		if !b.valid {
			return atom
		}
	default:
		return atom
	}
	if p.i < len(p.src) && p.src[p.i] == '?' {
		p.i++
		b.greedy = false
		b.lazy = true
	}
	if atom == nil {
		p.fail(b.pos, "quantifier with no preceding atom")
		return nil
	}
	if _, ok := atom.(*LookaroundNode); ok {
		p.fail(b.pos, "quantifier applied to a lookaround assertion")
		return nil
	}
	if b.max >= 0 && b.min > b.max {
		p.fail(b.pos, "quantifier minimum exceeds maximum")
		return nil
	}
	return &QuantNode{Offset: b.pos, Child: atom, Min: b.min, Max: b.max, Greedy: b.greedy}
}

// tryBounded parses {n}, {n,} or {n,m} only when fully well-formed.
func (p *parser) tryBounded() bounds {
	j := p.i + 1
	n, digits := 0, 0
	for j < len(p.src) && p.src[j] >= '0' && p.src[j] <= '9' {
		n = n*10 + int(p.src[j]-'0')
		digits++
		j++
}
	if digits == 0 {
		return bounds{}
	}
	min, max := n, n
	if j < len(p.src) && p.src[j] == ',' {
		j++
		hi, hiDigits := 0, 0
		for j < len(p.src) && p.src[j] >= '0' && p.src[j] <= '9' {
			hi = hi*10 + int(p.src[j]-'0')
			hiDigits++
			j++
		}
		if hiDigits == 0 {
			max = -1
		} else {
			max = hi
		}
	}
	if j >= len(p.src) || p.src[j] != '}' {
		return bounds{}
	}
	pos := p.i
	p.i = j + 1
	return bounds{valid: true, pos: pos, min: min, max: max, greedy: true}
}
