package syntax

import (
	"fmt"
	"unicode/utf8"
)

// Error describes a parse failure with a byte offset.
type Error struct {
	Offset int
	Msg    string
}

func (e *Error) Error() string { return fmt.Sprintf("syntax error at byte %d: %s", e.Offset, e.Msg) }

type parser struct {
	src   string
	pos   int
	flags Flags
}

// Parse compiles pattern text into an AST. The returned tree carries unique
// node ids and compiler-assigned data (capture numbers, flags).
func Parse(pattern string) (Node, error) {
	p := &parser{src: pattern, flags: Flags{U: true}}
	n, err := p.parseAlt(p.flags)
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.src) {
		return nil, &Error{Offset: p.pos, Msg: "unexpected trailing input"}
	}
	if n == nil {
		n = &Empty{}
	}
	assignIDs(n)
	if err := numberGroups(n, map[string]int{}); err != nil {
		return nil, err
	}
	return n, nil
}

func (p *parser) errf(at int, format string, args ...any) error {
	return &Error{Offset: at, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) peek() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) next() (rune, int) {
	if p.pos >= len(p.src) {
		return 0, 0
	}
	r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += sz
	return r, sz
}

func (p *parser) readRune() (rune, int) {
	if p.pos >= len(p.src) {
		return 0, 0
	}
	return utf8.DecodeRuneInString(p.src[p.pos:])
}

// parseAlt parses alternation using the inherited flags. Global flag changes
// encountered during parsing mutate f and therefore affect later alternatives
// in the same group (documented in SYNTAX.md).
func (p *parser) parseAlt(f Flags) (Node, error) {
	var branches []Node
	for {
		n, err := p.parseConcat(f)
		if err != nil {
			return nil, err
		}
		branches = append(branches, n)
		if p.peek() != '|' {
			break
		}
		p.pos++
		f = p.flags
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &Alt{Branches: branches}, nil
}

func (p *parser) parseConcat(f Flags) (Node, error) {
	var items []Node
	for {
		c := p.peek()
		if c == 0 || c == '|' || c == ')' {
			break
		}
		atom, err := p.parseAtom(f)
		if err != nil {
			return nil, err
		}
		atom, err = p.parseQuant(atom, f)
		if err != nil {
			return nil, err
		}
		items = append(items, atom)
	}
	if len(items) == 0 {
		return &Empty{}, nil
	}
	if len(items) == 1 {
		return items[0], nil
	}
	return &Concat{Items: items}, nil
}

func (p *parser) parseQuant(atom Node, f Flags) (Node, error) {
	c := p.peek()
	if c != '*' && c != '+' && c != '?' && c != '{' {
		return atom, nil
	}
	var min, max int
	var hasMax, unbounded bool
	start := p.pos
	switch c {
	case '*':
		p.pos++
		min, max, unbounded = 0, 0, true
	case '+':
		p.pos++
		min, max, unbounded = 1, 0, true
	case '?':
		p.pos++
		min, max, hasMax = 0, 1, true
	case '{':
		save := p.pos
		p.pos++
		lo, loOK := p.readDecimal()
		hi, hiOK := lo, loOK
		hm, unb := false, false
		if p.peek() == ',' {
			p.pos++
			hi, hiOK = p.readDecimal()
			hm, unb = true, !hiOK
		}
		if !loOK || p.peek() != '}' {
			p.pos = save
			return atom, nil
		}
		p.pos++
		min = lo
		if unb {
			unbounded = true
		} else {
			max, hasMax = hi, hm || true
			if hi < lo {
				return nil, p.errf(start, "repetition range min > max")
			}
		}
		_ = hasMax
	}
	greedy := true
	possessive := false
	if p.peek() == '?' {
		p.pos++
		greedy = false
	} else if p.peek() == '+' {
		return nil, p.errf(p.pos, "possessive quantifiers are not supported in syntax version 1.0")
	}
	if _, ok := atom.(*Anchor); ok {
		return nil, p.errf(start, "quantifier on zero-width assertion")
	}
	if _, ok := atom.(*Empty); ok {
		return nil, p.errf(start, "quantifier on empty subexpression")
	}
	r := &Repeat{Min: min, Max: max, Greedy: greedy, Possessive: possessive, Body: atom}
	if unbounded {
		r.Max = -1
	}
	_ = f
	return r, nil
}

func (p *parser) readDecimal() (int, bool) {
	start := p.pos
	v := 0
	ok := false
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		v = v*10 + int(p.src[p.pos]-'0')
		p.pos++
		ok = true
	}
	_ = start
	return v, ok
}
