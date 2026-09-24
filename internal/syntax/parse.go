package syntax

import (
	"fmt"
	"strconv"
	"unicode"
)

type parser struct {
	src    []rune
	pos    int
	groups int
	names  map[string]int
}

// Parse parses a pattern into an AST plus group metadata.
func Parse(pattern string) (*Node, *Info, error) {
	p := &parser{src: []rune(pattern), names: map[string]int{}}
	root, err := p.parseAlt()
	if err != nil {
		return nil, nil, err
	}
	if p.pos != len(p.src) {
		return nil, nil, fmt.Errorf("syntax: unexpected %q at offset %d", string(p.src[p.pos]), p.pos)
	}
	return root, &Info{Groups: p.groups, Names: p.names}, nil
}

func (p *parser) peek() rune {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) peekAt(d int) rune {
	if p.pos+d >= len(p.src) {
		return 0
	}
	return p.src[p.pos+d]
}

func (p *parser) parseAlt() (*Node, error) {
	var branches []*Node
	for {
		c, err := p.parseConcat()
		if err != nil {
			return nil, err
		}
		branches = append(branches, c)
		if p.peek() == '|' {
			p.pos++
			continue
		}
		break
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &Node{Kind: Alternate, Children: branches}, nil
}

func (p *parser) parseConcat() (*Node, error) {
	var items []*Node
	for p.pos < len(p.src) && p.peek() != '|' && p.peek() != ')' {
		n, err := p.parseRepeat()
		if err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	switch len(items) {
	case 0:
		return &Node{Kind: Empty}, nil
	case 1:
		return items[0], nil
	}
	return &Node{Kind: Concat, Children: items}, nil
}

func (p *parser) parseRepeat() (*Node, error) {
	atom, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	var min, max int
	applied := true
	switch p.peek() {
	case '*':
		min, max = 0, -1
		p.pos++
	case '+':
		min, max = 1, -1
		p.pos++
	case '?':
		min, max = 0, 1
		p.pos++
	case '{':
		mn, mx, ok, err := p.parseBounds()
		if err != nil {
			return nil, err
		}
		if !ok {
			return atom, nil // literal '{'
		}
		min, max = mn, mx
	default:
		applied = false
	}
	if !applied {
		return atom, nil
	}
	lazy := false
	if p.peek() == '?' {
		lazy = true
		p.pos++
	}
	switch p.peek() {
	case '*', '+', '?':
		return nil, fmt.Errorf("syntax: nested quantifier at offset %d", p.pos)
	case '{':
		if _, _, ok, err := p.parseBounds(); err != nil {
			return nil, err
		} else if ok {
			return nil, fmt.Errorf("syntax: nested quantifier at offset %d", p.pos)
		}
	}
	return &Node{Kind: Repeat, Children: []*Node{atom}, Min: min, Max: max, Lazy: lazy}, nil
}

func (p *parser) parseBounds() (int, int, bool, error) {
	save := p.pos
	p.pos++ // consume '{'
	readNum := func() (int, bool) {
		start := p.pos
		for p.pos < len(p.src) && unicode.IsDigit(p.src[p.pos]) {
			p.pos++
		}
		if p.pos == start {
			return 0, false
		}
		n, err := strconv.Atoi(string(p.src[start:p.pos]))
		if err != nil {
			return 0, false
		}
		return n, true
	}
	min, ok := readNum()
	if !ok {
		p.pos = save
		return 0, 0, false, nil
	}
	max := min
	switch p.peek() {
	case '}':
		p.pos++
	case ',':
		p.pos++
		if p.peek() == '}' {
			max = -1
			p.pos++
		} else {
			m, ok := readNum()
			if !ok || p.peek() != '}' {
				p.pos = save
				return 0, 0, false, nil
			}
			p.pos++
			max = m
		}
	default:
		p.pos = save
		return 0, 0, false, nil
	}
	if max >= 0 && max < min {
		return 0, 0, false, fmt.Errorf("syntax: repeat bounds {%d,%d} out of order", min, max)
	}
	if min > MaxRepeat || max > MaxRepeat {
		return 0, 0, false, fmt.Errorf("syntax: repeat bound exceeds %d", MaxRepeat)
	}
	return min, max, true, nil
}

func (p *parser) parseAtom() (*Node, error) {
	switch ch := p.peek(); ch {
	case 0:
		return nil, fmt.Errorf("syntax: unexpected end of pattern")
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '.':
		p.pos++
		return &Node{Kind: AnyChar}, nil
	case '^':
		p.pos++
		return &Node{Kind: Anchor, Anchor: AnchorStart}, nil
	case '$':
		p.pos++
		return &Node{Kind: Anchor, Anchor: AnchorEnd}, nil
	case '\\':
		return p.parseEscape()
	case '*', '+', '?':
		return nil, fmt.Errorf("syntax: quantifier without target at offset %d", p.pos)
	case ')':
		return nil, fmt.Errorf("syntax: unmatched ')' at offset %d", p.pos)
	default:
		p.pos++
		return &Node{Kind: Literal, Lit: string(ch)}, nil
	}
}

func (p *parser) expectClose() error {
	if p.peek() != ')' {
		return fmt.Errorf("syntax: missing ')' at offset %d", p.pos)
	}
	p.pos++
	return nil
}

func (p *parser) parseGroup() (*Node, error) {
	p.pos++ // consume '('
	if p.peek() == '?' {
		p.pos++
		switch p.peek() {
		case ':':
			p.pos++
			body, err := p.parseAlt()
			if err != nil {
				return nil, err
			}
			return body, p.expectClose()
		case '=':
			p.pos++
			return p.parseLook(LookAhead)
		case '!':
			p.pos++
			return p.parseLook(LookAheadNeg)
		case '<':
			p.pos++
			switch p.peek() {
			case '=':
				p.pos++
				return p.parseLook(LookBehind)
			case '!':
				p.pos++
				return p.parseLook(LookBehindNeg)
			}
			return p.parseNamedGroup()
		case 'P':
			p.pos++
			if p.peek() != '<' {
				return nil, fmt.Errorf("syntax: expected '<' after (?P at offset %d", p.pos)
			}
			p.pos++
			return p.parseNamedGroup()
		}
		return nil, fmt.Errorf("syntax: unknown group extension at offset %d", p.pos)
	}
	p.groups++
	idx := p.groups
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if err := p.expectClose(); err != nil {
		return nil, err
	}
	return &Node{Kind: Group, Index: idx, Children: []*Node{body}}, nil
}

func (p *parser) parseNamedGroup() (*Node, error) {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != '>' {
		p.pos++
	}
	if p.pos >= len(p.src) || p.pos == start {
		return nil, fmt.Errorf("syntax: bad group name at offset %d", start)
	}
	name := string(p.src[start:p.pos])
	p.pos++ // consume '>'
	if _, dup := p.names[name]; dup {
		return nil, fmt.Errorf("syntax: duplicate group name %q", name)
	}
	p.groups++
	idx := p.groups
	p.names[name] = idx
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if err := p.expectClose(); err != nil {
		return nil, err
	}
	return &Node{Kind: Group, Index: idx, Name: name, Children: []*Node{body}}, nil
}

func (p *parser) parseLook(kind LookKind) (*Node, error) {
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if err := p.expectClose(); err != nil {
		return nil, err
	}
	return &Node{Kind: Lookaround, Look: kind, Children: []*Node{body}}, nil
}

func (p *parser) parseClass() (*Node, error) {
	p.pos++ // consume '['
	n := &Node{Kind: CharClass}
	if p.peek() == '^' {
		n.Negate = true
		p.pos++
	}
	first := true
	for {
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("syntax: unterminated character class")
		}
		if p.peek() == ']' && !first {
			p.pos++
			return n, nil
		}
		first = false
		lo, isRune, err := p.classItem(n)
		if err != nil {
			return nil, err
		}
		if !isRune {
			continue // predicate/category already recorded
		}
		if p.peek() == '-' && p.peekAt(1) != ']' && p.peekAt(1) != 0 {
			p.pos++
			hi, isRune, err := p.classItem(n)
			if err != nil {
				return nil, err
			}
			if !isRune {
				return nil, fmt.Errorf("syntax: bad range end in class")
			}
			if hi < lo {
				return nil, fmt.Errorf("syntax: invalid range %q-%q", string(lo), string(hi))
			}
			n.Ranges = append(n.Ranges, Range{Lo: lo, Hi: hi})
		} else {
			n.Ranges = append(n.Ranges, Range{Lo: lo, Hi: lo})
		}
	}
}

// classItem parses one class item. It returns a rune when the item is a
// single char; predicates/categories are recorded directly on n.
func (p *parser) classItem(n *Node) (rune, bool, error) {
	if p.peek() != '\\' {
		ch := p.src[p.pos]
		p.pos++
		return ch, true, nil
	}
	p.pos++
	if p.pos >= len(p.src) {
		return 0, false, fmt.Errorf("syntax: trailing backslash in class")
	}
	c := p.src[p.pos]
	p.pos++
	switch c {
	case 'd':
		n.Preds = append(n.Preds, "digit")
	case 'D':
		n.Preds = append(n.Preds, "!digit")
	case 'w':
		n.Preds = append(n.Preds, "word")
	case 'W':
		n.Preds = append(n.Preds, "!word")
	case 's':
		n.Preds = append(n.Preds, "space")
	case 'S':
		n.Preds = append(n.Preds, "!space")
	case 'p', 'P':
		name, err := p.parseCatName()
		if err != nil {
			return 0, false, err
		}
		if c == 'P' {
			name = "!" + name
		}
		n.Cats = append(n.Cats, name)
	case 'n':
		return '\n', true, nil
	case 't':
		return '\t', true, nil
	case 'r':
		return '\r', true, nil
	case 'f':
		return '\f', true, nil
	case 'v':
		return '\v', true, nil
	case 'x':
		r, err := p.parseHex(2)
		return r, true, err
	case 'u':
		r, err := p.parseHex(4)
		return r, true, err
	default:
		return c, true, nil
	}
	return 0, false, nil
}

func (p *parser) parseCatName() (string, error) {
	if p.peek() == '{' {
		p.pos++
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] != '}' {
			p.pos++
		}
		if p.pos >= len(p.src) {
			return "", fmt.Errorf("syntax: unterminated \\p{...}")
		}
		name := string(p.src[start:p.pos])
		p.pos++
		return name, nil
	}
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("syntax: bad \\p at end of pattern")
	}
	name := string(p.src[p.pos])
	p.pos++
	return name, nil
}

func (p *parser) parseHex(n int) (rune, error) {
	if p.pos+n > len(p.src) {
		return 0, fmt.Errorf("syntax: short hex escape at offset %d", p.pos)
	}
	v, err := strconv.ParseInt(string(p.src[p.pos:p.pos+n]), 16, 32)
	if err != nil {
		return 0, fmt.Errorf("syntax: bad hex escape at offset %d", p.pos)
	}
	p.pos += n
	return rune(v), nil
}

func (p *parser) parseEscape() (*Node, error) {
	p.pos++ // consume backslash
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("syntax: trailing backslash")
	}
	c := p.src[p.pos]
	p.pos++
	switch c {
	case 'd':
		return &Node{Kind: CharClass, Preds: []string{"digit"}}, nil
	case 'D':
		return &Node{Kind: CharClass, Preds: []string{"!digit"}}, nil
	case 'w':
		return &Node{Kind: CharClass, Preds: []string{"word"}}, nil
	case 'W':
		return &Node{Kind: CharClass, Preds: []string{"!word"}}, nil
	case 's':
		return &Node{Kind: CharClass, Preds: []string{"space"}}, nil
	case 'S':
		return &Node{Kind: CharClass, Preds: []string{"!space"}}, nil
	case 'p', 'P':
		name, err := p.parseCatName()
		if err != nil {
			return nil, err
		}
		if c == 'P' {
			name = "!" + name
		}
		return &Node{Kind: CharClass, Cats: []string{name}}, nil
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return &Node{Kind: Backref, Index: int(c - '0')}, nil
	case 'k':
		if p.peek() != '<' {
			return nil, fmt.Errorf("syntax: expected \\k<name> at offset %d", p.pos)
		}
		p.pos++
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] != '>' {
			p.pos++
		}
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("syntax: unterminated \\k<name>")
		}
		name := string(p.src[start:p.pos])
		p.pos++
		return &Node{Kind: Backref, Name: name}, nil
	case 'A':
		return &Node{Kind: Anchor, Anchor: AnchorTextStart}, nil
	case 'z':
		return &Node{Kind: Anchor, Anchor: AnchorTextEnd}, nil
	case 'b':
		return &Node{Kind: Anchor, Anchor: AnchorWordBoundary}, nil
	case 'B':
		return &Node{Kind: Anchor, Anchor: AnchorNotWordBoundary}, nil
	case 'n':
		return &Node{Kind: Literal, Lit: "\n"}, nil
	case 't':
		return &Node{Kind: Literal, Lit: "\t"}, nil
	case 'r':
		return &Node{Kind: Literal, Lit: "\r"}, nil
	case 'f':
		return &Node{Kind: Literal, Lit: "\f"}, nil
	case 'v':
		return &Node{Kind: Literal, Lit: "\v"}, nil
	case 'x':
		r, err := p.parseHex(2)
		if err != nil {
			return nil, err
		}
		return &Node{Kind: Literal, Lit: string(r)}, nil
	case 'u':
		r, err := p.parseHex(4)
		if err != nil {
			return nil, err
		}
		return &Node{Kind: Literal, Lit: string(r)}, nil
	default:
		return &Node{Kind: Literal, Lit: string(c)}, nil
	}
}
