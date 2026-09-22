package syntax

import (
	"strings"
	"unicode"
)

// perl classes: d D s S w W
func isPerlClass(r rune) bool {
	return r == 'd' || r == 'D' || r == 's' || r == 'S' || r == 'w' || r == 'W'
}

func (p *parser) parseEscape(flags FlagSet) (*Node, error) {
	p.pos++ // consume backslash
	r, ok := p.peek()
	if !ok {
		return nil, p.errf("trailing backslash")
	}
	switch {
	case r >= '1' && r <= '9':
		return p.parseBackref()
	case r == 'k':
		return p.parseNamedBackref()
	case isPerlClass(r):
		p.pos++
		n := p.newNode(NCharClass)
		n.Classes = []string{string(r)}
		n.Src = "\\" + string(r)
		return n, nil
	case r == 'p' || r == 'P':
		negate := r == 'P'
		p.pos++
		name, err := p.readUnicodeProp()
		if err != nil {
			return nil, err
		}
		n := p.newNode(NCharClass)
		n.Negate = negate
		n.Classes = []string{"p:" + name}
		n.Src = "\\p{" + name + "}"
		return n, nil
	case simpleEscapes[r] != 0 || r == 'b' || r == 'B' || r == 'a' || r == 'f' || r == 'n' || r == 'r' || r == 't' || r == 'v' || r == '0':
		return p.parseSimpleEscape(r, flags)
	case r == 'x':
		return p.parseHexEscape(flags)
	case r == 'u' || r == 'U':
		return p.parseUnicodeEscape(r, flags)
	case r == 'c':
		return p.parseControlEscape(flags)
	default:
		// Escaped punctuation or letter = literal; backslash before a
		// non-word rune is always a literal.
		p.pos++
		n := p.newNode(NChar)
		n.Rune = r
		n.Fold = flags.I
		n.Src = "\\" + string(r)
		return n, nil
	}
}

var simpleEscapes = map[rune]rune{
	'a': 0x07, 'f': 0x0C, 'n': '\n', 'r': '\r',
	't': '\t', 'v': '\v', '0': 0,
}

func (p *parser) parseSimpleEscape(r rune, flags FlagSet) (*Node, error) {
	if r == 'b' || r == 'B' {
		p.pos++
		n := p.newNode(NAnchor)
		if r == 'b' {
			n.Anchor = "wb"
		} else {
			n.Anchor = "nwb"
		}
		n.Src = "\\" + string(r)
		return n, nil
	}
	p.pos++
	val := simpleEscapes[r]
	n := p.newNode(NChar)
	n.Rune = val
	n.Fold = flags.I
	n.Src = "\\" + string(r)
	return n, nil
}

func (p *parser) parseHexEscape(flags FlagSet) (*Node, error) {
	p.pos++ // x
	r, ok := p.peek()
	if !ok {
		return nil, p.errf("incomplete \\x escape")
	}
	var hex string
	if r == '{' {
		p.pos++
		for {
			c, cok := p.peek()
			if !cok {
				return nil, p.errf("unterminated \\x{...}")
			}
			if c == '}' {
				p.pos++
				break
			}
			if !isHex(c) {
				return nil, p.errf("bad hex digit %q", string(c))
			}
			hex += string(c)
			p.pos++
		}
	} else {
		if len(p.runes)-p.pos < 2 {
			return nil, p.errf("incomplete \\x escape")
		}
		hex = string(p.runes[p.pos : p.pos+2])
		if !isHex(p.runes[p.pos]) || !isHex(p.runes[p.pos+1]) {
			return nil, p.errf("bad \\x escape")
		}
		p.pos += 2
	}
	v := parseHex(hex)
	if v < 0 || v > unicode.MaxRune {
		return nil, p.errf("\\x{%s} is not a valid code point", hex)
	}
	n := p.newNode(NChar)
	n.Rune = rune(v)
	n.Fold = flags.I
	n.Src = "\\x" + hex
	return n, nil
}

func (p *parser) parseUnicodeEscape(r rune, flags FlagSet) (*Node, error) {
	p.pos++
	need := 4
	if r == 'U' {
		need = 8
	}
	if len(p.runes)-p.pos < need {
		return nil, p.errf("incomplete \\%c escape", r)
	}
	var sb strings.Builder
	for i := 0; i < need; i++ {
		c := p.runes[p.pos+i]
		if !isHex(c) {
			return nil, p.errf("bad hex digit in \\%c escape", r)
		}
		sb.WriteRune(c)
	}
	p.pos += need
	v := parseHex(sb.String())
	if v < 0 || v > unicode.MaxRune || (v >= 0xD800 && v <= 0xDFFF) {
		return nil, p.errf("\\%c%s is not a valid scalar value", r, sb.String())
	}
	n := p.newNode(NChar)
	n.Rune = rune(v)
	n.Fold = flags.I
	n.Src = "\\" + string(r) + sb.String()
	return n, nil
}

func (p *parser) parseControlEscape(flags FlagSet) (*Node, error) {
	p.pos++ // c
	r, ok := p.peek()
	if !ok {
		return nil, p.errf("incomplete \\c escape")
	}
	p.pos++
	v := r % 32
	n := p.newNode(NChar)
	n.Rune = v
	n.Fold = flags.I
	n.Src = "\\c" + string(r)
	return n, nil
}

func (p *parser) parseBackref() (*Node, error) {
	start := p.pos
	for p.pos < len(p.runes) && p.runes[p.pos] >= '0' && p.runes[p.pos] <= '9' {
		p.pos++
	}
	idx := atoiRunes(p.runes[start:p.pos])
	if idx >= len(p.groups) {
		return nil, p.errf("backreference \\%d refers to an undefined group", idx)
	}
	n := p.newNode(NBackref)
	n.RefIndex = idx
	n.Src = string(p.runes[start:p.pos])
	return n, nil
}

func (p *parser) parseNamedBackref() (*Node, error) {
	p.pos++ // k
	r, ok := p.peek()
	if !ok || r != '<' {
		return nil, p.errf("expected \\k<name>")
	}
	p.pos++
	var sb strings.Builder
	for {
		c, cok := p.peek()
		if !cok {
			return nil, p.errf("unterminated named backreference")
		}
		if c == '>' {
			p.pos++
			break
		}
		sb.WriteRune(c)
		p.pos++
	}
	name := sb.String()
	if name == "" {
		return nil, p.errf("empty named backreference")
	}
	for _, g := range p.groups {
		if g.Name == name {
			n := p.newNode(NBackref)
			n.RefIndex = g.Index
			n.RefName = name
			n.Src = name
			return n, nil
		}
	}
	return nil, p.errf("named backreference \\k<%s> has no matching group", name)
}

func (p *parser) readUnicodeProp() (string, error) {
	r, ok := p.peek()
	if !ok || r != '{' {
		return "", p.errf("expected { after \\p")
	}
	p.pos++
	var sb strings.Builder
	for {
		c, cok := p.peek()
		if !cok {
			return "", p.errf("unterminated \\p{...}")
		}
		if c == '}' {
			p.pos++
			break
		}
		sb.WriteRune(c)
		p.pos++
	}
	name := strings.TrimSpace(sb.String())
	if name == "" {
		return "", p.errf("empty unicode property")
	}
	if !knownUnicodeProp(name) {
		return "", p.errf("unknown unicode property %q", name)
	}
	return name, nil
}

func isHex(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func parseHex(s string) int {
	v := 0
	for _, r := range s {
		v <<= 4
		switch {
		case r >= '0' && r <= '9':
			v += int(r - '0')
		case r >= 'a' && r <= 'f':
			v += int(r-'a') + 10
		case r >= 'A' && r <= 'F':
			v += int(r-'A') + 10
		}
	}
	return v
}

// knownUnicodeProp is the deliberately small, documented property set.
func knownUnicodeProp(name string) bool {
	switch name {
	case "L", "Letter", "Nd", "Number, Decimal Digit", "Decimal_Number",
		"N", "Number", "Z", "Separator", "Zs", "Space_Separator",
		"White_Space", "P", "Punctuation", "C", "Other", "S", "Symbol",
		"Ll", "Lu", "Lt", "Lo", "Latin", "ASCII", "Any":
		return true
	}
	return false
}
