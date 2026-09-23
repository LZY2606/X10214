package syntax

import (
	"unicode/utf8"
)

// ClassMember is one element of a bracket character class.
type ClassMember struct {
	Single  rune
	Lo      rune
	Hi      rune
	IsRange bool
	Short   byte // d w s
	IsShort bool
	Prop    string
	NegProp bool
	IsProp  bool
}

func (p *parser) parseClass(pos int) (*Atom, error) {
	p.pos++ // [
	neg := false
	if p.pos < len(p.src) && p.src[p.pos] == '^' {
		neg = true
		p.pos++
	}
	var members []ClassMember
	first := true
	for {
		if p.pos >= len(p.src) {
			return nil, p.errf(pos, "unterminated character class, missing ']'")
		}
		if p.src[p.pos] == ']' && !first {
			p.pos++
			break
		}
		first = false
		m, err := p.parseClassAtom()
		if err != nil {
			return nil, err
		}
		// Range a-z?
		if m.Single >= 0 && p.pos < len(p.src) && p.src[p.pos] == '-' &&
			p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
			p.pos++ // -
			right, err := p.parseClassAtom()
			if err != nil {
				return nil, err
			}
			if right.Single < 0 {
				return nil, p.errf(p.pos, "range endpoint must be a literal")
			}
			if right.Single < m.Single {
				return nil, p.errf(p.pos, "range out of order: %U > %U", m.Single, right.Single)
			}
			members = append(members, ClassMember{Lo: m.Single, Hi: right.Single, IsRange: true})
			continue
		}
		members = append(members, m)
	}
	return &Atom{Kind: KindClass, Negated: neg, Members: members, Pos: pos, End: p.pos}, nil
}

func (p *parser) parseClassAtom() (ClassMember, error) {
	if p.pos >= len(p.src) {
		return ClassMember{}, p.errf(p.pos, "unterminated character class")
	}
	if p.src[p.pos] == '\\' {
		if p.pos+1 >= len(p.src) {
			return ClassMember{}, p.errf(p.pos, "trailing backslash in class")
		}
		c := p.src[p.pos+1]
		switch c {
		case 'd', 'w', 's', 'D', 'W', 'S':
			short := c
			neg := false
			if c == 'D' || c == 'W' || c == 'S' {
				short = c + ('a' - 'A')
				neg = true
			}
			p.pos += 2
			return ClassMember{Short: short, IsShort: true, NegProp: neg}, nil
		case 'p', 'P':
			pos := p.pos
			a, err := p.parseProperty(pos, c == 'P')
			if err != nil {
				return ClassMember{}, err
			}
			return ClassMember{Prop: a.Prop, IsProp: true, NegProp: a.Negated}, nil
		}
		r, sz := decodeSimpleEscape(p.src[p.pos:])
		if r < 0 {
			return ClassMember{}, p.errf(p.pos, "unsupported escape \\%c in class", c)
		}
		p.pos += sz
		return ClassMember{Single: r}, nil
	}
	c := p.src[p.pos]
	if c == '-' || c == ']' {
		p.pos++
		return ClassMember{Single: rune(c)}, nil
	}
	r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += sz
	return ClassMember{Single: r}, nil
}
