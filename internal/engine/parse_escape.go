package engine

import "strconv"

// parseEscape handles a backslash at p.pos-1 region; begin points at '\\'.
func (p *parser) parseEscape(begin int) (Node, error) {
	p.pos++ // consume backslash
	if p.pos >= len(p.src) {
		return nil, p.errf(begin, "trailing backslash")
	}
	c := p.src[p.pos]
	switch {
	case c == 'w', c == 'W', c == 'd', c == 'D', c == 's', c == 'S':
		p.pos++
		return &CharClass{
			baseNode: baseNode{begin, p.pos},
			Props:    []PropRef{{Name: "__" + string(c), Negated: c == 'W' || c == 'D' || c == 'S'}},
			CI:       p.flags.CI,
		}, nil
	case c == 'n', c == 't', c == 'r', c == 'f', c == 'v', c == 'a', c == 'e', c == '0':
		r := simpleEscRune(c)
		p.pos++
		return &Char{baseNode: baseNode{begin, p.pos}, R: r, CI: p.flags.CI}, nil
	case c == 'b', c == 'B':
		p.pos++
		kind := AnchorWord
		if c == 'B' {
			kind = AnchorNotWord
		}
		return &Anchor{baseNode: baseNode{begin, p.pos}, Kind: kind}, nil
	case c == 'A':
		p.pos++
		return &Anchor{baseNode: baseNode{begin, p.pos}, Kind: AnchorStartText}, nil
	case c == 'z':
		p.pos++
		return &Anchor{baseNode: baseNode{begin, p.pos}, Kind: AnchorEndText}, nil
	case c == 'Z':
		p.pos++
		return &Anchor{baseNode: baseNode{begin, p.pos}, Kind: AnchorEndTextNL}, nil
	case c == 'x':
		r, err := p.readHexEscape(2)
		if err != nil {
			return nil, err
		}
		return &Char{baseNode: baseNode{begin, p.pos}, R: r, CI: p.flags.CI}, nil
	case c == 'u':
		r, err := p.readHexEscape(4)
		if err != nil {
			return nil, err
		}
		return &Char{baseNode: baseNode{begin, p.pos}, R: r, CI: p.flags.CI}, nil
	case c == 'p', c == 'P':
		neg := c == 'P'
		p.pos++
		pr, err := p.readProp()
		if err != nil {
			return nil, err
		}
		pr.Negated = neg
		return &CharClass{baseNode: baseNode{begin, p.pos}, Props: []PropRef{*pr}, CI: p.flags.CI}, nil
	case c == 'k':
		p.pos++
		if p.peek() != '<' {
			return nil, p.errf(p.pos, "expected <name> after \\k")
		}
		p.pos++
		nm, err := p.readGroupName('>')
		if err != nil {
			return nil, err
		}
		idx, ok := p.names[nm]
		if !ok {
			return nil, p.errf(begin, "unknown named backreference %q", nm)
		}
		if idx < 0 {
			return nil, p.errf(begin, "duplicate group name %q", nm)
		}
		return &Ref{baseNode: baseNode{begin, p.pos}, Name: nm, Index: idx, CI: p.flags.CI}, nil
	case c == 'Q':
		p.pos++
		return p.readQuoted(begin)
	case c >= '1' && c <= '9':
		start := p.pos
		end := p.pos + 1
		for end < len(p.src) && end-start < 2 && p.src[end] >= '0' && p.src[end] <= '9' {
			end++
		}
		n, _ := strconv.Atoi(p.src[start:end])
		p.pos = end
		return &Ref{baseNode: baseNode{begin, p.pos}, Index: n, CI: p.flags.CI}, nil
	case c == 'R', c == 'X', c == 'g', c == 'G', c == 'y':
		return nil, p.errf(begin, "unsupported escape \\%c in CT-REGEX-1.0", c)
	default:
		r, _, err := p.decodeSourceRune()
		if err != nil {
			return nil, err
		}
		return &Char{baseNode: baseNode{begin, p.pos}, R: r, CI: p.flags.CI}, nil
	}
}

func simpleEscRune(c byte) rune {
	switch c {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	case 'f':
		return '\f'
	case 'v':
		return '\v'
	case 'a':
		return 0x07
	case 'e':
		return 0x1B
	case '0':
		return 0
	}
	return 0
}

func (p *parser) readHexEscape(fixed int) (rune, error) {
	at := p.pos
	p.pos++ // consume x/u
	n := fixed
	if p.peek() == '{' {
		p.pos++
		start := p.pos
		for p.pos < len(p.src) && isHex(p.src[p.pos]) {
			p.pos++
		}
		if p.pos == start || p.peek() != '}' {
			return 0, p.errf(at, "malformed hex escape")
		}
		v, err := strconv.ParseInt(p.src[start:p.pos], 16, 32)
		if err != nil || v > 0x10FFFF {
			return 0, p.errf(at, "hex escape out of range")
		}
		p.pos++
		return rune(v), nil
	}
	if p.pos+n > len(p.src) {
		return 0, p.errf(at, "malformed hex escape")
	}
	for i := 0; i < n; i++ {
		if !isHex(p.src[p.pos+i]) {
			return 0, p.errf(at, "malformed hex escape")
		}
	}
	v, err := strconv.ParseInt(p.src[p.pos:p.pos+n], 16, 32)
	if err != nil {
		return 0, p.errf(at, "malformed hex escape")
	}
	p.pos += n
	return rune(v), nil
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func (p *parser) readProp() (*PropRef, error) {
	at := p.pos - 1
	name := ""
	if p.peek() == '{' {
		p.pos++
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] != '}' {
			p.pos++
		}
		if p.pos >= len(p.src) {
			return nil, p.errf(at, "unterminated \\p property")
		}
		name = p.src[start:p.pos]
		p.pos++
	} else {
		r, _, err := p.decodeSourceRune()
		if err != nil {
			return nil, err
		}
		name = string(r)
	}
	if _, ok := lookupProperty(name); !ok {
		return nil, p.errf(at, "unknown Unicode property %q", name)
	}
	return &PropRef{Name: name}, nil
}

func (p *parser) readQuoted(begin int) (Node, error) {
	start := p.pos
	end := start
	for end < len(p.src) {
		if p.src[end] == '\\' && end+1 < len(p.src) && p.src[end+1] == 'E' {
			break
		}
		end++
	}
	if end >= len(p.src) {
		return nil, p.errf(begin, "unterminated \\Q...\\E")
	}
	lit := p.src[start:end]
	p.pos = end + 2
	var nodes []Node
	off := 0
	for off < len(lit) {
		r, size := decodeRune(lit[off:])
		nodes = append(nodes, &Char{
			baseNode: baseNode{begin + off, begin + off + size},
			R:        r,
			CI:       p.flags.CI,
		})
		off += size
	}
	return p.mkConcat(begin, p.pos, nodes), nil
}
