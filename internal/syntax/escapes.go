package syntax

import (
	"unicode/utf8"
)

func (p *parser) parseEscapeAtom(pos int) (*Atom, error) {
	if p.pos+1 >= len(p.src) {
		return nil, p.errf(pos, "trailing backslash")
	}
	c := p.src[p.pos+1]
	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		// Shorthands compile to singleton classes; negated shorthands also
		// match invalid-UTF-8 symbols, encoded by Negated=true.
		p.pos += 2
		neg := c == 'D' || c == 'W' || c == 'S'
		short := c
		if neg {
			short = short + ('a' - 'A') // D->d etc.
		}
		return &Atom{Kind: KindClass, Negated: neg, Pos: pos, End: p.pos,
			Members: []ClassMember{{Short: short, IsShort: true}}}, nil
	case 'b', 'B':
		p.pos += 2
		ak := AnchorWB
		if c == 'B' {
			ak = AnchorNWB
		}
		return &Atom{Kind: KindAnchor, Anchor: ak, Pos: pos, End: p.pos}, nil
	case 'p', 'P':
		return p.parseProperty(pos, c == 'P')
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		start := p.pos + 1
		p.pos++
		s := p.pos
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
		n := parseInt(p.src[s:p.pos])
		_ = start
		return &Atom{Kind: KindBackref, Index: n, Pos: pos, End: p.pos}, nil
	case '0':
		return nil, p.errf(pos, "octal/NULL escapes are not supported in %s", SyntaxVersion)
	case 'k':
		return nil, p.errf(pos, "\\k named backreferences are not supported; use \\1 or \\g{name}")
	case 'g':
		return nil, p.errf(pos, "\\g backreferences are not supported in %s", SyntaxVersion)
	default:
		r, sz := decodeSimpleEscape(p.src[p.pos:])
		if r < 0 {
			return nil, p.errf(pos, "unsupported escape \\%c", c)
		}
		p.pos += sz
		return &Atom{Kind: KindLiteral, Value: r, Pos: pos, End: p.pos}, nil
	}
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// decodeSimpleEscape resolves a backslash sequence starting at s[0]=='\\'
// and returns the rune plus consumed bytes, or (-1, 0) when unsupported.
func decodeSimpleEscape(s string) (rune, int) {
	if len(s) < 2 {
		return -1, 0
	}
	c := s[1]
	if r, ok := simpleEscapes[c]; ok {
		return r, 2
	}
	if c == 'x' {
		if len(s) >= 4 {
			if n, ok := hex2(s[2:4]); ok {
				return rune(n), 4
			}
		}
		if len(s) >= 3 && s[2] == '{' {
			i := 3
			for i < len(s) && s[i] != '}' {
				i++
			}
			if i <= len(s) && i > 3 {
				if n, ok := hexN(s[3:i]); ok && n <= utf8.MaxRune {
					return rune(n), i + 1
				}
			}
		}
		return -1, 0
	}
	if c == 'u' && len(s) >= 6 {
		if n, ok := hexN(s[2:6]); ok {
			return rune(n), 6
		}
		return -1, 0
	}
	if c == 'a' && len(s) >= 3 { // \aN not used; avoid conflict with alert
	}
	// Any other escaped punctuation is the literal itself ("\/", "\.", ...).
	if isPunctOrSpace(c) {
		return rune(c), 2
	}
	return -1, 0
}

var simpleEscapes = map[byte]rune{
	'n': '\n', 'r': '\r', 't': '\t', 'f': '\f', 'v': '\v',
	'a': 0x07, 'e': 0x1B,
}

func isPunctOrSpace(c byte) bool {
	switch c {
	case '!', '"', '#', '$', '%', '&', '\'', '(', ')', '*', '+', ',', '-',
		'.', '/', ':', ';', '<', '=', '>', '?', '@', '[', '\\', ']', '^',
		'_', '`', '{', '|', '}', '~', ' ':
		return true
	}
	return false
}

func hex2(s string) (int, bool) { return hexN(s) }

func hexN(s string) (int, bool) {
	if len(s) == 0 {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		var v int
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			v = int(c - '0')
		case c >= 'a' && c <= 'f':
			v = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v = int(c-'A') + 10
		default:
			return 0, false
		}
		n = n<<4 | v
	}
	return n, true
}

func (p *parser) parseProperty(pos int, neg bool) (*Atom, error) {
	p.pos += 2 // \p or \P
	if p.pos >= len(p.src) || p.src[p.pos] != '{' {
		return nil, p.errf(pos, "expected { after \\p")
	}
	p.pos++
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != '}' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return nil, p.errf(start, "unterminated property name")
	}
	name := p.src[start:p.pos]
	p.pos++ // }
	if name == "" {
		return nil, p.errf(start, "empty property name")
	}
	return &Atom{Kind: KindProperty, Prop: name, Negated: neg, Pos: pos, End: p.pos}, nil
}
