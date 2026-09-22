package syntax

import (
	"strings"
	"unicode"
)

func (p *parser) parseAtom(flags FlagSet) (*Node, error) {
	r, _ := p.peek()
	switch r {
	case '(':
		return p.parseGroup(flags)
	case '[':
		return p.parseClass(flags)
	case '.':
		p.pos++
		n := p.newNode(NAny)
		n.Src = "."
		return n, nil
	case '^':
		p.pos++
		n := p.newNode(NAnchor)
		n.Anchor = "bol"
		n.Src = "^"
		return n, nil
	case '$':
		p.pos++
		n := p.newNode(NAnchor)
		n.Anchor = "eol"
		n.Src = "$"
		return n, nil
	case '\\':
		return p.parseEscape(flags)
	case '*', '+', '?':
		return nil, p.errf("quantifier %q with nothing to repeat", string(r))
	case '{':
		if _, _, ok := p.scanBrace(); ok {
			return nil, p.errf("brace quantifier with nothing to repeat")
		}
	}
	p.pos++
	n := p.newNode(NChar)
	n.Rune = r
	n.Src = string(r)
	return n, nil
}

func (p *parser) scanBrace() (min, max int, ok bool) {
	j := p.pos
	if j >= len(p.runes) || p.runes[j] != '{' {
		return 0, 0, false
	}
	k := j + 1
	start := k
	for k < len(p.runes) && p.runes[k] >= '0' && p.runes[k] <= '9' {
		k++
	}
	if k == start {
		return 0, 0, false
	}
	min = atoiRunes(p.runes[start:k])
	max = min
	if k < len(p.runes) && p.runes[k] == '}' {
		return min, max, true
	}
	if k >= len(p.runes) || p.runes[k] != ',' {
		return 0, 0, false
	}
	k++
	numStart := k
	max = -1
	for k < len(p.runes) && p.runes[k] >= '0' && p.runes[k] <= '9' {
		k++
	}
	if k > numStart {
		max = atoiRunes(p.runes[numStart:k])
	}
	if k >= len(p.runes) || p.runes[k] != '}' {
		return 0, 0, false
	}
	_ = k
	return min, max, true
}

func atoiRunes(rs []rune) int {
	v := 0
	for _, r := range rs {
		v = v*10 + int(r-'0')
	}
	return v
}

func (p *parser) parseQuantifier(atom *Node, flags FlagSet) (*Node, error) {
	r, ok := p.peek()
	if !ok {
		return atom, nil
	}
	var min, max int
	has := true
	switch r {
	case '*':
		min, max = 0, -1
	case '+':
		min, max = 1, -1
	case '?':
		min, max = 0, 1
	case '{':
		mn, mx, bok := p.scanBrace()
		if !bok {
			return atom, nil
		}
		min, max = mn, mx
	default:
		has = false
	}
	if !has {
		return atom, nil
	}
	if max >= 0 && max < min {
		return nil, p.errf("quantifier min %d exceeds max %d", min, max)
	}
	switch r {
	case '*':
		p.pos++
	case '+':
		p.pos++
	case '?':
		p.pos++
	case '{':
		// advance to after '}'
		for p.pos < len(p.runes) && p.runes[p.pos] != '}' {
			p.pos++
		}
		p.pos++
	}
	if c, cok := p.peek(); cok && c == '+' {
		return nil, p.errf("possessive quantifiers are not supported by this grammar version")
	}
	greedy := true
	if p.eat('?') {
		greedy = false
	}
	if atom.Kind == NRepeat {
		return nil, p.errf("nested quantifier on a repeated node is not allowed; wrap in a group")
	}
	n := p.newNode(NRepeat)
	n.Min, n.Max, n.Greedy = min, max, greedy
	n.Child = atom
	return n, nil
}

func (p *parser) parseGroup(flags FlagSet) (*Node, error) {
	p.pos++ // consume '('
	if p.eat('?') {
		r, ok := p.peek()
		if !ok {
			return nil, p.errf("unterminated group option")
		}
		switch r {
		case ':':
			p.pos++
			return p.finishGroup(false, 0, "", flags, flags, nil)
		case '=':
			p.pos++
			return p.finishLook(false, false, flags)
		case '!':
			p.pos++
			return p.finishLook(true, false, flags)
		case '<':
			p.pos++
			c, cok := p.peek()
			if !cok {
				return nil, p.errf("unterminated group")
			}
			switch c {
			case '=':
				p.pos++
				return p.finishLook(false, true, flags)
			case '!':
				p.pos++
				return p.finishLook(true, true, flags)
			default:
				name, err := p.readGroupName()
				if err != nil {
					return nil, err
				}
				return p.finishNamedGroup(name, flags)
			}
		case '>', '-':
			return nil, p.errf("atomic groups and possessive constructs are not supported")
		default:
			return p.parseFlagGroup(flags)
		}
	}
	idx := len(p.groups)
	p.groups = append(p.groups, Group{Index: idx})
	return p.finishGroup(true, idx, "", flags, flags, nil)
}

func (p *parser) readGroupName() (string, error) {
	var sb strings.Builder
	for {
		r, ok := p.peek()
		if !ok {
			return "", p.errf("unterminated named group")
		}
		if r == '>' {
			p.pos++
			name := sb.String()
			if name == "" {
				return "", p.errf("empty group name")
			}
			return name, nil
		}
		if r == '(' || r == ')' || r == '|' {
			return "", p.errf("illegal character %q in group name", string(r))
		}
		sb.WriteRune(r)
		p.pos++
	}
}

func (p *parser) finishNamedGroup(name string, flags FlagSet) (*Node, error) {
	for _, g := range p.groups {
		if g.Name == name {
			return nil, p.errf("duplicate group name %q", name)
		}
	}
	idx := len(p.groups)
	p.groups = append(p.groups, Group{Index: idx, Name: name})
	return p.finishGroup(true, idx, name, flags, flags, nil)
}

func (p *parser) finishLook(negative, behind bool, flags FlagSet) (*Node, error) {
	body, err := p.parseAlternation(flags)
	if err != nil {
		return nil, err
	}
	if !p.eat(')') {
		return nil, p.errf("unterminated lookaround group")
	}
	if body == nil {
		body = p.newNode(NEmpty)
	}
	n := p.newNode(NLook)
	n.Negative = negative
	n.Behind = behind
	n.Children = []*Node{body}
	return n, nil
}

func (p *parser) finishGroup(capture bool, idx int, name string, outer, inner FlagSet, flagNode *Node) (*Node, error) {
	body, err := p.parseAlternation(inner)
	if err != nil {
		return nil, err
	}
	if !p.eat(')') {
		return nil, p.errf("unterminated group")
	}
	if body == nil {
		body = p.newNode(NEmpty)
	}
	if flagNode != nil {
		c := p.newNode(NConcat)
		c.Children = []*Node{flagNode, body}
		body = c
	}
	n := p.newNode(NGroup)
	n.Capture = capture
	n.Index = idx
	n.Name = name
	n.Children = []*Node{body}
	return n, nil
}

var _ = unicode.IsDigit
