package syntax

// parseGroup: '(' at startAtom already consumed.
func (p *parser) parseGroup(startAtom int) (Node, error) {
	so := p.off[startAtom]
	g := &Group{Flags: p.flags}
	newFlags := p.flags
	hasDirective := false
	local := true

	a, ok := p.peek()
	if !ok || a.bad {
		return nil, p.errf(so, "unterminated group")
	}
	if a.r == '?' {
		p.eat()
		b, ok := p.peek()
		if !ok || b.bad {
			return nil, p.errf(so, "unterminated group")
		}
		switch {
		case b.r == ':':
			p.eat()
		case b.r == '=':
			p.eat()
			g.Look, g.Assertion = '=', true
		case b.r == '!':
			p.eat()
			g.Look, g.Assertion = '!', true
		case b.r == '<':
			p.eat()
			c, ok := p.peek()
			if !ok || c.bad {
				return nil, p.errf(so, "unterminated group")
			}
			switch c.r {
			case '=':
				p.eat()
				g.Look, g.Assertion = '<', true
			case '!':
				p.eat()
				g.Look, g.Assertion = '>', true
			default:
				name, err := p.parseName('>')
				if err != nil {
					return nil, err
				}
				p.caps++
				g.Index = p.caps
				g.Name = name
				p.names[name] = g.Index
			}
		case b.r == '>':
			p.eat()
			g.Atomic = true
		case b.r == 'i' || b.r == 'm' || b.r == 's' || b.r == '-':
			f, body, isLocal, err := p.parseFlagGroup()
			if err != nil {
				return nil, err
			}
			newFlags, hasDirective, local = f, true, isLocal
			if body == '=' || body == '!' {
				g.Look, g.Assertion = body, true
			}
		default:
			return nil, p.errf(p.curOff(), "unsupported group construct (?%c", b.r)
		}
	} else {
		p.caps++
		g.Index = p.caps
	}

	outer := p.flags
	p.flags = newFlags
	body, err := p.parseRe()
	p.flags = outer
	if err != nil {
		return nil, err
	}
	c, ok := p.peek()
	if !ok || c.bad || c.r != ')' {
		return nil, p.errf(so, "unterminated group")
	}
	p.eat()
	g.Body = body
	g.baseNode = baseNode{off: so, end: p.curOff()}
	if hasDirective && !local {
		// Bare directive (?i) affects the remainder of the enclosing scope.
		p.flags = newFlags
	}
	return p.maybeQuant(g)
}

func (p *parser) parseName(term rune) (string, error) {
	var name []rune
	startOff := p.curOff()
	for {
		a, ok := p.peek()
		if !ok || a.bad {
			return "", p.errf(startOff, "unterminated name")
		}
		if a.r == term {
			p.eat()
			break
		}
		if a.r == '(' || a.r == ')' || a.r == '<' || a.r == '>' || a.r == ' ' {
			return "", p.errf(p.curOff(), "invalid character in group name")
		}
		name = append(name, a.r)
		p.eat()
	}
	if len(name) == 0 {
		return "", p.errf(startOff, "empty group name")
	}
	return string(name), nil
}

// parseFlagGroup entered after '(?'; returns flags, optional body marker
// (':' is normalized to 0), and whether the scope is local.
func (p *parser) parseFlagGroup() (Flags, byte, bool, error) {
	f := p.flags
	on := true
	for {
		a, ok := p.peek()
		if !ok || a.bad {
			return f, 0, true, p.errf(p.curOff(), "unterminated flags")
		}
		switch a.r {
		case 'i', 'm', 's':
			p.eat()
			switch a.r {
			case 'i':
				f.I = on
			case 'm':
				f.M = on
			case 's':
				f.S = on
			}
		case '-':
			p.eat()
			on = false
		case ':':
			p.eat()
			return f, 0, true, nil
		case '=', '!':
			marker := a.r
			p.eat()
			return f, marker, true, nil
		case ')':
			p.eat()
			return f, 0, false, nil
		default:
			return f, 0, true, p.errf(p.curOff(), "unsupported flag %q", string(a.r))
		}
	}
}
