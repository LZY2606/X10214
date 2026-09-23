package engine

import (
	"strconv"
	"strings"
	"unicode"
)

func (p *parser) parseAtom() (Node, error) {
	begin := p.pos
	switch c := p.peek(); c {
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '.':
		p.pos++
		return &AnyChar{baseNode: baseNode{begin, p.pos}, DotAll: p.flags.DotAll}, nil
	case '^':
		p.pos++
		return &Anchor{baseNode: baseNode{begin, p.pos}, Kind: AnchorStartLine, Multiline: p.flags.Multiline}, nil
	case '$':
		p.pos++
		return &Anchor{baseNode: baseNode{begin, p.pos}, Kind: AnchorEndLine, Multiline: p.flags.Multiline}, nil
	case '*', '+', '?', '{':
		return nil, p.errf(begin, "quantifier with nothing to repeat")
	case ')':
		return nil, p.errf(begin, "unmatched ')'")
	case '\\':
		n, err := p.parseEscape(begin)
		if err != nil {
			return nil, err
		}
		return p.parseQuant(n)
	default:
		r, size, err := p.decodeSourceRune()
		if err != nil {
			return nil, err
		}
		_ = size
		return p.parseQuant(&Char{baseNode: baseNode{begin, p.pos}, R: r, CI: p.flags.CI})
	}
}

func (p *parser) decodeSourceRune() (rune, int, error) {
	r, size := decodeRune(p.src[p.pos:])
	if size <= 0 {
		return 0, 0, p.errf(p.pos, "invalid UTF-8 in pattern")
	}
	p.pos += size
	return r, size, nil
}

func (p *parser) parseQuant(n Node) (Node, error) {
	if p.pos >= len(p.src) {
		return n, nil
	}
	begin := p.pos
	var min, max int
	var unbounded, hasMax bool
	switch p.src[p.pos] {
	case '*':
		p.pos++
		min, max, unbounded = 0, 0, true
	case '+':
		p.pos++
		min, max, unbounded = 1, 0, true
	case '?':
		p.pos++
		min, max = 0, 1
	case '{':
		lo, hi, unb, ok, adv, err := p.parseBraceRep()
		if err != nil {
			return nil, err
		}
		if !ok {
			return n, nil // literal '{' per common behavior
		}
		p.pos += adv
		min, max, hasMax, unbounded = lo, hi, true, unb
	default:
		return n, nil
	}
	lazy := false
	if p.peek() == '?' {
		p.pos++
		lazy = true
	}
	if !unbounded {
		if !hasMax {
			max = min
		}
		if max < min {
			return nil, p.errf(begin, "repetition count is out of order")
		}
	}
	s, _ := n.Span()
	rp := &Repeat{baseNode: baseNode{s, p.pos}, Child: n, Min: min, Max: -1, Lazy: lazy}
	if !unbounded {
		rp.Max = max
	}
	return rp, nil
}

// parseBraceRep parses {n}, {n,}, {n,m} without consuming on failure.
func (p *parser) parseBraceRep() (min, max int, unbounded, ok bool, adv int, err error) {
	j := p.pos + 1
	if j >= len(p.src) || p.src[j] < '0' || p.src[j] > '9' {
		return 0, 0, false, false, 0, nil
	}
	start := j
	for j < len(p.src) && p.src[j] >= '0' && p.src[j] <= '9' {
		j++
	}
	n, perr := strconv.Atoi(p.src[start:j])
	if perr != nil {
		return 0, 0, false, false, 0, p.errf(start, "bad repetition")
	}
	if j >= len(p.src) {
		return 0, 0, false, false, 0, nil
	}
	if p.src[j] == '}' {
		return n, n, false, true, j + 1 - p.pos, nil
	}
	if p.src[j] != ',' {
		return 0, 0, false, false, 0, nil
	}
	j++
	if j < len(p.src) && p.src[j] == '}' {
		return n, 0, true, true, j + 1 - p.pos, nil
	}
	mstart := j
	for j < len(p.src) && p.src[j] >= '0' && p.src[j] <= '9' {
		j++
	}
	if j == mstart || j >= len(p.src) || p.src[j] != '}' {
		return 0, 0, false, false, 0, nil
	}
	m, perr := strconv.Atoi(p.src[mstart:j])
	if perr != nil {
		return 0, 0, false, false, 0, p.errf(mstart, "bad repetition")
	}
	return n, m, false, true, j + 1 - p.pos, nil
}

func (p *parser) parseGroup() (Node, error) {
	begin := p.pos // at '('
	p.pos++        // consume (
	capture := -1
	name := ""
	var flags *FlagState
	incoming := p.flags

	if p.peek() == '?' && p.pos+1 < len(p.src) {
		switch p.src[p.pos+1] {
		case ':':
			p.pos += 2
			flags = &incoming
		case '=':
			p.pos += 2
			return p.finishLookaround(begin, false, true)
		case '!':
			p.pos += 2
			return p.finishLookaround(begin, true, true)
		case '<':
			if p.pos+2 < len(p.src) {
				switch p.src[p.pos+2] {
				case '=':
					p.pos += 3
					return p.finishLookaround(begin, false, false)
				case '!':
					p.pos += 3
					return p.finishLookaround(begin, true, false)
				}
			}
			p.pos += 2
			nm, err := p.readGroupName('>')
			if err != nil {
				return nil, err
			}
			name = nm
			capture = p.allocGroup(name)
		case 'P':
			if p.pos+2 >= len(p.src) || p.src[p.pos+2] != '<' {
				return nil, p.errf(p.pos, "expected (?P<name>...)")
			}
			p.pos += 3
			nm, err := p.readGroupName('>')
			if err != nil {
				return nil, err
			}
			name = nm
			capture = p.allocGroup(name)
		default:
			on, off, colon, adv, err := p.readFlagDirective()
			if err != nil {
				return nil, err
			}
			nf := incoming.with(on, off)
			if colon {
				p.pos += adv
				flags = &nf
			} else {
				// bare directive: scope to the remainder of this group seq.
				p.pos += adv
				capture = 0
				flags = &nf
				if _, ok := p.names[""]; !ok && capture == 0 {
					// anonymous wrapper group, no slot
				}
			}
		}
	} else {
		capture = p.allocGroup("")
	}

	if flags != nil {
		p.flags = *flags
	}
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	p.flags = incoming
	if !p.eat(')') {
		return nil, p.errf(p.pos, "missing closing ')'")
	}
	if capture == 0 {
		g := &Group{baseNode: baseNode{begin, p.pos}, Index: 0, Child: body}
		return p.parseQuantNode(g)
	}
	g := &Group{baseNode: baseNode{begin, p.pos}, Index: capture, Name: name, Child: body}
	return p.parseQuantNode(g)
}

func (p *parser) parseQuantNode(n Node) (Node, error) {
	if q := p.peek(); q == '*' || q == '+' || q == '?' || q == '{' {
		return p.parseQuant(n)
	}
	return n, nil
}

func (p *parser) allocGroup(name string) int {
	p.group++
	idx := p.group
	p.groups = append(p.groups, GroupInfo{Index: idx, Name: name})
	if name != "" {
		if _, exists := p.names[name]; exists {
			// record will be rejected by the compiler with a clear message
			p.names[name] = -1
		} else {
			p.names[name] = idx
		}
	}
	return idx
}

func (p *parser) finishLookaround(begin int, negative, ahead bool) (Node, error) {
	incoming := p.flags
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	p.flags = incoming
	if !p.eat(')') {
		return nil, p.errf(p.pos, "missing closing ')' after lookaround")
	}
	return p.parseQuantNode(&Lookaround{
		baseNode: baseNode{begin, p.pos},
		Negative: negative,
		Behind:   !ahead,
		Child:    body,
	})
}

func (p *parser) readGroupName(close byte) (string, error) {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != close {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return "", p.errf(start, "unterminated group name")
	}
	name := p.src[start:p.pos]
	p.pos++ // consume close
	if name == "" {
		return "", p.errf(start, "empty group name")
	}
	return name, nil
}

func (p *parser) readFlagDirective() (on, off string, colon bool, adv int, err error) {
	// pos points at '?'; next char should be a flag or '-'
	j := p.pos + 1
	segStart := j
	for j < len(p.src) {
		c := p.src[j]
		if c == '-' {
			if j == segStart {
				j++
				continue
			}
		}
		if c == ')' || c == ':' {
			break
		}
		if c != 'i' && c != 'm' && c != 's' {
			return "", "", false, 0, p.errf(j, "unsupported flag %q (only i,m,s)", string(c))
		}
		j++
	}
	if j >= len(p.src) {
		return "", "", false, 0, p.errf(p.pos, "unterminated flag directive")
	}
	directive := p.src[p.pos+1 : j]
	colon = p.src[j] == ':'
	adv = j + 1 - p.pos
	if i := strings.IndexByte(directive, '-'); i >= 0 {
		on = directive[:i]
		off = directive[i+1:]
	} else {
		on = directive
	}
	return on, off, colon, adv, nil
}

var _ = unicode.IsLetter
