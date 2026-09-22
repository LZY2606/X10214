package regex

import (
	"fmt"
	"strconv"
	"unicode/utf8"
)

// Pattern is a parsed regular expression.
type Pattern struct {
	Root       *Node
	NumGroups  int // number of capture groups (group 0 is the whole match)
	GroupNames map[string]int
	Source     string
}

// Parse compiles pattern text into an AST. Syntax is pinned to SyntaxVersion.
func Parse(src string) (*Pattern, error) {
	p := &parser{src: src, names: map[string]int{}}
	root, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.runes) {
		return nil, fmt.Errorf("parse: unexpected %q at offset %d", p.runes[p.pos], p.pos)
	}
	return &Pattern{Root: root, NumGroups: p.groups, GroupNames: p.names, Source: src}, nil
}

type parser struct {
	src    string
	runes  []rune
	pos    int
	groups int
	names  map[string]int
}

func (p *parser) peek() (rune, bool) {
	if p.pos >= len(p.runes) {
		return 0, false
	}
	if p.runes == nil {
		p.runes = []rune(p.src)
	}
	return p.runes[p.pos], true
}

func (p *parser) next() (rune, bool) {
	r, ok := p.peek()
	if ok {
		p.pos++
	}
	return r, ok
}

func (p *parser) parseAlt() (*Node, error) {
	if p.runes == nil {
		p.runes = []rune(p.src)
	}
	first, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	alts := []*Node{first}
	for {
		r, ok := p.peek()
		if !ok || r != '|' {
			break
		}
		p.pos++
		n, err := p.parseConcat()
		if err != nil {
			return nil, err
		}
		alts = append(alts, n)
	}
	if len(alts) == 1 {
		return first, nil
	}
	return &Node{Kind: KindAlt, Kids: alts}, nil
}

func (p *parser) parseConcat() (*Node, error) {
	var kids []*Node
	for {
		r, ok := p.peek()
		if !ok || r == '|' || r == ')' {
			break
		}
		n, err := p.parseRepeat()
		if err != nil {
			return nil, err
		}
		kids = append(kids, n)
	}
	switch len(kids) {
	case 0:
		return &Node{Kind: KindEmpty}, nil
	case 1:
		return kids[0], nil
	}
	return &Node{Kind: KindConcat, Kids: kids}, nil
}

func (p *parser) parseRepeat() (*Node, error) {
	atom, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	for {
		r, ok := p.peek()
		if !ok {
			return atom, nil
		}
		var min, max int
		switch r {
		case '*':
			min, max = 0, -1
		case '+':
			min, max = 1, -1
		case '?':
			min, max = 0, 1
		case '{':
			mn, mx, err := p.parseBrace()
			if err != nil {
				return nil, err
			}
			min, max = mn, mx
		default:
			return atom, nil
		}
		p.pos++
		lazy := false
		if r2, ok := p.peek(); ok && r2 == '?' {
			p.pos++
			lazy = true
		}
		atom = &Node{Kind: KindRepeat, Min: min, Max: max, Lazy: lazy, Kids: []*Node{atom}}
	}
}

func (p *parser) parseBrace() (int, int, error) {
	// p.pos points at '{'
	save := p.pos
	p.pos++
	readNum := func() (int, bool) {
		start := p.pos
		for p.pos < len(p.runes) && p.runes[p.pos] >= '0' && p.runes[p.pos] <= '9' {
			p.pos++
		}
		if p.pos == start {
			return 0, false
		}
		n, _ := strconv.Atoi(string(p.runes[start:p.pos]))
		return n, true
	}
	min, ok := readNum()
	if !ok {
		p.pos = save
		return 0, 0, fmt.Errorf("parse: malformed {m,n} at offset %d", save)
	}
	max := min
	if p.pos < len(p.runes) && p.runes[p.pos] == ',' {
		p.pos++
		if m, ok := readNum(); ok {
			max = m
		} else {
			max = -1
		}
	}
	if p.pos >= len(p.runes) || p.runes[p.pos] != '}' {
		p.pos = save
		return 0, 0, fmt.Errorf("parse: malformed {m,n} at offset %d", save)
	}
	p.pos++
	if max >= 0 && max < min {
		return 0, 0, fmt.Errorf("parse: quantifier {%d,%d} has max < min", min, max)
	}
	return min, max, nil
}

func (p *parser) parseAtom() (*Node, error) {
	r, ok := p.next()
	if !ok {
		return nil, fmt.Errorf("parse: unexpected end of pattern")
	}
	switch r {
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '.':
		return &Node{Kind: KindAny}, nil
	case '^':
		return &Node{Kind: KindAnchorStart}, nil
	case '$':
		return &Node{Kind: KindAnchorEnd}, nil
	case '\\':
		return p.parseEscape()
	case '*', '+', '?', '{':
		return nil, fmt.Errorf("parse: quantifier %q without target at offset %d", r, p.pos-1)
	case ')':
		return nil, fmt.Errorf("parse: unmatched ')' at offset %d", p.pos-1)
	}
	return &Node{Kind: KindLiteral, Rune: r}, nil
}

func (p *parser) parseGroup() (*Node, error) {
	// '(' already consumed.
	if r, ok := p.peek(); ok && r == '?' {
		p.pos++
		r2, ok := p.next()
		if !ok {
			return nil, fmt.Errorf("parse: truncated group")
		}
		switch r2 {
		case ':':
			body, err := p.parseAlt()
			if err != nil {
				return nil, err
			}
			if err := p.expectClose(); err != nil {
				return nil, err
			}
			return &Node{Kind: KindGroup, GroupIdx: 0, Kids: []*Node{body}}, nil
		case 'P':
			if r3, ok := p.next(); !ok || r3 != '<' {
				return nil, fmt.Errorf("parse: expected (?P<name>...")
			}
			name, err := p.readName()
			if err != nil {
				return nil, err
			}
			return p.finishCapture(name)
		case '=':
			return p.finishLook(true, true)
		case '!':
			return p.finishLook(true, false)
		case '<':
			r3, ok := p.next()
			if !ok {
				return nil, fmt.Errorf("parse: truncated group")
			}
			switch r3 {
			case '=':
				return p.finishLook(false, true)
			case '!':
				return p.finishLook(false, false)
			default:
				return nil, fmt.Errorf("parse: unknown group (?<%c", r3)
			}
		}
		return nil, fmt.Errorf("parse: unknown group (?%c", r2)
	}
	return p.finishCapture("")
}

func (p *parser) finishCapture(name string) (*Node, error) {
	p.groups++
	idx := p.groups
	if name != "" {
		if _, dup := p.names[name]; dup {
			return nil, fmt.Errorf("parse: duplicate group name %q", name)
		}
		p.names[name] = idx
	}
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if err := p.expectClose(); err != nil {
		return nil, err
	}
	return &Node{Kind: KindGroup, GroupIdx: idx, Name: name, Kids: []*Node{body}}, nil
}

func (p *parser) finishLook(ahead, positive bool) (*Node, error) {
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if err := p.expectClose(); err != nil {
		return nil, err
	}
	return &Node{Kind: KindLook, Ahead: ahead, Positive: positive, Kids: []*Node{body}}, nil
}

func (p *parser) expectClose() error {
	r, ok := p.next()
	if !ok || r != ')' {
		return fmt.Errorf("parse: missing ')'")
	}
	return nil
}

func (p *parser) readName() (string, error) {
	start := p.pos
	for p.pos < len(p.runes) && p.runes[p.pos] != '>' {
		p.pos++
	}
	if p.pos >= len(p.runes) {
		return "", fmt.Errorf("parse: unterminated group name")
	}
	name := string(p.runes[start:p.pos])
	p.pos++
	if name == "" {
		return "", fmt.Errorf("parse: empty group name")
	}
	return name, nil
}

func (p *parser) parseClass() (*Node, error) {
	// '[' already consumed.
	n := &Node{Kind: KindClass}
	if r, ok := p.peek(); ok && r == '^' {
		p.pos++
		n.Negated = true
	}
	first := true
	for {
		r, ok := p.next()
		if !ok {
			return nil, fmt.Errorf("parse: unterminated character class")
		}
		if r == ']' && !first {
			break
		}
		first = false
		var lo rune
		if r == '\\' {
			esc, err := p.next()
			if !ok {
				return nil, fmt.Errorf("parse: unterminated class escape")
			}
			if cls, ok := classEscape(esc); ok {
				// Merge a builtin class into the range set.
				n.Ranges = append(n.Ranges, cls.Ranges...)
				continue
			}
			lo = unescape(esc)
		} else {
			lo = r
		}
		// Possible range.
		if r2, ok := p.peek(); ok && r2 == '-' {
			p.pos++
			r3, ok := p.next()
			if !ok {
				return nil, fmt.Errorf("parse: unterminated character class")
			}
			if r3 == ']' {
				// '-' is literal.
				n.Ranges = append(n.Ranges, [2]rune{lo, lo}, [2]rune{'-', '-'})
				break
			}
			var hi rune
			if r3 == '\\' {
				esc, err := p.next()
				if !ok {
					return nil, fmt.Errorf("parse: unterminated class escape")
				}
				if _, isCls := classEscape(esc); isCls {
					return nil, fmt.Errorf("parse: class escape cannot end a range")
				}
				hi = unescape(esc)
			} else {
				hi = r3
			}
			if hi < lo {
				return nil, fmt.Errorf("parse: invalid range %q-%q", lo, hi)
			}
			n.Ranges = append(n.Ranges, [2]rune{lo, hi})
			continue
		}
		n.Ranges = append(n.Ranges, [2]rune{lo, lo})
	}
	return n, nil
}

func (p *parser) parseEscape() (*Node, error) {
	r, ok := p.next()
	if !ok {
		return nil, fmt.Errorf("parse: trailing backslash")
	}
	if cls, ok := classEscape(r); ok {
		return cls, nil
	}
	if r >= '1' && r <= '9' {
		return &Node{Kind: KindBackref, RefIdx: int(r - '0')}, nil
	}
	if r == 'k' {
		if r2, ok := p.next(); !ok || r2 != '<' {
			return nil, fmt.Errorf("parse: expected \\k<name>")
		}
		name, err := p.readName()
		if err != nil {
			return nil, err
		}
		return &Node{Kind: KindBackref, RefName: name}, nil
	}
	return &Node{Kind: KindLiteral, Rune: unescape(r)}, nil
}

func unescape(r rune) rune {
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
	case '0':
		return 0
	}
	return r
}

// classEscape returns the AST class for \d \w \s and negations.
func classEscape(r rune) (*Node, bool) {
	switch r {
	case 'd':
		return &Node{Kind: KindClass, Ranges: [][2]rune{{'0', '9'}}}, true
	case 'D':
		return &Node{Kind: KindClass, Negated: true, Ranges: [][2]rune{{'0', '9'}}}, true
	case 'w':
		return &Node{Kind: KindClass, Ranges: wordRanges()}, true
	case 'W':
		return &Node{Kind: KindClass, Negated: true, Ranges: wordRanges()}, true
	case 's':
		return &Node{Kind: KindClass, Ranges: spaceRanges()}, true
	case 'S':
		return &Node{Kind: KindClass, Negated: true, Ranges: spaceRanges()}, true
	}
	return nil, false
}

func wordRanges() [][2]rune {
	return [][2]rune{{'0', '9'}, {'A', 'Z'}, {'_', '_'}, {'a', 'z'}}
}

func spaceRanges() [][2]rune {
	return [][2]rune{{'\t', '\r'}, {' ', ' '}}
}

// utf8Len is a small helper kept for tests.
func utf8Len(r rune) int { return utf8.RuneLen(r) }
