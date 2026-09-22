package syntax

import "unicode"

func (p *parser) parseClass(flags FlagSet) (*Node, error) {
	p.pos++ // [
	n := p.newNode(NCharClass)
	n.Src = "["
	if p.eat('^') {
		n.Negate = true
		n.Src += "^"
	}
	if p.eat(']') {
		// A leading ] is a literal member.
		n.Ranges = append(n.Ranges, RuneRange{Lo: ']', Hi: ']'})
	}
	for {
		r, ok := p.peek()
		if !ok {
			return nil, p.errf("unterminated character class")
		}
		if r == ']' {
			p.pos++
			return n, nil
		}
		member, perl, err := p.readClassMember(flags)
		if err != nil {
			return nil, err
		}
		if perl != "" {
			n.Classes = append(n.Classes, perl)
			continue
		}
		// range?
		if p.pos < len(p.runes) && p.runes[p.pos] == '-' &&
			p.pos+1 < len(p.runes) && p.runes[p.pos+1] != ']' {
			p.pos++ // -
			hi, hperl, herr := p.readClassMember(flags)
			if herr != nil {
				return nil, herr
			}
			if hperl != "" {
				return nil, p.errf("range endpoint cannot be a class escape")
			}
			if member > hi {
				return nil, p.errf("range %q-%q is reversed", string(member), string(hi))
			}
			n.Ranges = append(n.Ranges, RuneRange{Lo: member, Hi: hi})
			continue
		}
		n.Ranges = append(n.Ranges, RuneRange{Lo: member, Hi: member})
	}
}

// readClassMember returns either a literal rune member or a perl/property
// class token (prefixed), never both.
func (p *parser) readClassMember(flags FlagSet) (rune, string, error) {
	r, ok := p.peek()
	if !ok {
		return 0, "", p.errf("unterminated character class")
	}
	if r == '\\' {
		p.pos++
		c, cok := p.peek()
		if !cok {
			return 0, "", p.errf("trailing backslash in class")
		}
		if isPerlClass(c) {
			p.pos++
			return 0, string(c), nil
		}
		if c == 'p' || c == 'P' {
			neg := c == 'P'
			p.pos++
			name, err := p.readUnicodeProp()
			if err != nil {
				return 0, "", err
			}
			tok := "p:" + name
			if neg {
				tok = "P:" + name
			}
			return 0, tok, nil
		}
		if c == 'x' {
			// reuse escape logic but backslash already consumed: temporarily
			// rewind one rune by inlining.
			p.pos--
			node, err := p.parseEscape(flags)
			if err != nil {
				return 0, "", err
			}
			if node.Kind == NChar {
				return node.Rune, "", nil
			}
			return 0, "", p.errf("unsupported escape inside class")
		}
		if v, known := simpleEscapes[c]; known {
			p.pos++
			return v, "", nil
		}
		if c == 'u' || c == 'U' || c == 'c' {
			p.pos--
			node, err := p.parseEscape(flags)
			if err != nil {
				return 0, "", err
			}
			if node.Kind == NChar {
				return node.Rune, "", nil
			}
		}
		p.pos++
		return c, "", nil
	}
	p.pos++
	return r, "", nil
}

var _ = unicode.MaxRune
