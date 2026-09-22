package syntax

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

// ParseError carries a 1-based column for diagnostics.
type ParseError struct {
	Msg string
	Col int
}

func (e *ParseError) Error() string { return fmt.Sprintf("parse error at col %d: %s", e.Col, e.Msg) }

type parser struct {
	src string
	pos int

	caps    int
	names   map[string]int
	capName map[int]string

	flags uint8
}

// Parse compiles source into a Pattern.
func Parse(src string) (*Pattern, error) {
	p := &parser{
		src:     src,
		names:   map[string]int{},
		capName: map[int]string{},
	}
	root, _, err := p.parseAlternation(')', false)
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.src) {
		return nil, p.errorf("unexpected closing parenthesis")
	}
	return &Pattern{
		Root:     root,
		Source:   src,
		NumCaps:  p.caps,
		Names:    p.names,
		CapNames: p.capName,
		Flags:    p.flags,
	}, nil
}

func (p *parser) errorf(format string, args ...any) error {
	return &ParseError{Msg: fmt.Sprintf(format, args...), Col: p.pos + 1}
}

// parseAlternation consumes until stop byte ')' or end-of-input. The returned
// bounds enclose everything parsed including optional flags state changes.
func (p *parser) parseAlternation(stop byte, inGroup bool) (Node, int, error) {
	lo := p.pos
	var branches [][]Node
	cur := []Node{}
	for {
		if p.flags&FlagX != 0 {
			p.skipExtendedSpace()
		}
		if p.pos >= len(p.src) || p.src[p.pos] == ')' {
			break
		}
		if p.src[p.pos] == '|' {
			p.pos++
			branches = append(branches, cur)
			cur = nil
			continue
		}
		atom, err := p.parseAtom()
		if err != nil {
			return nil, 0, err
		}
		if atom != nil {
			cur = append(cur, atom)
		}
	}
	branches = append(branches, cur)
	if inGroup {
		if p.pos >= len(p.src) || p.src[p.pos] != ')' {
			return nil, 0, p.errorf("unterminated group")
		}
	}
	return p.buildAltSeq(branches, lo, p.pos), p.pos, nil
}

func (p *parser) buildAltSeq(branches [][]Node, lo, hi int) Node {
	if len(branches) == 1 {
		return &Seq{Kids: branches[0], SrcLo: lo, SrcHi: hi}
	}
	return &Alt{Branches: branches, SrcLo: lo, SrcHi: hi}
}

func (p *parser) skipExtendedSpace() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t' || p.src[p.pos] == '\n') {
		p.pos++
	}
}

func (p *parser) parseAtom() (Node, error) {
	c := p.src[p.pos]
	switch {
	case c == '(':
		return p.parseGroup()
	case c == '[':
		return p.parseClass()
	case c == '\\':
		return p.parseEscape()
	case c == '.' || c == '^' || c == '$':
		return p.parseSimple()
	case c == '*' || c == '+' || c == '?':
		return nil, p.errorf("quantifier with nothing to repeat")
	case c == '{' && p.looksLikeRepetition():
		return nil, p.errorf("quantifier with nothing to repeat")
	default:
		lo := p.pos
		r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += sz
		atom := &Char{R: r, SrcLo: lo, SrcHi: p.pos}
		return p.parseQuantifier(atom)
	}
}

func (p *parser) parseSimple() (Node, error) {
	lo := p.pos
	c := p.src[p.pos]
	p.pos++
	var n Node
	switch c {
	case '.':
		n = &Dot{SrcLo: lo, SrcHi: p.pos}
	case '^':
		n = &Anchor{Kind: AnchorLineStart, SrcLo: lo, SrcHi: p.pos}
	case '$':
		n = &Anchor{Kind: AnchorLineEnd, SrcLo: lo, SrcHi: p.pos}
	}
	// Anchors are zero width; quantifiers on them are legal but useless and
	// would be confusing, so reject them.
	if _, ok := n.(*Anchor); ok {
		if p.pos < len(p.src) && (p.src[p.pos] == '*' || p.src[p.pos] == '+' || p.src[p.pos] == '?' || p.src[p.pos] == '{') {
			return nil, p.errorf("quantifier on a zero-width assertion")
		}
		return n, nil
	}
	return p.parseQuantifier(n)
}

// looksLikeRepetition reports whether `{` starts a valid {n}, {n,} or {n,m}.
func (p *parser) looksLikeRepetition() bool {
	i := p.pos + 1
	start := i
	for i < len(p.src) && p.src[i] >= '0' && p.src[i] <= '9' {
		i++
	}
	if i == start {
		return false
	}
	if i < len(p.src) && p.src[i] == '}' {
		return true
	}
	if i < len(p.src) && p.src[i] == ',' {
		i++
		lo := i
		for i < len(p.src) && p.src[i] >= '0' && p.src[i] <= '9' {
			i++
		}
		_ = lo
		if i < len(p.src) && p.src[i] == '}' {
			return true
		}
	}
	return false
}

func (p *parser) parseQuantifier(atom Node) (Node, error) {
	if p.pos >= len(p.src) {
		return atom, nil
	}
	c := p.src[p.pos]
	var rep *Repeat
	lo := srcLoOf(atom)
	switch c {
	case '*', '+', '?':
		min, max := 0, -1
		if c == '+' {
			min = 1
		}
		if c == '?' {
			max = 1
		}
		p.pos++
		rep = &Repeat{Kid: atom, Min: min, Max: max, Greedy: true, SrcLo: lo, SrcHi: p.pos}
	case '{':
		if !p.looksLikeRepetition() {
			return atom, nil
		}
		min, max, unbounded, err := p.consumeRepetition()
		if err != nil {
			return nil, err
		}
		hi := p.pos
		r := &Repeat{Kid: atom, Min: min, SrcLo: lo, SrcHi: hi}
		if unbounded {
			r.Max = -1
		} else {
			r.Max = max
		}
		r.Greedy = true
		rep = r
	default:
		return atom, nil
	}
	// modifier suffix
	possessive := false
	if p.pos < len(p.src) && p.src[p.pos] == '+' {
		rep.Possessive = true
		possessive = true
		p.pos++
		rep.SrcHi = p.pos
	}
	if !possessive && p.pos < len(p.src) && p.src[p.pos] == '?' {
		rep.Greedy = false
		p.pos++
		rep.SrcHi = p.pos
	}
	if p.flags&FlagU != 0 {
		rep.Greedy = !rep.Greedy
	}
	if rep.Max != -1 && rep.Max < rep.Min {
		return nil, p.errorf("repetition range out of order: %d > %d", rep.Min, rep.Max)
	}
	if _, isAnchor := atom.(*Anchor); isAnchor {
		return nil, p.errorf("quantifier on a zero-width assertion")
	}
	return rep, nil
}

func (p *parser) consumeRepetition() (min, max int, unbounded bool, err error) {
	p.pos++ // skip {
	for p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		min = min*10 + int(p.src[p.pos]-'0')
		p.pos++
	}
	if p.src[p.pos] == '}' {
		p.pos++
		return min, min, false, nil
	}
	// p.src[p.pos] == ','
	p.pos++
	if p.src[p.pos] == '}' {
		p.pos++
		return min, 0, true, nil
	}
	for p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		max = max*10 + int(p.src[p.pos]-'0')
		p.pos++
	}
	if p.src[p.pos] != '}' {
		return 0, 0, false, p.errorf("malformed repetition")
	}
	p.pos++
	return min, max, false, nil
}

var _ = unicode.IsDigit
