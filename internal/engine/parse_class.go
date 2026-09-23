package engine

// parseClass parses a bracket expression starting at '['.
func (p *parser) parseClass() (Node, error) {
	begin := p.pos
	p.pos++ // '['
	cc := &CharClass{baseNode: baseNode{begin, 0}, CI: p.flags.CI}
	if p.eat('^') {
		cc.Negated = true
	}
	if p.eat(']') {
		cc.Ranges = append(cc.Ranges, RuneRange{Lo: ']', Hi: ']'})
	}
	for p.peek() != ']' && p.peek() != 0 {
		member, err := p.readClassMember()
		if err != nil {
			return nil, err
		}
		// range?
		if p.peek() == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
			p.pos++ // consume '-'
			hi, err := p.readClassCodePoint()
			if err != nil {
				return nil, err
			}
			if member.r > hi {
				return nil, p.errf(begin, "character range is out of order")
			}
			cc.Ranges = append(cc.Ranges, RuneRange{Lo: member.r, Hi: hi})
			continue
		}
		if member.prop != nil {
			cc.Props = append(cc.Props, *member.prop)
		} else {
			cc.Ranges = append(cc.Ranges, RuneRange{Lo: member.r, Hi: member.r})
		}
	}
	if p.peek() != ']' {
		return nil, p.errf(begin, "unterminated character class")
	}
	p.pos++
	cc.E = p.pos
	return p.parseQuant(cc)
}

type classMember struct {
	r    rune
	prop *PropRef
}

func (p *parser) readClassMember() (classMember, error) {
	if p.peek() == '\\' {
		return p.readClassEscape()
	}
	r, _, err := p.decodeSourceRune()
	if err != nil {
		return classMember{}, err
	}
	return classMember{r: r}, nil
}

func (p *parser) readClassCodePoint() (rune, error) {
	m, err := p.readClassMember()
	if err != nil {
		return 0, err
	}
	if m.prop != nil {
		return 0, p.errf(p.pos, "range endpoint cannot be a character class")
	}
	return m.r, nil
}

func (p *parser) readClassEscape() (classMember, error) {
	at := p.pos
	p.pos++ // backslash
	if p.pos >= len(p.src) {
		return classMember{}, p.errf(at, "trailing backslash in class")
	}
	c := p.src[p.pos]
	switch c {
	case 'w', 'W', 'd', 'D', 's', 'S':
		p.pos++
		return classMember{prop: &PropRef{Name: "__" + string(c), Negated: c == 'W' || c == 'D' || c == 'S'}}, nil
	case 'p', 'P':
		p.pos++
		pr, err := p.readProp()
		if err != nil {
			return classMember{}, err
		}
		pr.Negated = c == 'P'
		return classMember{prop: pr}, nil
	case 'x':
		r, err := p.readHexEscape(2)
		if err != nil {
			return classMember{}, err
		}
		return classMember{r: r}, nil
	case 'u':
		r, err := p.readHexEscape(4)
		if err != nil {
			return classMember{}, err
		}
		return classMember{r: r}, nil
	case 'b':
		p.pos++
		return classMember{r: 0x08}, nil
	case 'n', 't', 'r', 'f', 'v', 'a', 'e', '0':
		r := simpleEscRune(c)
		p.pos++
		return classMember{r: r}, nil
	default:
		r, _, err := p.decodeSourceRune()
		if err != nil {
			return classMember{}, err
		}
		return classMember{r: r}, nil
	}
}
