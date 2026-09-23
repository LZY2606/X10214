package regex

import (
	"unicode/utf8"
)

func (p *parser) parseClass() (Node, error) {
	pos := p.pos
	p.pos++ // [
	cls := &AtomClass{baseNode: baseNode{pos}}
	if p.peek() == '^' {
		cls.Negate = true
		p.pos++
	}
	first := true
	for {
		c := p.peek()
		if c == -1 {
			return nil, &ParseError{Offset: p.pos, Msg: "unterminated character class"}
		}
		if c == ']' && !first {
			p.pos++
			break
		}
		first = false
		if c == '-' {
			// a '-' that appears here is a literal hyphen.
			elem, err := p.classLiteral('-')
			if err != nil {
				return nil, err
			}
			cls.Elems = append(cls.Elems, elem)
			continue
		}
		elem, err := p.parseClassAtom()
		if err != nil {
			return nil, err
		}
		if p.peek() == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
			p.pos++ // consume '-'
			if p.peek() == '-' {
				// 'a--b' style is not supported; treat second '-' literally below
			}
			hi, err := p.parseClassAtom()
			if err != nil {
				return nil, err
			}
			if elem.Perl != 0 || elem.Prop != "" || hi.Perl != 0 || hi.Prop != "" {
				return nil, &ParseError{Offset: pos, Msg: "range endpoint must be a literal"}
			}
			if elem.Lo > hi.Lo {
				return nil, &ParseError{Offset: pos, Msg: "character class range out of order"}
			}
			cls.Elems = append(cls.Elems, ClassElem{Lo: elem.Lo, Hi: hi.Lo})
			continue
		}
		cls.Elems = append(cls.Elems, elem)
	}
	return cls, nil
}

func (p *parser) classLiteral(c rune) (ClassElem, error) {
	if p.peek() != int(c) {
		return ClassElem{}, &ParseError{Offset: p.pos, Msg: "expected literal"}
	}
	p.pos++
	return ClassElem{Lo: c, Hi: c}, nil
}

func (p *parser) parseClassAtom() (ClassElem, error) {
	pos := p.pos
	c := p.peek()
	if c == '\\' {
		p.pos++
		esc := p.peek()
		if esc == -1 {
			return ClassElem{}, &ParseError{Offset: pos, Msg: "trailing backslash in class"}
		}
		switch esc {
		case 'd', 'w', 's':
			p.pos++
			return ClassElem{Perl: byte(esc)}, nil
		case 'D', 'W', 'S':
			p.pos++
			return ClassElem{Perl: byte(esc + 'a' - 'A'), NegRange: true}, nil
		case 'p', 'P':
			p.pos++
			prop, err := p.parseProp()
			if err != nil {
				return ClassElem{}, err
			}
			return ClassElem{Prop: prop, NegRange: esc == 'P'}, nil
		case 'x':
			p.pos++
			r, err := p.parseHex(2)
			if err != nil {
				return ClassElem{}, err
			}
			return ClassElem{Lo: r, Hi: r}, nil
		case 'u':
			p.pos++
			r, err := p.parseHex(4)
			if err != nil {
				return ClassElem{}, err
			}
			return ClassElem{Lo: r, Hi: r}, nil
		case 'n', 'r', 't', 'f', 'a', 'v':
			p.pos++
			var r rune
			switch esc {
			case 'n':
				r = '\n'
			case 'r':
				r = '\r'
			case 't':
				r = '\t'
			case 'f':
				r = '\f'
			case 'a':
				r = '\a'
			case 'v':
				r = '\v'
			}
			return ClassElem{Lo: r, Hi: r}, nil
		default:
			p.pos++
			r, _ := utf8.DecodeRuneInString(p.src[p.pos-1:])
			return ClassElem{Lo: r, Hi: r}, nil
		}
	}
	p.pos++
	if c < 128 {
		return ClassElem{Lo: rune(c), Hi: rune(c)}, nil
	}
	r, size := utf8.DecodeRuneInString(p.src[pos:])
	if r == utf8.RuneError && size == 1 {
		return ClassElem{Lo: rune(p.src[pos]), Hi: rune(p.src[pos]), Invalid: true}, nil
	}
	p.pos = pos + size
	return ClassElem{Lo: r, Hi: r}, nil
}
