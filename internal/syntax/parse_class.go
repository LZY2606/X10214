package syntax

import (
	"strconv"
	"unicode/utf8"
)

// class := '[' '^'? (']' | range)+ ']'
// Elements support \d-style classes, POSIX [:alpha:] and a-z intervals.
func (p *parser) parseClass(from int) (*Node, error) {
	p.pos++ // '['
	n := p.newNode(KindClass)
	if p.peek() == '^' {
		p.pos++
		n.Negated = true
	}
	if p.peek() == ']' {
		// a leading ] is a literal member
		p.pos++
		n.Ranges = append(n.Ranges, singleton(Atom{R: ']', RawByte: -1}))
	}
	for p.peek() != 0 && p.peek() != ']' {
		rng, err := p.parseClassElement()
		if err != nil {
			return nil, err
		}
		n.Ranges = append(n.Ranges, rng)
	}
	if p.peek() != ']' {
		return nil, p.errf("unterminated character class")
	}
	p.pos++
	n.CaseFold = p.flags().I
	n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
	return n, nil
}

func singleton(a Atom) *Range { return &Range{Lo: a, Hi: a, Closed: true} }

func (p *parser) parseClassAtom() (Atom, error) {
	if p.peek() == '\\' {
		p.pos++
		if p.pos >= len(p.src) {
			return Atom{}, p.errf("trailing backslash in class")
		}
		switch c := p.src[p.pos]; c {
		case 'n':
			p.pos++
			return Atom{R: '\n', RawByte: -1}, nil
		case 'r':
			p.pos++
			return Atom{R: '\r', RawByte: -1}, nil
		case 't':
			p.pos++
			return Atom{R: '\t', RawByte: -1}, nil
		case 'f':
			p.pos++
			return Atom{R: '\f', RawByte: -1}, nil
		case 'v':
			p.pos++
			return Atom{R: '\v', RawByte: -1}, nil
		case 'a':
			p.pos++
			return Atom{R: '\a', RawByte: -1}, nil
		case 'x':
			r, err := p.classHex()
			if err != nil {
				return Atom{}, err
			}
			return Atom{R: r, RawByte: -1}, nil
		case '0':
			p.pos++
			return Atom{R: 0, RawByte: -1}, nil
		case 'd', 'D', 'w', 'W', 's', 'S':
			// handled by caller via parseClassElement; signal through a marker
			p.pos--
			return Atom{}, errClassShortcut
		default:
			p.pos++
			return Atom{R: rune(c), RawByte: -1}, nil
		}
	}
	off := p.pos
	r, size := decodeAt(p.src, off)
	p.pos += size
	return atomFrom(r, p.src, off, size), nil
}

var errClassShortcut = &ParseError{Msg: "shortcut marker", Offset: -1}

func (p *parser) classHex() (rune, error) {
	p.pos++ // x
	if p.peek() == '{' {
		p.pos++
		begin := p.pos
		for isHex(p.peek()) {
			p.pos++
		}
		if p.peek() != '}' || p.pos == begin {
			return 0, p.errf("bad \\x{...} in class")
		}
		v, _ := strconv.ParseInt(p.src[begin:p.pos], 16, 32)
		p.pos++
		return rune(v), nil
	}
	if p.pos+2 > len(p.src) || !isHex(p.src[p.pos]) || !isHex(p.src[p.pos+1]) {
		return 0, p.errf("bad \\xHH in class")
	}
	v, _ := strconv.ParseInt(p.src[p.pos:p.pos+2], 16, 32)
	p.pos += 2
	return rune(v), nil
}

func (p *parser) parseClassElement() (*Range, error) {
	// POSIX class [:alpha:], collation [.a.], equivalence [=a=] (only class supported)
	if p.peek() == '[' && p.pos+1 < len(p.src) && (p.src[p.pos+1] == ':' || p.src[p.pos+1] == '.' || p.src[p.pos+1] == '=') {
		return p.parsePOSIX()
	}
	if p.peek() == '\\' && p.pos+1 < len(p.src) {
		c := p.src[p.pos+1]
		if c == 'd' || c == 'D' || c == 'w' || c == 'W' || c == 's' || c == 'S' {
			p.pos += 2
			return &Range{Class: string(c), Negate: c == 'D' || c == 'W' || c == 'S'}, nil
		}
	}
	lo, err := p.parseClassAtom()
	if err != nil {
		return nil, err
	}
	if p.peek() == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
		p.pos++ // '-'
		hi, err := p.parseClassAtom()
		if err != nil {
			return nil, err
		}
		if lo.RawByte >= 0 || hi.RawByte >= 0 || lo.R > hi.R {
			return nil, p.errf("invalid character range")
		}
		return &Range{Lo: lo, Hi: hi, Closed: true}, nil
	}
	return singleton(lo), nil
}

func (p *parser) parsePOSIX() (*Range, error) {
	kind := p.src[p.pos+1]
	p.pos += 2
	begin := p.pos
	for p.pos < len(p.src) && !(p.src[p.pos] == byte(kind) && p.pos+1 < len(p.src) && p.src[p.pos+1] == ']') {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return nil, p.errf("unterminated POSIX class")
	}
	name := p.src[begin:p.pos]
	p.pos += 2
	neg := false
	if len(name) > 0 && name[0] == '^' {
		neg = true
		name = name[1:]
	}
	if kind != ':' {
		return nil, p.errf("collating/equivalence classes are not supported")
	}
	if !posixSupported(name) {
		return nil, p.errf("unsupported POSIX class %q", name)
	}
	return &Range{Class: "[:" + name + ":]", Negate: neg}, nil
}

var _ = utf8.RuneLen
