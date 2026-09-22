package syntax

import "strings"

// parseFlagGroup handles (?ims-ims:body) and bare (?ims-ims) switches.
func (p *parser) parseFlagGroup(outer FlagSet) (*Node, error) {
	set, clear, scoped, err := p.readFlagLetters()
	if err != nil {
		return nil, err
	}
	inner := applyFlags(outer, set, clear)
	if !scoped {
		if !p.eat(')') {
			return nil, p.errf("malformed flag group")
		}
		fn := p.newNode(NFlags)
		fn.SetFlags, fn.ClearFlags, fn.Src = set, clear, "(?"+flagText(set, clear)+")"
		// A bare flag switch takes effect for the remainder of the enclosing
		// sequence: keep parsing under the new flag set and glue the marker on.
		rest, err := p.parseSequence(inner)
		if err != nil {
			return nil, err
		}
		if rest == nil {
			return fn, nil
		}
		c := p.newNode(NConcat)
		c.Children = []*Node{fn, rest}
		return c, nil
	}
	// (?flags:body) is a non-capturing group with scoped flags.
	body, err := p.parseAlternation(inner)
	if err != nil {
		return nil, err
	}
	if !p.eat(')') {
		return nil, p.errf("unterminated flag group")
	}
	if body == nil {
		body = p.newNode(NEmpty)
	}
	n := p.newNode(NGroup)
	n.Capture = false
	n.Children = []*Node{body}
	return n, nil
}

func (p *parser) readFlagLetters() (set, clear string, scoped bool, err error) {
	var sb strings.Builder
	phase := 0 // 0: setting, 1: clearing
	for {
		r, ok := p.peek()
		if !ok {
			return "", "", false, p.errf("unterminated flag group")
		}
		if r == ':' {
			p.pos++
			return set, clear, true, nil
		}
		if r == ')' {
			return set, clear, false, nil
		}
		if r == '-' {
			if phase == 1 {
				return "", "", false, p.errf("repeated '-' in flag group")
			}
			phase = 1
			set = sb.String()
			sb.Reset()
			p.pos++
			continue
		}
		if r != 'i' && r != 'm' && r != 's' && r != 'u' {
			return "", "", false, p.errf("unknown flag %q", string(r))
		}
		sb.WriteRune(r)
		p.pos++
	}
}

func flagText(set, clear string) string {
	if clear == "" {
		return set
	}
	return set + "-" + clear
}

func applyFlags(f FlagSet, set, clear string) FlagSet {
	toggle := func(s string, on bool) {
		for _, r := range s {
			switch r {
			case 'i':
				f.I = on
			case 'm':
				f.M = on
			case 's':
				f.S = on
			case 'u':
				f.U = on
			}
		}
	}
	toggle(set, true)
	toggle(clear, false)
	return f
}
