package syntax

import (
	"strconv"
	"unicode/utf8"
)

// atom := char | '.' | class | group | anchor | backref | escape
func (p *parser) parseAtom() (*Node, error) {
	from := p.pos
	c := p.peek()
	switch c {
	case 0, '|', ')':
		return nil, nil
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass(from)
	case '.':
		p.pos++
		n := p.newNode(KindAny)
		n.DotAll = p.flags().S
		n.CaseFold = p.flags().I
		n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
		return n, nil
	case '^':
		p.pos++
		n := p.newNode(KindAssertStart)
		n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
		return n, nil
	case '$':
		p.pos++
		n := p.newNode(KindAssertEnd)
		n.AtFinalNL = true
		n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
		return n, nil
	case '\\':
		return p.parseEscape(from)
	default:
		return p.parseLiteral(from)
	}
}

func (p *parser) parseLiteral(from int) (*Node, error) {
	r, size := decodeAt(p.src, p.pos)
	p.pos += size
	n := p.newNode(KindChar)
	n.Atom = atomFrom(r, p.src, from, size)
	n.CaseFold = p.flags().I
	n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
	return n, nil
}

// decodeAt decodes one unit at off. An invalid byte yields RuneError with
// size 1 so invalid UTF-8 is matched byte-by-byte.
func decodeAt(s string, off int) (rune, int) {
	r, size := utf8.DecodeRuneInString(s[off:])
	if r == utf8.RuneError && size == 1 {
		return RuneError, 1
	}
	return r, size
}

func atomFrom(r rune, src string, off, size int) Atom {
	if r == RuneError {
		return InvalidByte(src[off])
	}
	return Atom{R: r, RawByte: -1}
}

func (p *parser) parseEscape(from int) (*Node, error) {
	p.pos++ // consume backslash
	if p.pos >= len(p.src) {
		return nil, p.errf("trailing backslash")
	}
	c := p.src[p.pos]
	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		p.pos++
		n := p.newNode(KindClass)
		neg := c == 'D' || c == 'W' || c == 'S'
		n.Ranges = []*Range{{Class: string(c), Negate: neg}}
		n.Negated = false // the per-range Negate carries polarity
		n.CaseFold = p.flags().I
		n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
		return n, nil
	case 'b', 'B':
		p.pos++
		n := p.newNode(KindWordBound)
		n.WBPos = c == 'b'
		n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
		return n, nil
	case 'A':
		p.pos++
		n := p.newNode(KindAssertStart)
		n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
		return n, nil
	case 'z':
		p.pos++
		n := p.newNode(KindAssertEnd)
		n.AtFinalNL = false
		n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
		return n, nil
	case 'n', 'r', 't', 'f', 'v', '0', 'a':
		return p.parseSimpleEscape(from, c)
	case 'x':
		return p.parseHexEscape(from)
	case 'k':
		return p.parseNamedBackref(from)
	default:
		if c >= '1' && c <= '9' {
			return p.parseNumericBackref(from)
		}
		// escaped punctuation is a literal char
		p.pos++
		n := p.newNode(KindChar)
		n.Atom = Atom{R: rune(c), RawByte: -1}
		n.CaseFold = p.flags().I
		n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
		return n, nil
	}
}

func (p *parser) parseSimpleEscape(from int, c byte) (*Node, error) {
	var r rune
	switch c {
	case 'n':
		r = '\n'
	case 'r':
		r = '\r'
	case 't':
		r = '\t'
	case 'f':
		r = '\f'
	case 'v':
		r = '\v'
	case 'a':
		r = '\a'
	case '0':
		r = 0
	}
	p.pos++
	n := p.newNode(KindChar)
	n.Atom = Atom{R: r, RawByte: -1}
	n.CaseFold = p.flags().I
	n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
	return n, nil
}

func (p *parser) parseHexEscape(from int) (*Node, error) {
	p.pos++ // 'x'
	if p.peek() == '{' {
		p.pos++
		begin := p.pos
		for isHex(p.peek()) {
			p.pos++
		}
		if p.peek() != '}' || p.pos == begin {
			return nil, p.errf("bad \\x{...} escape")
		}
		v, _ := strconv.ParseInt(p.src[begin:p.pos], 16, 32)
		p.pos++
		return p.charNode(from, rune(v)), nil
	}
	if p.pos+2 > len(p.src) || !isHex(p.src[p.pos]) || !isHex(p.src[p.pos+1]) {
		return nil, p.errf("bad \\xHH escape")
	}
	v, _ := strconv.ParseInt(p.src[p.pos:p.pos+2], 16, 32)
	p.pos += 2
	return p.charNode(from, rune(v)), nil
}

func (p *parser) charNode(from int, r rune) *Node {
	n := p.newNode(KindChar)
	n.Atom = Atom{R: r, RawByte: -1}
	n.CaseFold = p.flags().I
	n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
	return n
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func (p *parser) parseNumericBackref(from int) (*Node, error) {
	begin := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	idx, _ := strconv.Atoi(p.src[begin:p.pos])
	n := p.newNode(KindBackref)
	n.Index = idx
	n.BackName = strconv.Itoa(idx)
	n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
	return n, nil
}

func (p *parser) parseNamedBackref(from int) (*Node, error) {
	p.pos++ // 'k'
	if p.peek() != '<' {
		return nil, p.errf("expected < after \\k")
	}
	p.pos++
	begin := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != '>' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return nil, p.errf("unterminated named backreference")
	}
	name := p.src[begin:p.pos]
	p.pos++ // '>'
	n := p.newNode(KindBackref)
	n.Name = name
	n.BackName = name
	n.From, n.To, n.Src = from, p.pos, p.src[from:p.pos]
	return n, nil
}
