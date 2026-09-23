package regex

import (
	"unicode/utf8"
)

func (p *parser) parseClass() (*Node, error) {
	start := p.pos
	p.pos++ // '['
	cc := &CharClass{}
	if p.pos < len(p.src) && p.src[p.pos] == '^' {
		cc.Negate = true
		p.pos++
	}
	// A leading ']' is a literal member.
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		cc.Elems = append(cc.Elems, ClassElem{Lo: ']', Hi: ']'})
		p.pos++
	}
	for p.pos < len(p.src) && p.src[p.pos] != ']' {
		lo, err := p.readClassUnit()
		if err != nil {
			return nil, err
		}
		// range?
		if lo.named == "" && p.pos+1 < len(p.src) && p.src[p.pos] == '-' && p.src[p.pos+1] != ']' {
			dash := p.pos
			p.pos++ // '-'
			hi, err := p.readClassUnit()
			if err != nil {
				return nil, err
			}
			if hi.named != "" {
				return nil, p.err(dash, "range endpoint cannot be a class shorthand")
			}
			if hi.r < lo.r {
				return nil, p.err(dash, "invalid character range %q-%q", lo.r, hi.r)
			}
			cc.Elems = append(cc.Elems, ClassElem{Lo: lo.r, Hi: hi.r})
			continue
		}
		if lo.named != "" {
			cc.Elems = append(cc.Elems, ClassElem{Named: lo.named, Prop: lo.prop})
		} else {
			cc.Elems = append(cc.Elems, ClassElem{Lo: lo.r, Hi: lo.r})
		}
	}
	if p.pos >= len(p.src) {
		return nil, p.err(start, "unterminated character class")
	}
	p.pos++ // ']'
	return &Node{Kind: KClass, Class: cc, Src: TextRange{start, p.pos}}, nil
}

type classUnit struct {
	r     rune
	named string
	prop  string
}

func (p *parser) readClassUnit() (classUnit, error) {
	if p.src[p.pos] != '\\' {
		r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += sz
		return classUnit{r: r}, nil
	}
	return p.readClassEscape()
}

func (p *parser) readClassEscape() (classUnit, error) {
	at := p.pos
	p.pos++ // backslash
	if p.pos >= len(p.src) {
		return classUnit{}, p.err(at, "trailing backslash")
	}
	c := p.src[p.pos]
	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		p.pos++
		return classUnit{named: string(c)}, nil
	case 'p', 'P':
		neg := c == 'P'
		p.pos++
		if p.pos >= len(p.src) || p.src[p.pos] != '{' {
			return classUnit{}, p.err(at, "expected { after \\%c", c)
		}
		p.pos++
		from := p.pos
		for p.pos < len(p.src) && p.src[p.pos] != '}' {
			p.pos++
		}
		if p.pos >= len(p.src) {
			return classUnit{}, p.err(at, "unterminated \\p property")
		}
		prop := p.src[from:p.pos]
		p.pos++
		if neg {
			return classUnit{named: "P", prop: prop}, nil
		}
		return classUnit{named: "p", prop: prop}, nil
	}
	r, _, err := p.readSimpleEscape()
	if err != nil {
		return classUnit{}, err
	}
	return classUnit{r: r}, nil
}
