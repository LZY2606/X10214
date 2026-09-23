package syntax

import (
	"unicode/utf8"
)

func (p *parser) parseClass() (*Node, error) {
	start := p.pos
	p.pos++ // [
	n := NewNode(KClass)
	n.Flags = p.flags
	if p.peek() == '^' {
		p.pos++
		n.Negated = true
	}
	if p.peek() == ']' {
		// leading ] is a literal member
		n.Parts = append(n.Parts, ClassPart{Lo: ']', Hi: ']'})
		p.pos++
	}
	for p.peek() != ']' && p.peek() != -1 {
		parts, err := p.readClassMember()
		if err != nil {
			return nil, err
		}
		// possible range
		if p.peek() == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
			p.pos++ // -
			right, err := p.readClassMember()
			if err != nil {
				return nil, err
			}
			if len(parts) == 1 && len(right) == 1 {
				lo := parts[0]
				hi := right[0]
				if lo.Class == 0 && hi.Class == 0 && lo.Raw == hi.Raw {
					n.Parts = append(n.Parts, ClassPart{Lo: lo.Lo, Hi: hi.Hi, Raw: lo.Raw})
					continue
				}
				// invalid range endpoint: emit pieces literally
				n.Parts = append(n.Parts, parts...)
				n.Parts = append(n.Parts, ClassPart{Lo: '-', Hi: '-'})
				n.Parts = append(n.Parts, right...)
				continue
			}
			n.Parts = append(n.Parts, parts...)
			n.Parts = append(n.Parts, ClassPart{Lo: '-', Hi: '-'})
			n.Parts = append(n.Parts, right...)
			continue
		}
		n.Parts = append(n.Parts, parts...)
	}
	if p.peek() != ']' {
		return nil, p.errf("unterminated character class")
	}
	p.pos++
	n.Source = p.src[start:p.pos]
	return n, nil
}

func (p *parser) readClassMember() ([]ClassPart, error) {
	c := p.peek()
	if c == '\\' {
		return p.readClassEscape()
	}
	if c == '[' && p.src[p.pos:p.pos+2] != "[]" && p.pos+2 < len(p.src) && p.src[p.pos+1] == ':' {
		return p.readPOSIX()
	}
	if c == -1 {
		return nil, p.errf("unterminated class member")
	}
	// rune or raw byte
	if b := p.src[p.pos]; b < utf8RuneSelf {
		r := rune(b)
		p.pos++
		return []ClassPart{{Lo: r, Hi: r}}, nil
	}
	r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
	if r == utf8.RuneError && sz == 1 {
		b := p.src[p.pos]
		p.pos++
		return []ClassPart{{Lo: rune(b), Hi: rune(b), Raw: true}}, nil
	}
	p.pos += sz
	return []ClassPart{{Lo: r, Hi: r}}, nil
}

func (p *parser) readClassEscape() ([]ClassPart, error) {
	p.pos++ // backslash
	if p.pos >= len(p.src) {
		return nil, p.errf("trailing backslash in class")
	}
	c := p.src[p.pos]
	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		p.pos++
		return []ClassPart{{Class: c}}, nil
	case 'p', 'P':
		p.pos++
		prop, err := p.parseUnicodeProp()
		if err != nil {
			return nil, err
		}
		return []ClassPart{{Class: 'p', Prop: prop, Neg: c == 'P'}}, nil
	case 'n':
		p.pos++
		return []ClassPart{{Lo: '\n', Hi: '\n'}}, nil
	case 'r':
		p.pos++
		return []ClassPart{{Lo: '\r', Hi: '\r'}}, nil
	case 't':
		p.pos++
		return []ClassPart{{Lo: '\t', Hi: '\t'}}, nil
	case 'f':
		p.pos++
		return []ClassPart{{Lo: 0x0C, Hi: 0x0C}}, nil
	case 'v':
		p.pos++
		return []ClassPart{{Lo: 0x0B, Hi: 0x0B}}, nil
	case 'a':
		p.pos++
		return []ClassPart{{Lo: 0x07, Hi: 0x07}}, nil
	case 'x':
		p.pos++
		return p.readHexPart()
	case '0':
		p.pos++
		return []ClassPart{{Lo: 0, Hi: 0}}, nil
	}
	// escaped punctuation
	if c < utf8RuneSelf {
		p.pos++
		return []ClassPart{{Lo: rune(c), Hi: rune(c)}}, nil
	}
	r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += sz
	return []ClassPart{{Lo: r, Hi: r}}, nil
}

func (p *parser) readHexPart() ([]ClassPart, error) {
	if p.peek() == '{' {
		p.pos++
		ds := p.pos
		for isHexDigit(p.peek()) {
			p.pos++
		}
		digits := p.src[ds:p.pos]
		if p.peek() != '}' || digits == "" {
			return nil, p.errf("bad \\x in class")
		}
		p.pos++
		v64, ok := parseHex32(digits)
		v := int(v64)
		if !ok || v > 0x10FFFF {
			return nil, p.errf("bad hex value")
		}
		return []ClassPart{{Lo: rune(v), Hi: rune(v)}}, nil
	}
	var d [2]byte
	for i := 0; i < 2; i++ {
		if !isHexDigit(p.peek()) {
			return nil, p.errf("bad \\x in class")
		}
		d[i] = p.src[p.pos]
		p.pos++
	}
	v64, _ := parseHex32(string(d[:]))
	v := int(v64)
	if v >= utf8RuneSelf {
		return []ClassPart{{Lo: rune(v), Hi: rune(v), Raw: true}}, nil
	}
	return []ClassPart{{Lo: rune(v), Hi: rune(v)}}, nil
}

func (p *parser) readPOSIX() ([]ClassPart, error) {
	// current pos at '['; pattern [:<name>:]
	p.pos += 2 // [:
	neg := false
	if p.peek() == '^' {
		neg = true
		p.pos++
	}
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != ':' {
		p.pos++
	}
	name := p.src[start:p.pos]
	if p.pos+1 >= len(p.src) || p.src[p.pos] != ':' || p.src[p.pos+1] != ']' {
		return nil, p.errf("bad POSIX class")
	}
	p.pos += 2
	entry, ok := posixMap[name]
	if !ok {
		return nil, p.errf("unknown POSIX class %q", name)
	}
	return []ClassPart{{Class: entry.Class, Prop: entry.Prop, Neg: neg}}, nil
}

type posixEntry struct {
	Class byte
	Prop  string
}

var posixMap = map[string]posixEntry{
	"alnum":  {'w', ""},
	"alpha":  {'o', "alpha"},
	"digit":  {'d', ""},
	"lower":  {'o', "lower"},
	"upper":  {'o', "upper"},
	"space":  {'s', ""},
	"blank":  {'o', "blank"},
	"cntrl":  {'o', "cntrl"},
	"print":  {'o', "print"},
	"graph":  {'o', "graph"},
	"punct":  {'o', "punct"},
	"xdigit": {'o', "xdigit"},
}
