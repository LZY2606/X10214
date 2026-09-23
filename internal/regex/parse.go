package regex

import (
	"fmt"
	"strconv"
	"unicode/utf8"
)

// ParseError describes a syntax failure with a byte offset.
type ParseError struct {
	Offset int
	Msg    string
}

func (e *ParseError) Error() string { return fmt.Sprintf("parse error at %d: %s", e.Offset, e.Msg) }

type parser struct {
	src   string
	pos   int
	flags Flags
	names map[string]int
	count int
	dupName string
}

// Parse builds the AST, assigns capture indices and reports semantic
// versions. It never consults the host regexp library.
func Parse(pattern string, flags Flags) (Node, *GroupInfo, error) {
	p := &parser{src: pattern, flags: flags, names: map[string]int{}}
	node, err := p.parseAlternation(0)
	if err != nil {
		return nil, nil, err
	}
	if p.pos != len(p.src) {
		return nil, nil, &ParseError{Offset: p.pos, Msg: "unexpected trailing input"}
	}
	if p.dupName != "" {
		return nil, nil, &ParseError{Offset: 0, Msg: "duplicate group name " + p.dupName}
	}
	info := &GroupInfo{Count: p.count, Names: p.names, Pattern: pattern, Flags: flags}
	if err := resolveRefs(node, info); err != nil {
		return nil, nil, err
	}
	return node, info, nil
}

// GroupInfo records capture layout after parsing.
type GroupInfo struct {
	Count   int
	Names   map[string]int
	Pattern string
	Flags   Flags
}

func (p *parser) parseAlternation(depth int) (Node, error) {
	start := p.pos
	var branches []Node
	first, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	branches = append(branches, first)
	for p.peek() == '|' {
		p.pos++
		b, err := p.parseConcat()
		if err != nil {
			return nil, err
		}
		branches = append(branches, b)
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &Alt{baseNode: baseNode{start}, Branches: branches}, nil
}

func (p *parser) parseConcat() (Node, error) {
	start := p.pos
	var items []Node
	for {
		c := p.peek()
		if c == -1 || c == '|' || c == ')' {
			break
		}
		n, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		n, err = p.parseQuantifier(n)
		if err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	if len(items) == 0 {
		return &Concat{baseNode: baseNode{start}}, nil
	}
	if len(items) == 1 {
		return items[0], nil
	}
	return &Concat{baseNode: baseNode{start}, Items: items}, nil
}

func (p *parser) parseQuantifier(n Node) (Node, error) {
	c := p.peek()
	if c != '*' && c != '+' && c != '?' && c != '{' {
		return n, nil
	}
	pos := p.pos
	min, max, has, err := p.peekBrace()
	if err != nil {
		return nil, err
	}
	if c == '{' && !has {
		return n, nil // literal '{'
	}
	lazy := false
	if p.peek() == '?' {
		lazy = true
		p.pos++
	}
	return &Quant{baseNode: baseNode{pos}, Child: n, Min: min, Max: max, Lazy: lazy}, nil
}

func (p *parser) peekBrace() (min, max int, has bool, err error) {
	switch p.peek() {
	case '*':
		p.pos++
		return 0, -1, true, nil
	case '+':
		p.pos++
		return 1, -1, true, nil
	case '?':
		p.pos++
		return 0, 1, true, nil
	case '{':
		save := p.pos
		p.pos++
		a := p.pos
		for isDigit(p.peek()) {
			p.pos++
		}
		if p.pos == a {
			p.pos = save
			return 0, 0, false, nil
		}
		mn, _ := strconv.Atoi(p.src[a:p.pos])
		mx := mn
		if p.peek() == ',' {
			p.pos++
			b := p.pos
			for isDigit(p.peek()) {
				p.pos++
			}
			if p.pos == b {
				mx = -1
			} else {
				mx, _ = strconv.Atoi(p.src[b:p.pos])
			}
		}
		if p.peek() != '}' {
			p.pos = save
			return 0, 0, false, nil
		}
		p.pos++
		if mx != -1 && mx < mn {
			return 0, 0, true, &ParseError{Offset: save, Msg: "quantifier max < min"}
		}
		return mn, mx, true, nil
	}
	return 0, 0, false, nil
}

func isDigit(c int) bool { return c >= '0' && c <= '9' }

func (p *parser) peek() int {
	if p.pos >= len(p.src) {
		return -1
	}
	return int(p.src[p.pos])
}

func (p *parser) parseAtom() (Node, error) {
	pos := p.pos
	c := p.peek()
	switch c {
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '.':
		p.pos++
		return &AtomDot{baseNode: baseNode{pos}, DotAll: p.flags.DotAll}, nil
	case '^':
		p.pos++
		return &AtomBoundary{baseNode: baseNode{pos}, Kind: BoundaryStart}, nil
	case '$':
		p.pos++
		return &AtomBoundary{baseNode: baseNode{pos}, Kind: BoundaryEnd}, nil
	case '\\':
		return p.parseEscape()
	}
	if c == '{' || c == '}' {
		return nil, &ParseError{Offset: pos, Msg: "lone quantifier metacharacter"}
	}
	if c == '*' || c == '+' || c == '?' {
		return nil, &ParseError{Offset: pos, Msg: "quantifier with nothing to repeat"}
	}
	p.pos++
	r, size := utf8.DecodeRuneInString(p.src[pos:])
	if r == utf8.RuneError && size == 1 {
		return &AtomLit{baseNode: baseNode{pos}, R: rune(p.src[pos]), Invalid: true}, nil
	}
	p.pos = pos + size
	return &AtomLit{baseNode: baseNode{pos}, R: r}, nil
}

func (p *parser) parseGroup() (Node, error) {
	pos := p.pos
	p.pos++ // '('
	capture := true
	var applied string
	var childFlags Flags = p.flags
	if p.peek() == '?' {
		p.pos++
		switch p.peek() {
		case ':':
			p.pos++
			capture = false
		case '=':
			p.pos++
			ch, err := p.parseUntilGroup()
			if err != nil {
				return nil, err
			}
			return &Look{baseNode: baseNode{pos}, Child: ch, Neg: false}, nil
		case '!':
			p.pos++
			ch, err := p.parseUntilGroup()
			if err != nil {
				return nil, err
			}
			return &Look{baseNode: baseNode{pos}, Child: ch, Neg: true}, nil
		case '<':
			p.pos++
			switch p.peek() {
			case '=':
				p.pos++
				ch, err := p.parseUntilGroup()
				if err != nil {
					return nil, err
				}
				if _, err := RequireFixedLength(ch); err != nil {
					return nil, &ParseError{Offset: pos, Msg: "lookbehind is not fixed-length: " + err.Error()}
				}
				return &Look{baseNode: baseNode{pos}, Child: ch, Behind: true, Neg: false}, nil
			case '!':
				p.pos++
				ch, err := p.parseUntilGroup()
				if err != nil {
					return nil, err
				}
				if _, err := RequireFixedLength(ch); err != nil {
					return nil, &ParseError{Offset: pos, Msg: "lookbehind is not fixed-length: " + err.Error()}
				}
				return &Look{baseNode: baseNode{pos}, Child: ch, Behind: true, Neg: true}, nil
			default:
				name, err := p.parseName()
				if err != nil {
					return nil, err
				}
				if p.peek() != '>' {
					return nil, &ParseError{Offset: p.pos, Msg: "expected > in named group"}
				}
				p.pos++
				idx := p.newCapture(name, pos)
				ch, err := p.parseAlternation(1)
				if err != nil {
					return nil, err
				}
				if err := p.expectClose(pos); err != nil {
					return nil, err
				}
				return &Group{baseNode: baseNode{pos}, Child: ch, Capture: true, Index: idx, Name: name, Flags: p.flags}, nil
			}
		default:
			toggle, on, err := p.parseFlagToggle()
			if err != nil {
				return nil, err
			}
			applied = toggle
			if p.peek() == ')' {
				p.pos++
				nf, err := p.flags.with(toggle, on)
				if err != nil {
					return nil, err
				}
				p.flags = nf
				return &Concat{baseNode: baseNode{pos}}, nil
			}
			if p.peek() != ':' {
				return nil, &ParseError{Offset: p.pos, Msg: "expected : or ) in flag group"}
			}
			p.pos++
			capture = false
			nf, err := p.flags.with(toggle, on)
			if err != nil {
				return nil, err
			}
			childFlags = nf
		}
	}
	var idx int
	if capture {
		idx = p.newCapture("", pos)
	}
	saved := p.flags
	p.flags = childFlags
	ch, err := p.parseAlternation(1)
	p.flags = saved
	if err != nil {
		return nil, err
	}
	if err := p.expectClose(pos); err != nil {
		return nil, err
	}
	return &Group{baseNode: baseNode{pos}, Child: ch, Capture: capture, Index: idx, Name: "", Flags: childFlags, Set: applied}, nil
}

func (p *parser) expectClose(open int) error {
	if p.peek() != ')' {
		return &ParseError{Offset: p.pos, Msg: "unbalanced group, missing )"}
	}
	p.pos++
	return nil
}

func (p *parser) parseUntilGroup() (Node, error) {
	ch, err := p.parseAlternation(1)
	if err != nil {
		return nil, err
	}
	if p.peek() != ')' {
		return nil, &ParseError{Offset: p.pos, Msg: "unbalanced lookaround, missing )"}
	}
	p.pos++
	return ch, nil
}

func (p *parser) parseName() (string, error) {
	start := p.pos
	c := p.peek()
	if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
		return "", &ParseError{Offset: p.pos, Msg: "group name must start with letter or _"}
	}
	for {
		c := p.peek()
		if c == -1 || c == '>' {
			break
		}
		if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || isDigit(c)) {
			return "", &ParseError{Offset: p.pos, Msg: "invalid character in group name"}
		}
		p.pos++
	}
	if start == p.pos {
		return "", &ParseError{Offset: start, Msg: "empty group name"}
	}
	return p.src[start:p.pos], nil
}

func (p *parser) parseFlagToggle() (toggle string, on bool, err error) {
	on = true
	start := p.pos
	for {
		c := p.peek()
		if c == '-' {
			on = false
			p.pos++
			continue
		}
		if c == ':' || c == ')' {
			break
		}
		if c == -1 {
			return "", false, &ParseError{Offset: p.pos, Msg: "unterminated flag group"}
		}
		if c != 'i' && c != 's' && c != 'm' {
			return "", false, &ParseError{Offset: p.pos, Msg: fmt.Sprintf("unknown flag %q", string(rune(c)))}
		}
		toggle += string(rune(c))
		p.pos++
	}
	if toggle == "" {
		return "", false, &ParseError{Offset: start, Msg: "empty flag list"}
	}
	return toggle, on, nil
}

func (p *parser) newCapture(name string, pos int) int {
	p.count++
	if name != "" {
		if _, dup := p.names[name]; dup {
			p.dupName = name
		}
		p.names[name] = p.count
	}
	return p.count
}
