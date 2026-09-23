package syntax

import (
	"strconv"
	"unicode/utf8"
)

func decodeRuneInString(s string) (rune, int) {
	return utf8.DecodeRuneInString(s)
}

func (p *parser) parseEscape() (*Node, error) {
	start := p.pos
	p.pos++ // backslash
	if p.pos >= len(p.src) {
		return nil, p.errf("trailing backslash")
	}
	c := p.src[p.pos]
	switch {
	case c >= '1' && c <= '9':
		return p.parseBackref(start)
	case c == 'k':
		return p.parseNamedBackref(start)
	}

	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		p.pos++
		n := NewNode(KClass)
		n.Flags = p.flags
		n.Parts = []ClassPart{{Class: c}}
		return n, nil
	case 'p', 'P':
		p.pos++
		prop, err := p.parseUnicodeProp()
		if err != nil {
			return nil, err
		}
		n := NewNode(KClass)
		n.Flags = p.flags
		n.Parts = []ClassPart{{Class: 'p', Prop: prop, Neg: c == 'P'}}
		return n, nil
	case 'b', 'B':
		p.pos++
		return p.anchor(c), nil
	case 'A', 'z':
		p.pos++
		return p.anchor(c), nil
	case 'a':
		p.pos++
		return p.literalByte(start, 0x07)
	case 'f':
		p.pos++
		return p.literalByte(start, 0x0C)
	case 'n':
		p.pos++
		return p.literalByte(start, '\n')
	case 'r':
		p.pos++
		return p.literalByte(start, '\r')
	case 't':
		p.pos++
		return p.literalByte(start, '\t')
	case 'v':
		p.pos++
		return p.literalByte(start, 0x0B)
	case 'x':
		p.pos++
		return p.parseHex(start)
	case '0':
		p.pos++
		return p.literalByte(start, 0)
	case 'Q':
		return p.parseQuoted(start)
	}
	// escaped punctuation is literal
	p.pos++
	n := NewNode(KChar)
	n.Rune = rune(c)
	n.Flags = p.flags
	n.Source = p.src[start:p.pos]
	return n, nil
}

func (p *parser) literalByte(start int, b byte) (*Node, error) {
	n := NewNode(KChar)
	n.Rune = rune(b)
	n.Flags = p.flags
	n.Source = p.src[start:p.pos]
	return n, nil
}

func (p *parser) parseHex(start int) (*Node, error) {
	if p.peek() == '{' {
		p.pos++
		digStart := p.pos
		for isHexDigit(p.peek()) {
			p.pos++
		}
		digits := p.src[digStart:p.pos]
		if err := p.expect('}'); err != nil || digits == "" {
			return nil, p.errf("bad \\x{} escape")
		}
		v, err := strconv.ParseUint(digits, 16, 32)
		if err != nil || v > 0x10FFFF {
			return nil, p.errf("bad \\x{%s}", digits)
		}
		return p.runeNode(start, rune(v)), nil
	}
	var d [2]byte
	for i := 0; i < 2; i++ {
		c := p.peek()
		if !isHexDigit(c) {
			return nil, p.errf("bad \\x escape")
		}
		d[i] = p.src[p.pos]
		p.pos++
	}
	v, _ := strconv.ParseUint(string(d[:]), 16, 8)
	if v >= utf8RuneSelf {
		n := NewNode(KChar)
		n.RawByte = int(v)
		n.Flags = p.flags
		n.Source = p.src[start:p.pos]
		return n, nil
	}
	return p.runeNode(start, rune(v)), nil
}

func (p *parser) runeNode(start int, r rune) *Node {
	n := NewNode(KChar)
	n.Rune = r
	n.Flags = p.flags
	n.Source = p.src[start:p.pos]
	return n
}

func (p *parser) parseQuoted(start int) (*Node, error) {
	p.pos++ // Q
	qStart := p.pos
	for p.pos < len(p.src) {
		if p.src[p.pos] == '\\' && p.pos+1 < len(p.src) && p.src[p.pos+1] == 'E' {
			break
		}
		p.pos++
	}
	if p.pos >= len(p.src) {
		return nil, p.errf("unterminated \\Q")
	}
	text := p.src[qStart:p.pos]
	p.pos += 2 // \E
	var kids []*Node
	for i := 0; i < len(text); {
		if text[i] < utf8RuneSelf {
			kids = append(kids, p.runeNode(start, rune(text[i])))
			i++
		} else {
			r, sz := utf8.DecodeRuneInString(text[i:])
			kids = append(kids, p.runeNode(start, r))
			i += sz
		}
	}
	if len(kids) == 1 {
		return kids[0], nil
	}
	n := NewNode(KConcat)
	n.Flags = p.flags
	n.Kids = kids
	n.Source = p.src[start:p.pos]
	return n, nil
}

func (p *parser) parseUnicodeProp() (string, error) {
	if p.peek() != '{' {
		return "", p.errf("expected { after \\p")
	}
	p.pos++
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != '}' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return "", p.errf("unterminated \\p{")
	}
	prop := p.src[start:p.pos]
	p.pos++
	return prop, nil
}

func (p *parser) parseBackref(start int) (*Node, error) {
	dstart := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	num, _ := strconv.Atoi(p.src[dstart:p.pos])
	if num > p.groups {
		return nil, p.errf("backreference to undefined group %d", num)
	}
	n := NewNode(KBackref)
	n.RefIndex = num
	n.Flags = p.flags
	n.Source = p.src[start:p.pos]
	return n, nil
}

func (p *parser) parseNamedBackref(start int) (*Node, error) {
	p.pos++ // k
	if p.peek() != '<' {
		return nil, p.errf("expected < after \\k")
	}
	p.pos++
	nstart := p.pos
	for p.pos < len(p.src) && isNameByte(p.src[p.pos]) {
		p.pos++
	}
	name := p.src[nstart:p.pos]
	if name == "" || p.peek() != '>' {
		return nil, p.errf("bad \\k<name>")
	}
	p.pos++
	n := NewNode(KBackref)
	n.RefName = name
	n.RefIndex = p.names[name]
	n.Flags = p.flags
	n.Source = p.src[start:p.pos]
	return n, nil
}

func isHexDigit(c int) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
