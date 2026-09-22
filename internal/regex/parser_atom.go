package regex

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

func (p *parser) parseAtom(flags Flags) (*Node, error) {
	r := p.eat()
	switch r {
	case '(':
		return p.parseGroup(flags)
	case '[':
		return p.parseClass(flags)
	case '.':
		return &Node{ID: p.newID(), Kind: NDot}, nil
	case '^':
		return &Node{ID: p.newID(), Kind: NAnchor, Anchor: "bol"}, nil
	case '$':
		return &Node{ID: p.newID(), Kind: NAnchor, Anchor: "eol"}, nil
	case '\\':
		return p.parseEscape(flags)
	case '*', '+', '?':
		return nil, p.errf("quantifier '%c' with nothing to repeat", r)
	default:
		n := &Node{ID: p.newID(), Kind: NChar, Value: r}
		if flags.I {
			// recorded at compile time via flags; marker kept on node
			n.CI = true
		}
		return n, nil
	}
}

func (p *parser) parseGroup(flags Flags) (*Node, error) {
	if r, ok := p.peek(); ok && r == '?' {
		p.eat()
		next, ok := p.peek()
		if !ok {
			return nil, p.errf("unterminated group opener")
		}
		switch next {
		case ':':
			p.eat()
		case '=':
			p.eat()
			return p.parseLook(flags, false, false)
		case '!':
			p.eat()
			return p.parseLook(flags, true, false)
		case '<':
			p.eat()
			c, ok := p.peek()
			if !ok {
				return nil, p.errf("unterminated group opener")
			}
			switch c {
			case '=':
				p.eat()
				return p.parseLook(flags, false, true)
			case '!':
				p.eat()
				return p.parseLook(flags, true, true)
			default:
				return p.parseNamedGroup(flags)
			}
		case 'P':
			p.eat()
			c2, ok2 := p.peek()
			if !ok2 || c2 != '<' {
				return nil, p.errf("expected (?P<name>...)")
			}
			p.eat()
			return p.parseNamedGroup(flags)
		case 'i', 'm', 's', 'u', '-':
			return p.parseFlagGroup(flags)
		default:
			return nil, p.errf("unsupported group extension '%c'", next)
		}
	}
	return p.finishCapture(flags, "", 0)
}

func (p *parser) parseNamedGroup(flags Flags) (*Node, error) {
	start := p.pos
	for {
		r, ok := p.peek()
		if !ok {
			return nil, p.errf("unterminated named group")
		}
		if r == '>' {
			break
		}
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return nil, p.errf("invalid character %q in group name", r)
		}
		p.eat()
	}
	name := string(p.src[start:p.pos])
	if name == "" {
		return nil, p.errf("empty group name")
	}
	if _, dup := p.groups[name]; dup {
		return nil, p.errf("duplicate group name %q", name)
	}
	p.eat() // '>'
	return p.finishCapture(flags, name, 0)
}

func (p *parser) finishCapture(flags Flags, name string, fixed int) (*Node, error) {
	idx := fixed
	if fixed == 0 {
		p.ng++
		idx = p.ng
	}
	if name != "" {
		p.groups[name] = idx
	}
	body, err := p.parseAlt(flags)
	if err != nil {
		return nil, err
	}
	if err := p.expectClose(); err != nil {
		return nil, err
	}
	return &Node{ID: p.newID(), Kind: NGroup, Capture: idx, Name: name, Body: body}, nil
}

func (p *parser) expectClose() error {
	r, ok := p.peek()
	if !ok {
		return p.errf("missing closing parenthesis")
	}
	if r != ')' {
		return p.errf("expected ')', got '%c'", r)
	}
	p.eat()
	return nil
}

func (p *parser) parseLook(flags Flags, negative, behind bool) (*Node, error) {
	body, err := p.parseAlt(flags)
	if err != nil {
		return nil, err
	}
	if err := p.expectClose(); err != nil {
		return nil, err
	}
	return &Node{ID: p.newID(), Kind: NLook, Negative: negative, Behind: behind, Body: body}, nil
}

func (p *parser) parseFlagGroup(flags Flags) (*Node, error) {
	// parse [imsux-imsux]* optionally followed by ':'
	var set, clear strings.Builder
	clearing := false
	scoped := false
	for {
		r, ok := p.peek()
		if !ok {
			return nil, p.errf("unterminated flag group")
		}
		if r == ')' {
			break
		}
		if r == ':' {
			scoped = true
			p.eat()
			break
		}
		if r == '-' {
			clearing = true
			p.eat()
			continue
		}
		nf, valid := flags.with(r, !clearing)
		if !valid {
			return nil, p.errf("invalid flag %q", r)
		}
		flags = nf
		if clearing {
			clear.WriteRune(r)
		} else {
			set.WriteRune(r)
		}
		p.eat()
	}
	if !scoped {
		if err := p.expectClose(); err != nil {
			return nil, err
		}
		body := &Node{ID: p.newID(), Kind: NEmpty}
		return &Node{ID: p.newID(), Kind: NFlagScope, SetFlags: set.String(), ClearFlags: clear.String(),
			Body: body}, nil
	}
	body, err := p.parseAlt(flags)
	if err != nil {
		return nil, err
	}
	if err := p.expectClose(); err != nil {
		return nil, err
	}
	return &Node{ID: p.newID(), Kind: NFlagScope, SetFlags: set.String(), ClearFlags: clear.String(), Body: body}, nil
}

func (p *parser) parseEscape(flags Flags) (*Node, error) {
	r, ok := p.peek()
	if !ok {
		return nil, p.errf("trailing backslash")
	}
	p.eat()
	switch r {
	case 'd', 'D', 'w', 'W', 's', 'S':
		neg := r >= 'a' && r <= 'z' && (r == 'D' || r == 'W' || r == 'S')
		_ = neg
		kind := map[rune]string{'d': "digit", 'D': "digit", 'w': "word", 'W': "word", 's': "space", 'S': "space"}[r]
		rr := RuneRange{Kind: kind, Neg: r == 'D' || r == 'W' || r == 'S'}
		return &Node{ID: p.newID(), Kind: NClass, Ranges: []RuneRange{rr}, CI: flags.I}, nil
	case 'p', 'P':
		name, err := p.readPropName()
		if err != nil {
			return nil, err
		}
		rr := RuneRange{Kind: "unicode:" + name, Neg: r == 'P'}
		return &Node{ID: p.newID(), Kind: NClass, Ranges: []RuneRange{rr}, CI: flags.I}, nil
	case 'b':
		return &Node{ID: p.newID(), Kind: NAnchor, Anchor: "wb"}, nil
	case 'B':
		return &Node{ID: p.newID(), Kind: NAnchor, Anchor: "nwb"}, nil
	case 'A':
		return &Node{ID: p.newID(), Kind: NAnchor, Anchor: "boa"}, nil
	case 'z':
		return &Node{ID: p.newID(), Kind: NAnchor, Anchor: "eoa"}, nil
	case 'a':
		return p.lit('\a', flags.I), nil
	case 'f':
		return p.lit('\f', flags.I), nil
	case 'n':
		return p.lit('\n', flags.I), nil
	case 'r':
		return p.lit('\r', flags.I), nil
	case 't':
		return p.lit('\t', flags.I), nil
	case 'v':
		return p.lit('\v', flags.I), nil
	case '0':
		return p.lit(rune(0), flags.I), nil
	case 'x':
		cp, err := p.readHexEscape(2)
		if err != nil {
			return nil, err
		}
		return p.lit(cp, flags.I), nil
	case 'u':
		cp, err := p.readHexEscape(4)
		if err != nil {
			return nil, err
		}
		return p.lit(cp, flags.I), nil
	case 'U':
		cp, err := p.readHexEscape(8)
		if err != nil {
			return nil, err
		}
		if !utf8.ValidRune(cp) {
			return nil, p.errf("invalid code point U+%04X", cp)
		}
		return p.lit(cp, flags.I), nil
	case 'k':
		name, err := p.readBackrefName()
		if err != nil {
			return nil, err
		}
		idx, ok := p.groups[name]
		if !ok {
			return nil, p.errf("backreference to undefined named group %q", name)
		}
		return &Node{ID: p.newID(), Kind: NBackref, RefIndex: idx}, nil
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		p.pos--
		n, _, err := p.readDecimal()
		if err != nil {
			return nil, err
		}
		if n > p.ng {
			return nil, p.errf("backreference to undefined group %d", n)
		}
		bn := &Node{ID: p.newID(), Kind: NBackref, RefIndex: n}
		_ = bn
		return &Node{ID: p.newID(), Kind: NBackref, RefIndex: n, CI: flags.I}, nil
	default:
		// Escaped punctuation (including \(, \), \., \\, \/, \-) is literal.
		return p.lit(r, flags.I), nil
	}
}

func (p *parser) lit(r rune, ci bool) *Node {
	return &Node{ID: p.newID(), Kind: NChar, Value: r, CI: ci}
}

func (p *parser) readHexEscape(n int) (rune, error) {
	var v int64
	for i := 0; i < n; i++ {
		r, ok := p.peek()
		if !ok || !isHex(r) {
			return 0, p.errf("bad hex escape")
		}
		p.eat()
		v = v*16 + int64(hexVal(r))
	}
	return rune(v), nil
}

func isHex(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
}

func hexVal(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0')
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10
	default:
		return int(r-'A') + 10
	}
}

func (p *parser) readPropName() (string, error) {
	r, ok := p.peek()
	if !ok {
		return "", p.errf("missing property name")
	}
	if r == '{' {
		p.eat()
		start := p.pos
		for {
			c, ok := p.peek()
			if !ok {
				return "", p.errf("unterminated property name")
			}
			if c == '}' {
				break
			}
			p.eat()
		}
		name := strings.TrimSpace(string(p.src[start:p.pos]))
		p.eat()
		if name == "" {
			return "", p.errf("empty property name")
		}
		return normalizeProp(name), nil
	}
	p.eat()
	return normalizeProp(string(r)), nil
}

func normalizeProp(s string) string {
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, " ", "")
	return strings.ToLower(s)
}

func (p *parser) readBackrefName() (string, error) {
	r, ok := p.peek()
	if !ok || r != '<' {
		return "", p.errf("expected <name> after \\k")
	}
	p.eat()
	start := p.pos
	for {
		c, ok := p.peek()
		if !ok {
			return "", p.errf("unterminated backreference name")
		}
		if c == '>' {
			break
		}
		p.eat()
	}
	name := string(p.src[start:p.pos])
	p.eat()
	if name == "" {
		return "", p.errf("empty backreference name")
	}
	if n, err := strconv.Atoi(name); err == nil {
		if n <= 0 || n > p.ng {
			return "", p.errf("backreference to undefined group %d", n)
		}
	}
	return name, nil
}
