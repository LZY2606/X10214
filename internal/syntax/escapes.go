package syntax

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

func (p *parser) parseEscapeAtom() (Node, error) {
	at := p.pos
	p.pos++ // backslash
	if p.pos >= Pos(len(p.src)) {
		return nil, p.errf(at, "trailing backslash")
	}
	c := p.consume()
	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		return ClassNode{P: at, Items: []ClassItem{{Property: string(c)}}}, nil
	case 'p', 'P':
		negate := c == 'P'
		prop, err := p.readUnicodeProperty()
		if err != nil {
			return nil, err
		}
		return ClassNode{P: at, Negate: negate, Items: []ClassItem{{Property: prop}}}, nil
	case 'b':
		return AnchorNode{P: at, Kind: AnchorWordBoundary}, nil
	case 'B':
		return AnchorNode{P: at, Kind: AnchorNotWordBoundary}, nil
	case 'A':
		return AnchorNode{P: at, Kind: AnchorTextStart}, nil
	case 'z':
		return AnchorNode{P: at, Kind: AnchorTextEnd}, nil
	case 'Z':
		return AnchorNode{P: at, Kind: AnchorEndOrFinalNL}, nil
	case 'a':
		return LitNode{P: at, R: '\a'}, nil
	case 'f':
		return LitNode{P: at, R: '\f'}, nil
	case 'n':
		return LitNode{P: at, R: '\n'}, nil
	case 'r':
		return LitNode{P: at, R: '\r'}, nil
	case 't':
		return LitNode{P: at, R: '\t'}, nil
	case 'v':
		return LitNode{P: at, R: '\v'}, nil
	case '0':
		return p.readOctal(at, true)
	case 'x':
		return p.readHexEscape(at)
	case 'u':
		return p.readFixedHex(at, 4)
	case 'Q':
		return p.readQuoted(at)
	case 'k':
		name, err := p.readBackrefName()
		if err != nil {
			return nil, err
		}
		return RefNode{P: at, Name: name}, nil
	}
	if c >= '1' && c <= '9' {
		digits := string(c)
		for p.peek() >= '0' && p.peek() <= '9' {
			digits += string(p.consume())
		}
		n, _ := strconv.Atoi(digits)
		if n > p.rx.NumGroups {
			return nil, p.errf(at, "invalid backreference \\%s: only %d capture group(s) exist", digits, p.rx.NumGroups)
		}
		return RefNode{P: at, Index: n}, nil
	}
	// Escaped punctuation is literal.
	return LitNode{P: at, R: rune(c)}, nil
}

func (p *parser) readOctal(at Pos, leadingZero bool) (Node, error) {
	digits := strings.Builder{}
	if leadingZero {
		digits.WriteByte('0')
	}
	for i := 0; i < 2; i++ {
		b := p.peek()
		if b >= '0' && b <= '7' {
			digits.WriteByte(b)
			p.pos++
		} else {
			break
		}
	}
	v, _ := strconv.ParseInt(digits.String(), 8, 32)
	return LitNode{P: at, R: rune(v)}, nil
}

func (p *parser) readHexEscape(at Pos) (Node, error) {
	if p.peek() == '{' {
		p.pos++
		var h strings.Builder
		for p.peek() != 0 && p.peek() != '}' {
			h.WriteByte(p.consume())
		}
		if p.peek() != '}' || h.Len() == 0 || h.Len() > 6 {
			return nil, p.errf(at, "malformed \\x{} escape")
		}
		p.pos++
		v, err := strconv.ParseInt(h.String(), 16, 32)
		if err != nil || v > 0x10FFFF || (v >= 0xD800 && v <= 0xDFFF) {
			return nil, p.errf(at, "invalid code point in \\x{%s}", h.String())
		}
		return LitNode{P: at, R: rune(v)}, nil
	}
	if p.pos+2 > Pos(len(p.src)) {
		return nil, p.errf(at, "malformed \\x escape")
	}
	v, err := strconv.ParseInt(p.src[p.pos:p.pos+2], 16, 32)
	if err != nil {
		return nil, p.errf(at, "malformed \\x escape")
	}
	p.pos += 2
	return LitNode{P: at, R: rune(v)}, nil
}

func (p *parser) readFixedHex(at Pos, n int) (Node, error) {
	if p.pos+Pos(n) > Pos(len(p.src)) {
		return nil, p.errf(at, "short unicode escape")
	}
	v, err := strconv.ParseInt(p.src[p.pos:p.pos+Pos(n)], 16, 32)
	if err != nil {
		return nil, p.errf(at, "malformed unicode escape")
	}
	p.pos += Pos(n)
	if v >= 0xD800 && v <= 0xDFFF {
		return nil, p.errf(at, "surrogate code point in escape")
	}
	return LitNode{P: at, R: rune(v)}, nil
}

func (p *parser) readQuoted(at Pos) (Node, error) {
	start := p.pos
	end := start
	for end < Pos(len(p.src)) {
		if p.src[end] == '\\' && end+1 < Pos(len(p.src)) && p.src[end+1] == 'E' {
			break
		}
		_, sz := utf8.DecodeRuneInString(p.src[end:])
		end += Pos(sz)
	}
	lit := p.src[start:end]
	if end < Pos(len(p.src)) {
		p.pos = end + 2 // \E
	} else {
		p.pos = end
	}
	if lit == "" {
		return EmptyNode{P: at}, nil
	}
	nodes := make([]Node, 0, len(lit))
	off := 0
	for off < len(lit) {
		r, sz := utf8.DecodeRuneInString(lit[off:])
		nodes = append(nodes, LitNode{P: at + Pos(off) + 2, R: r})
		off += sz
	}
	if len(nodes) == 1 {
		return nodes[0], nil
	}
	return CatNode{P: at, Nodes: nodes}, nil
}

func (p *parser) readBackrefName() (string, error) {
	c := p.peek()
	var close byte
	switch c {
	case '<':
		close = '>'
	case '\'':
		close = c
	default:
		return "", p.errf(p.pos, "expected < or ' after \\k")
	}
	p.pos++
	var b strings.Builder
	for p.peek() != 0 && p.peek() != close {
		b.WriteByte(p.consume())
	}
	if p.peek() != close {
		return "", p.errf(p.pos, "unterminated backreference name")
	}
	p.pos++
	name := b.String()
	if name == "" {
		return "", p.errf(p.pos, "empty backreference name")
	}
	return name, nil
}

func (p *parser) readUnicodeProperty() (string, error) {
	if p.peek() == '{' {
		p.pos++
		var b strings.Builder
		for p.peek() != 0 && p.peek() != '}' {
			b.WriteByte(p.consume())
		}
		if p.peek() != '}' {
			return "", p.errf(p.pos, "unterminated unicode property")
		}
		p.pos++
		prop := strings.TrimSpace(b.String())
		if prop == "" {
			return "", p.errf(p.pos, "empty unicode property")
		}
		if strings.HasPrefix(prop, "Is") {
			prop = prop[2:]
		}
		return prop, nil
	}
	if p.pos < Pos(len(p.src)) {
		c := p.consume()
		return string(c), nil
	}
	return "", p.errf(p.pos, "missing unicode property")
}

// parseClassEscape handles escapes that appear inside [...].
func (p *parser) parseClassEscape(at Pos) (rune, error) {
	c := p.consume()
	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		// These become "rune 0 with property marker" — handled by caller via
		// a sentinel below. Reuse read path by returning pseudo-rune.
		return pseudoPropertyRune(c), nil
	case 'p', 'P':
		prop, err := p.readUnicodeProperty()
		if err != nil {
			return 0, err
		}
		marker := 'p'
		if c == 'P' {
			marker = 'P'
		}
		_ = marker
		return 0, &classPropErr{negate: c == 'P', prop: prop}
	case 'a':
		return '\a', nil
	case 'f':
		return '\f', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case 'v':
		return '\v', nil
	case '0':
		n, _ := p.readOctal(at, true)
		return n.(LitNode).R, nil
	case 'x':
		n, err := p.readHexEscape(at)
		if err != nil {
			return 0, err
		}
		return n.(LitNode).R, nil
	case 'u':
		n, err := p.readFixedHex(at, 4)
		if err != nil {
			return 0, err
		}
		return n.(LitNode).R, nil
	}
	if c >= '0' && c <= '7' {
		// Up to three octal digits total.
		d := strings.Builder{}
		d.WriteByte(c)
		for i := 0; i < 2 && p.peek() >= '0' && p.peek() <= '7'; i++ {
			d.WriteByte(p.consume())
		}
		v, _ := strconv.ParseInt(d.String(), 8, 32)
		return rune(v), nil
	}
	if c >= '1' && c <= '9' {
		return 0, p.errf(at, "backreferences are not allowed inside character classes")
	}
	if c == 'b' {
		return 0x08, nil
	}
	return rune(c), nil
}

// pseudoPropertyRune encodes \d style classes inside brackets; the compiler
// treats negative rune values as property markers.
func pseudoPropertyRune(c byte) rune {
	switch c {
	case 'd':
		return propDRune
	case 'D':
		return propDURune
	case 'w':
		return propWRune
	case 'W':
		return propWURune
	case 's':
		return propSRune
	case 'S':
		return propSURune
	}
	return 0
}

const (
	propDRune  = -1
	propDURune = -2
	propWRune  = -3
	propWURune = -4
	propSRune  = -5
	propSURune = -6
)

type classPropErr struct {
	negate bool
	prop   string
}

func (e *classPropErr) Error() string {
	return fmt.Sprintf("unicode property %s inside class", e.prop)
}
