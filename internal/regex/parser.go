package regex

import "fmt"

// parser turns pattern text into an AST and numbers capture groups in
// opening-paren order (the documented, stable group numbering).
type parser struct {
	pat []rune
	pos int

	groupCount int
	groups     map[string]int // name -> 1-based index
}

// PatternInfo carries group metadata produced while parsing.
type PatternInfo struct {
	GroupCount int
	Groups     map[string]int
}

// Parse parses pattern into an AST plus group metadata.
func Parse(pattern string) (Node, *PatternInfo, error) {
	p := &parser{pat: []rune(pattern), groups: map[string]int{}}
	root, err := p.parseAlt()
	if err != nil {
		return nil, nil, err
	}
	if p.pos < len(p.pat) {
		return nil, nil, fmt.Errorf("unexpected %q at rune %d", p.pat[p.pos], p.pos)
	}
	return root, &PatternInfo{GroupCount: p.groupCount, Groups: p.groups}, nil
}

func (p *parser) parseAlt() (Node, error) {
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
	return &Alt{Branches: branches}, nil
}

func (p *parser) parseConcat() (Node, error) {
	var nodes []Node
	for {
		r := p.peek()
		if r == 0 || r == '|' || r == ')' {
			break
		}
		n, err := p.parseRepeat()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 0 {
		return &Concat{}, nil
	}
	if len(nodes) == 1 {
		return nodes[0], nil
	}
	return &Concat{Nodes: nodes}, nil
}

func (p *parser) parseRepeat() (Node, error) {
	atom, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	for {
		r := p.peek()
		switch r {
		case '*', '+', '?':
			if _, nested := atom.(*Repeat); nested {
				return nil, fmt.Errorf("nested quantifier at rune %d", p.pos)
			}
			min, max := 0, -1
			if r == '+' {
				min = 1
			}
			if r == '?' {
				max = 1
			}
			p.pos++
			lazy := false
			if p.peek() == '?' {
				lazy = true
				p.pos++
			}
			atom = &Repeat{Child: atom, Min: min, Max: max, Lazy: lazy}
		case '{':
			if _, nested := atom.(*Repeat); nested {
				return nil, fmt.Errorf("nested quantifier at rune %d", p.pos)
			}
			lo := p.pos
			min, max, ok := p.tryBounded()
			if !ok {
				// not a real quantifier: leave the '{' as a literal
				p.pos = lo
				return atom, nil
			}
			lazy := false
			if p.peek() == '?' {
				lazy = true
				p.pos++
			}
			atom = &Repeat{Child: atom, Min: min, Max: max, Lazy: lazy}
		default:
			return atom, nil
		}
	}
}

// tryBounded parses {n}, {n,}, {n,m} starting at '{'; on failure it
// restores the position and reports false (the '{' is then literal).
func (p *parser) tryBounded() (min, max int, ok bool) {
	start := p.pos
	p.pos++ // consume '{'
	min, got := p.readDigits()
	if !got {
		p.pos = start
		return 0, 0, false
	}
	max = min
	if p.peek() == ',' {
		p.pos++
		if n, got := p.readDigits(); got {
			max = n
		} else {
			max = -1
		}
	}
	if p.peek() != '}' {
		p.pos = start
		return 0, 0, false
	}
	p.pos++
	if max >= 0 && max < min {
		p.pos = start
		return 0, 0, false
	}
	return min, max, true
}

func (p *parser) readDigits() (int, bool) {
	if !isASCIIDigit(p.peek()) {
		return 0, false
	}
	n := 0
	for isASCIIDigit(p.peek()) {
		n = n*10 + int(p.pat[p.pos]-'0')
		p.pos++
	}
	return n, true
}

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

func (p *parser) peek() rune {
	if p.pos >= len(p.pat) {
		return 0
	}
	return p.pat[p.pos]
}

func (p *parser) peekAt(n int) rune {
	if p.pos+n >= len(p.pat) {
		return 0
	}
	return p.pat[p.pos+n]
}

func (p *parser) parseAtom() (Node, error) {
	r := p.peek()
	switch r {
	case 0:
		return nil, fmt.Errorf("expected an atom at end of pattern")
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '.':
		p.pos++
		return &Any{}, nil
	case '^':
		p.pos++
		return &Anchor{Kind: AnchorBeginTextCaret}, nil
	case '$':
		p.pos++
		return &Anchor{Kind: AnchorEndTextOrFinalNL}, nil
	case '\\':
		return p.parseEscapeAtom()
	case '*', '+', '?', '|', ')':
		return nil, fmt.Errorf("unexpected %q at rune %d", r, p.pos)
	default:
		p.pos++
		return &Literal{R: r}, nil
	}
}

func (p *parser) parseGroup() (Node, error) {
	p.pos++ // consume '('
	if p.peek() != '?' {
		return p.captureGroup("")
	}
	p.pos++ // consume '?'
	switch p.peek() {
	case ':':
		p.pos++
		child, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		if err := p.expect(')'); err != nil {
			return nil, err
		}
		return &Group{Child: child, Capture: false}, nil
	case '<':
		p.pos++ // consume '<'
		if p.peek() == '=' || p.peek() == '!' {
			neg := p.peek() == '!'
			p.pos++
			child, err := p.parseAlt()
			if err != nil {
				return nil, err
			}
			if err := p.expect(')'); err != nil {
				return nil, err
			}
			return &Look{Child: child, Negate: neg, Behind: true}, nil
		}
		name, err := p.readNameUntil('>')
		if err != nil {
			return nil, err
		}
		return p.captureGroup(name)
	case 'P':
		p.pos++
		if p.peek() != '<' {
			return nil, fmt.Errorf("expected '<' after (?P at rune %d", p.pos)
		}
		p.pos++
		name, err := p.readNameUntil('>')
		if err != nil {
			return nil, err
		}
		return p.captureGroup(name)
	case '\'':
		p.pos++
		name, err := p.readNameUntil('\'')
		if err != nil {
			return nil, err
		}
		return p.captureGroup(name)
	case '=':
		p.pos++
		child, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		if err := p.expect(')'); err != nil {
			return nil, err
		}
		return &Look{Child: child, Negate: false, Behind: false}, nil
	case '!':
		p.pos++
		child, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		if err := p.expect(')'); err != nil {
			return nil, err
		}
		return &Look{Child: child, Negate: true, Behind: false}, nil
	default:
		return nil, fmt.Errorf("unsupported group flag %q at rune %d", p.peek(), p.pos)
	}
}

func (p *parser) captureGroup(name string) (Node, error) {
	p.groupCount++
	idx := p.groupCount
	if name != "" {
		if _, dup := p.groups[name]; dup {
			return nil, fmt.Errorf("duplicate capture group name %q", name)
		}
		p.groups[name] = idx
	}
	child, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return &Group{Child: child, Index: idx, Name: name, Capture: true}, nil
}

func (p *parser) readNameUntil(end rune) (string, error) {
	var name []rune
	for p.peek() != 0 && p.peek() != end {
		r := p.peek()
		if r == ')' || r == '(' {
			return "", fmt.Errorf("invalid character %q in group name", r)
		}
		name = append(name, r)
		p.pos++
	}
	if len(name) == 0 {
		return "", fmt.Errorf("empty group name at rune %d", p.pos)
	}
	if err := p.expect(end); err != nil {
		return "", err
	}
	return string(name), nil
}

func (p *parser) expect(r rune) error {
	if p.peek() != r {
		return fmt.Errorf("expected %q at rune %d, got %q", r, p.pos, p.peek())
	}
	p.pos++
	return nil
}

func (p *parser) parseClass() (Node, error) {
	p.pos++ // consume '['
	neg := false
	if p.peek() == '^' {
		neg = true
		p.pos++
	}
	var items []ClassItem
	first := true
	for {
		if p.peek() == ']' && !first {
			break
		}
		first = false
		if p.peek() == 0 {
			return nil, fmt.Errorf("unterminated character class")
		}
		item, err := p.readClassItem()
		if err != nil {
			return nil, err
		}
		// range?  item '-' item   ('-' as last char stays literal)
		if item.Table == "" && p.peek() == '-' && p.peekAt(1) != ']' && p.peekAt(1) != 0 {
			p.pos++ // consume '-'
			hi, err := p.readClassItem()
			if err != nil {
				return nil, err
			}
			if hi.Table != "" {
				return nil, fmt.Errorf("range endpoints must be literals")
			}
			if hi.Lo < item.Lo {
				return nil, fmt.Errorf("invalid range %s-%s", quoteRune(item.Lo), quoteRune(hi.Lo))
			}
			items = append(items, ClassItem{Lo: item.Lo, Hi: hi.Lo})
			continue
		}
		items = append(items, item)
	}
	p.pos++ // consume ']'
	return &Class{Negated: neg, Items: items}, nil
}

// readClassItem reads one item inside [...] (literal or escape).
func (p *parser) readClassItem() (ClassItem, error) {
	if p.peek() != '\\' {
		r := p.peek()
		p.pos++
		return ClassItem{Lo: r, Hi: r}, nil
	}
	p.pos++ // consume backslash
	r := p.peek()
	if r == 0 {
		return ClassItem{}, fmt.Errorf("dangling backslash in class")
	}
	switch r {
	case 'd', 'w', 's':
		p.pos++
		return ClassItem{Table: shortClassTable(r)}, nil
	case 'D', 'W', 'S':
		p.pos++
		return ClassItem{Table: shortClassTable(r + 'a' - 'A'), Neg: true}, nil
	case 'p', 'P':
		p.pos++
		name, err := p.readUnicodeProp()
		if err != nil {
			return ClassItem{}, err
		}
		if !isKnownTable(name) {
			return ClassItem{}, fmt.Errorf("unknown unicode property %q", name)
		}
		return ClassItem{Table: name, Neg: r == 'P'}, nil
	default:
		p.pos++
		return ClassItem{Lo: simpleEscaped(r), Hi: simpleEscaped(r)}, nil
	}
}

func shortClassTable(r rune) string {
	switch r {
	case 'd':
		return "Nd"
	case 'w':
		return "Word"
	case 's':
		return "White_Space"
	}
	return ""
}

func (p *parser) parseEscapeAtom() (Node, error) {
	start := p.pos
	p.pos++ // consume backslash
	r := p.peek()
	if r == 0 {
		return nil, fmt.Errorf("dangling backslash at rune %d", start)
	}
	switch r {
	case 'd', 'w', 's':
		p.pos++
		return &Class{Items: []ClassItem{{Table: shortClassTable(r)}}}, nil
	case 'D', 'W', 'S':
		p.pos++
		return &Class{Items: []ClassItem{{Table: shortClassTable(r + 'a' - 'A'), Neg: true}}}, nil
	case 'b':
		p.pos++
		return &Anchor{Kind: AnchorWordBoundary}, nil
	case 'B':
		p.pos++
		return &Anchor{Kind: AnchorNonWordBoundary}, nil
	case 'A':
		p.pos++
		return &Anchor{Kind: AnchorBeginText}, nil
	case 'z':
		p.pos++
		return &Anchor{Kind: AnchorEndText}, nil
	case 'k':
		p.pos++
		if p.peek() != '<' {
			return nil, fmt.Errorf("expected '<' after \\k at rune %d", p.pos)
		}
		p.pos++
		name, err := p.readNameUntil('>')
		if err != nil {
			return nil, err
		}
		idx, ok := p.groups[name]
		if !ok {
			return nil, fmt.Errorf("backreference to unknown group %q", name)
		}
		return &Backref{Index: idx, Name: name}, nil
	case 'p', 'P':
		p.pos++
		name, err := p.readUnicodeProp()
		if err != nil {
			return nil, err
		}
		if !isKnownTable(name) {
			return nil, fmt.Errorf("unknown unicode property %q", name)
		}
		return &Class{Items: []ClassItem{{Table: name, Neg: r == 'P'}}}, nil
	}
	if r >= '1' && r <= '9' {
		n := 0
		for isASCIIDigit(p.peek()) {
			n = n*10 + int(p.peek()-'0')
			p.pos++
		}
		if n > p.groupCount {
			return nil, fmt.Errorf("backreference %d exceeds number of capture groups", n)
		}
		return &Backref{Index: n}, nil
	}
	p.pos++
	return &Literal{R: simpleEscaped(r)}, nil
}

// readUnicodeProp parses \pL, \p{Lu} style property names.
func (p *parser) readUnicodeProp() (string, error) {
	if p.peek() == '{' {
		p.pos++
		start := p.pos
		for p.peek() != 0 && p.peek() != '}' {
			p.pos++
		}
		if p.peek() != '}' {
			return "", fmt.Errorf("unterminated \\p{...}")
		}
		name := string(p.pat[start:p.pos])
		p.pos++
		return name, nil
	}
	r := p.peek()
	if r == 0 {
		return "", fmt.Errorf("missing unicode property name")
	}
	p.pos++
	return string(r), nil
}

// simpleEscaped maps escape letters to their literal rune.
func simpleEscaped(r rune) rune {
	switch r {
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
		return '\a'
	case '0':
		return 0
	}
	return r
}
