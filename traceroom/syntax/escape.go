package syntax

import (
	"fmt"
	"strconv"
	"unicode/utf8"
)

// parseEscape handles a leading backslash outside character classes.
func (p *parser) parseEscape() (Node, error) {
	lo := p.pos
	p.pos++ // consume backslash
	if p.pos >= len(p.src) {
		return nil, p.errorf("trailing backslash")
	}
	c := p.src[p.pos]

	switch c {
	case 'A':
		p.pos++
		return &Anchor{Kind: AnchorStart, SrcLo: lo, SrcHi: p.pos}, nil
	case 'z':
		p.pos++
		return &Anchor{Kind: AnchorEnd, SrcLo: lo, SrcHi: p.pos}, nil
	case 'Z':
		p.pos++
		return &Anchor{Kind: AnchorEndHalf, SrcLo: lo, SrcHi: p.pos}, nil
	case 'b':
		p.pos++
		return &Anchor{Kind: AnchorWordBoundary, SrcLo: lo, SrcHi: p.pos}, nil
	case 'B':
		p.pos++
		return &Anchor{Kind: AnchorNotWordBoundary, SrcLo: lo, SrcHi: p.pos}, nil
	}

	if prop, neg, ok := shorthand(c); ok {
		p.pos++
		cls := &Class{SrcLo: lo, SrcHi: p.pos, Terms: []ClassTerm{{Prop: prop, Neg: neg}}}
		return p.parseQuantifier(cls)
	}

	// octal \nnn (1..3 octal digits, value <= 255)
	if c >= '0' && c <= '7' {
		val := 0
		n := 0
		for n < 3 && p.pos < len(p.src) {
			d := p.src[p.pos]
			if d < '0' || d > '7' {
				break
			}
			next := val*8 + int(d-'0')
			if next > 255 {
				break
			}
			val = next
			p.pos++
			n++
		}
		return p.parseQuantifier(&Char{R: rune(val), SrcLo: lo, SrcHi: p.pos})
	}

	// decimal backreference
	if c >= '1' && c <= '9' {
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
		id, _ := strconv.Atoi(p.src[start:p.pos])
		return p.parseQuantifier(&Backref{ID: id, SrcLo: lo, SrcHi: p.pos})
	}

	// named backreference \k<name> or \k'name'
	if c == 'k' {
		if p.pos+1 >= len(p.src) || (p.src[p.pos+1] != '<' && p.src[p.pos+1] != '\'') {
			return nil, p.errorf(`\k requires <name>`)
		}
		term := byte('>')
		if p.src[p.pos+1] == '\'' {
			term = '\''
		}
		nameStart := p.pos + 2
		i := nameStart
		for i < len(p.src) && p.src[i] != term {
			i++
		}
		if i >= len(p.src) {
			return nil, p.errorf("unterminated named backreference")
		}
		name := p.src[nameStart:i]
		id, err := p.resolveName(name)
		if err != nil {
			return nil, err
		}
		p.pos = i + 1
		return p.parseQuantifier(&Backref{ID: id, Name: name, SrcLo: lo, SrcHi: p.pos})
	}

	// hex escapes
	if c == 'x' || c == 'u' {
		r, err := p.parseHexEscape()
		if err != nil {
			return nil, err
		}
		return p.parseQuantifier(&Char{R: r, SrcLo: lo, SrcHi: p.pos})
	}

	if c == 'Q' {
		p.pos++
		start := p.pos
		for p.pos < len(p.src) {
			if p.src[p.pos] == '\\' && p.pos+1 < len(p.src) && p.src[p.pos+1] == 'E' {
				text := p.src[start:p.pos]
				p.pos += 2
				return p.literalSeq(text, lo), nil
			}
			p.pos++
		}
		return nil, p.errorf("unterminated \\Q (missing \\E)")
	}

	if r, ok := simpleEscape(c); ok {
		p.pos++
		return p.parseQuantifier(&Char{R: r, SrcLo: lo, SrcHi: p.pos})
	}

	// unknown escape: literal escaped rune
	p.pos++
	r, _ := utf8.DecodeLastRuneInString(p.src[:p.pos])
	return p.parseQuantifier(&Char{R: r, SrcLo: lo, SrcHi: p.pos})
}

func (p *parser) parseHexEscape() (rune, error) {
	// p.src[p.pos] is 'x' or 'u'
	if p.src[p.pos] == 'u' {
		p.pos++
		return p.readHex(4, true)
	}
	p.pos++ // x
	if p.pos < len(p.src) && p.src[p.pos] == '{' {
		p.pos++
		start := p.pos
		for p.pos < len(p.src) && isHex(p.src[p.pos]) {
			p.pos++
		}
		if p.pos >= len(p.src) || p.src[p.pos] != '}' || p.pos == start {
			return 0, p.errorf("malformed \\x{} escape")
		}
		v, _ := strconv.ParseInt(p.src[start:p.pos], 16, 32)
		p.pos++
		if v > utf8.MaxRune {
			return 0, p.errorf("\\x escape above U+10FFFF")
		}
		return rune(v), nil
	}
	return p.readHex(2, false)
}

func (p *parser) readHex(n int, strict bool) (rune, error) {
	start := p.pos
	for i := 0; i < n; i++ {
		if p.pos >= len(p.src) || !isHex(p.src[p.pos]) {
			if strict {
				return 0, p.errorf("expected %d hex digits", n)
			}
			break
		}
		p.pos++
	}
	if p.pos == start {
		return 0, p.errorf("expected hex digit")
	}
	v, _ := strconv.ParseInt(p.src[start:p.pos], 16, 32)
	if v > utf8.MaxRune {
		return 0, fmt.Errorf("hex escape above U+10FFFF")
	}
	return rune(v), nil
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// simpleEscape maps one-letter escapes after a backslash.
func simpleEscape(c byte) (rune, bool) {
	switch c {
	case 'n':
		return '\n', true
	case 'r':
		return '\r', true
	case 't':
		return '\t', true
	case 'f':
		return '\f', true
	case 'v':
		return '\v', true
	case 'a':
		return '\a', true
	case '0':
		return 0, true
	case 'e':
		return 0x1b, true
	}
	return 0, false
}

// shorthand maps class shorthands. `neg` indicates the uppercase inverse form.
func shorthand(c byte) (prop string, neg bool, ok bool) {
	switch c {
	case 'd':
		return "d", false, true
	case 'D':
		return "d", true, true
	case 'w':
		return "w", false, true
	case 'W':
		return "w", true, true
	case 's':
		return "s", false, true
	case 'S':
		return "s", true, true
	case 'h':
		return "h", false, true
	case 'H':
		return "h", true, true
	case 'v':
		return "Vspace", false, true
	case 'V':
		return "Vspace", true, true
	}
	return "", false, false
}

// literalSeq wraps \Q...\E text as a sequence of literal chars.
func (p *parser) literalSeq(text string, lo int) Node {
	kids := make([]Node, 0, len(text))
	i := 0
	for i < len(text) {
		r, sz := utf8.DecodeRuneInString(text[i:])
		start := lo + 2 + i
		kids = append(kids, &Char{R: r, SrcLo: start, SrcHi: start + sz})
		i += sz
	}
	return &Seq{Kids: kids, SrcLo: lo, SrcHi: p.pos}
}

func (p *parser) resolveName(name string) (int, error) {
	if id, ok := p.names[name]; ok {
		return id, nil
	}
	return 0, p.errorf("unknown named capture %q", name)
}
