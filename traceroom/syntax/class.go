package syntax

import (
	"unicode/utf8"
)

// parseClass parses a bracket expression starting at '['.
//
// Supported: negation ([^]), ranges (a-z), class shorthands (\d \w \s and
// inverses), \xHH/\x{...}, standard escapes, POSIX classes [:alpha:], and a
// leading/trailing literal ']'/'-'.
func (p *parser) parseClass() (Node, error) {
	lo := p.pos
	p.pos++ // [
	cls := &Class{SrcLo: lo}
	if p.pos < len(p.src) && p.src[p.pos] == '^' {
		cls.Negated = true
		p.pos++
	}
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		// leading ] is literal
		cls.Terms = append(cls.Terms, ClassTerm{Lo: ']', Hi: ']'})
		p.pos++
	}
	for p.pos < len(p.src) && p.src[p.pos] != ']' {
		// POSIX class
		if p.src[p.pos] == '[' && p.pos+1 < len(p.src) && p.src[p.pos+1] == ':' {
			term, err := p.parsePOSIXClass()
			if err != nil {
				return nil, err
			}
			cls.Terms = append(cls.Terms, term)
			continue
		}
		term, isShorthand, err := p.parseClassAtom()
		if err != nil {
			return nil, err
		}
		// range?
		if !isShorthand && p.pos < len(p.src) && p.src[p.pos] == '-' &&
			p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
			p.pos++ // consume -
			hiTerm, hiShorthand, err := p.parseClassAtom()
			if err != nil {
				return nil, err
			}
			if hiShorthand {
				return nil, p.errorf("range endpoint cannot be a shorthand class")
			}
			if term.Lo > hiTerm.Lo {
				return nil, p.errorf("character range out of order: %q > %q", string(term.Lo), string(hiTerm.Lo))
			}
			term.Hi = hiTerm.Lo
		} else if term.Hi == 0 {
			term.Hi = term.Lo
		}
		cls.Terms = append(cls.Terms, term)
	}
	if p.pos >= len(p.src) {
		return nil, p.errorf("unterminated character class")
	}
	p.pos++ // ]
	cls.SrcHi = p.pos
	return p.parseQuantifier(cls)
}

func (p *parser) parseClassAtom() (ClassTerm, bool, error) {
	c := p.src[p.pos]
	if c == '\\' {
		if p.pos+1 >= len(p.src) {
			return ClassTerm{}, false, p.errorf("trailing backslash in class")
		}
		esc := p.src[p.pos+1]
		if prop, neg, ok := shorthand(esc); ok {
			p.pos += 2
			return ClassTerm{Prop: prop, Neg: neg}, true, nil
		}
		r, err := p.readClassEscape()
		if err != nil {
			return ClassTerm{}, false, err
		}
		return ClassTerm{Lo: r, Hi: r}, false, nil
	}
	r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += sz
	return ClassTerm{Lo: r, Hi: r}, false, nil
}

// readClassEscape reads one escaped rune inside a bracket class (no
// quantifiers allowed).
func (p *parser) readClassEscape() (rune, error) {
	p.pos++ // backslash
	c := p.src[p.pos]
	if c == 'x' || c == 'u' {
		return p.parseHexEscape()
	}
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
		return rune(val), nil
	}
	if r, ok := simpleEscape(c); ok {
		p.pos++
		return r, nil
	}
	p.pos++
	r, _ := utf8.DecodeLastRuneInString(p.src[:p.pos])
	return r, nil
}

func (p *parser) parsePOSIXClass() (ClassTerm, error) {
	// at "[:"
	p.pos += 2
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != ':' {
		p.pos++
	}
	if p.pos+1 >= len(p.src) || p.src[p.pos+1] != ']' {
		return ClassTerm{}, p.errorf("malformed POSIX class")
	}
	name := p.src[start:p.pos]
	p.pos += 2
	neg := false
	if len(name) > 1 && name[0] == '^' {
		neg = true
		name = name[1:]
	}
	prop, ok := posixMap[name]
	if !ok {
		return ClassTerm{}, p.errorf("unknown POSIX class %q", name)
	}
	return ClassTerm{Prop: prop, Neg: neg}, nil
}

// posixMap maps POSIX names onto the engine property vocabulary.
var posixMap = map[string]string{
	"alnum":  "alnum",
	"alpha":  "alpha",
	"digit":  "d",
	"xdigit": "xdigit",
	"upper":  "upper",
	"lower":  "lower",
	"space":  "s",
	"blank":  "h",
	"cntrl":  "cntrl",
	"graph":  "graph",
	"print":  "print",
	"punct":  "punct",
}
