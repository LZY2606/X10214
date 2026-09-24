package syntax

import (
	"unicode"
	"unicode/utf8"
)

// parseClass parses '[' ... ']'; startAtom is the index of '['.
func (p *parser) parseClass(startAtom int) (*Class, error) {
	so := p.off[startAtom]
	c := &Class{}
	a, ok := p.peek()
	if ok && !a.bad && a.r == '^' {
		c.Negate = true
		p.eat()
	}
	// A leading ']' is a literal member.
	first := true
	for {
		a, ok := p.peek()
		if !ok || a.bad {
			return nil, p.errf(so, "unterminated character class")
		}
		if a.r == ']' && !first {
			p.eat()
			c.baseNode = baseNode{off: so, end: p.curOff()}
			return c, nil
		}
		first = false
		lo, err := p.readClassAtom()
		if err != nil {
			return nil, err
		}
		hi := lo
		// range?
		if d, ok := p.peek(); ok && !d.bad && d.r == '-' {
			// look ahead one
			if p.pos+1 < len(p.atoms) {
				nx := p.atoms[p.pos+1]
				if !nx.bad && nx.r != ']' {
					p.eat() // '-'
					hi, err = p.readClassAtom()
					if err != nil {
						return nil, err
					}
					if hi.r < lo.r {
						return nil, p.errf(so, "invalid character range")
					}
					if !lo.bad && !hi.bad {
						c.Ranges = append(c.Ranges, ClassRange{Lo: lo.r, Hi: hi.r})
						continue
					}
				}
			}
		}
		if lo.bad {
			// invalid byte represented as single-value rune sentinel
			c.Ranges = append(c.Ranges, ClassRange{Lo: utf8.RuneError, Hi: utf8.RuneError})
		} else {
			c.Ranges = append(c.Ranges, ClassRange{Lo: lo.r, Hi: lo.r})
		}
	}
}

type classAtom struct {
	r   rune
	bad bool
}

func (p *parser) readClassAtom() (classAtom, error) {
	a := p.eat()
	if a.bad {
		return classAtom{r: utf8.RuneError, bad: true}, nil
	}
	if a.r != '\\' {
		return classAtom{r: a.r}, nil
	}
	b, ok := p.peek()
	if !ok || b.bad {
		return classAtom{}, p.errf(p.curOff(), "trailing backslash in class")
	}
	switch b.r {
	case 'd', 'D', 'w', 'W', 's', 'S':
		// handled by caller via returned marker? Encode through Perl stack:
		// easiest: stash on parser temporarily; we return sentinel.
		p.eat()
		p.pendingPerl = append(p.pendingPerl, byte(b.r))
		return classAtom{r: perlSentinel}, nil
	case 'n':
		p.eat()
		return classAtom{r: '\n'}, nil
	case 'r':
		p.eat()
		return classAtom{r: '\r'}, nil
	case 't':
		p.eat()
		return classAtom{r: '\t'}, nil
	case 'f':
		p.eat()
		return classAtom{r: '\f'}, nil
	case 'v':
		p.eat()
		return classAtom{r: '\v'}, nil
	case 'a':
		p.eat()
		return classAtom{r: 0x07}, nil
	case '0':
		p.eat()
		return classAtom{r: 0}, nil
	case 'x':
		p.eat()
		r, err := p.readHex()
		if err != nil {
			return classAtom{}, err
		}
		return classAtom{r: r}, nil
	case 'p', 'P':
		neg := b.r == 'P'
		p.eat()
		rngs, err := p.readUnicodeClass(neg)
		if err != nil {
			return classAtom{}, err
		}
		return classAtom{r: unicodeSentinel, ranges: rngs}, nil
	default:
		p.eat()
		return classAtom{r: b.r}, nil
	}
}

const perlSentinel rune = -1
const unicodeSentinel rune = -2
