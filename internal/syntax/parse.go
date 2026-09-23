package syntax

import (
	"fmt"
	"unicode"
)

// Parsed is a parsed pattern: AST plus capture-group metadata.
type Parsed struct {
	Root   *Node
	Groups []GroupInfo
}

type GroupInfo struct {
	Index int
	Name  string
}

type parser struct {
	src    []rune
	pos    int
	groups []GroupInfo
	names  map[string]int
}

// Parse compiles the pattern string into an AST.
func Parse(pattern string) (*Parsed, error) {
	p := &parser{src: []rune(pattern), names: map[string]int{}}
	root, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.src) {
		return nil, fmt.Errorf("syntax: unexpected %q at rune %d", p.src[p.pos], p.pos)
	}
	return &Parsed{Root: root, Groups: p.groups}, nil
}

func (p *parser) peek() rune {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) hasPrefix(s string) bool {
	r := []rune(s)
	if p.pos+len(r) > len(p.src) {
		return false
	}
	for i, c := range r {
		if p.src[p.pos+i] != c {
			return false
		}
	}
	return true
}

func (p *parser) parseAlt() (*Node, error) {
	var alts []*Node
	for {
		part, err := p.parseConcat()
		if err != nil {
			return nil, err
		}
		alts = append(alts, part)
		if p.peek() != '|' {
			break
		}
		p.pos++
	}
	if len(alts) == 1 {
		return alts[0], nil
	}
	return &Node{Kind: KAlternate, Sub: alts}, nil
}

func (p *parser) parseConcat() (*Node, error) {
	var seq []*Node
	for {
		r := p.peek()
		if r == 0 || r == '|' || r == ')' {
			break
		}
		n, err := p.parseRepeat()
		if err != nil {
			return nil, err
		}
		seq = append(seq, n)
	}
	switch len(seq) {
	case 0:
		return &Node{Kind: KEmpty}, nil
	case 1:
		return seq[0], nil
	default:
		return &Node{Kind: KConcat, Sub: seq}, nil
	}
}

func (p *parser) parseRepeat() (*Node, error) {
	atom, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	min, max, ok, err := p.tryQuantifier()
	if err != nil {
		return nil, err
	}
	if !ok {
		return atom, nil
	}
	lazy := false
	if p.peek() == '?' {
		lazy = true
		p.pos++
	}
	if max >= 0 && max < min {
		return nil, fmt.Errorf("syntax: repeat min > max at rune %d", p.pos)
	}
	if max > MaxRepeat {
		return nil, fmt.Errorf("syntax: repeat bound %d exceeds max %d", max, MaxRepeat)
	}
	return &Node{Kind: KRepeat, Child: atom, Min: min, Max: max, Lazy: lazy}, nil
}

// tryQuantifier parses *, +, ?, {n}, {n,}, {n,m}. ok=false means no
// quantifier was consumed (a '{' that is not a valid quantifier is literal).
func (p *parser) tryQuantifier() (min, max int, ok bool, err error) {
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
		start := p.pos
		p.pos++
		n, hasN := p.readDigits()
		if !hasN {
			p.pos = start
			return 0, 0, false, nil
		}
		min, max = n, n
		if p.peek() == ',' {
			p.pos++
			max = -1
			if m, hasM := p.readDigits(); hasM {
				max = m
			}
		}
		if p.peek() != '}' {
			p.pos = start
			return 0, 0, false, nil
		}
		p.pos++
		return min, max, true, nil
	}
	return 0, 0, false, nil
}

func (p *parser) readDigits() (int, bool) {
	start := p.pos
	n := 0
	for c := p.peek(); c >= '0' && c <= '9'; c = p.peek() {
		n = n*10 + int(c-'0')
		p.pos++
	}
	return n, p.pos > start
}

func (p *parser) parseAtom() (*Node, error) {
	r := p.peek()
	switch r {
	case 0:
		return nil, fmt.Errorf("syntax: unexpected end of pattern")
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '.':
		p.pos++
		return &Node{Kind: KAny}, nil
	case '^':
		p.pos++
		return &Node{Kind: KAnchor, Anchor: ABeginText}, nil
	case '$':
		p.pos++
		return &Node{Kind: KAnchor, Anchor: AEndText}, nil
	case '\\':
		return p.parseEscape()
	case '*', '+', '?':
		return nil, fmt.Errorf("syntax: unexpected %q at rune %d", r, p.pos)
	case ')', '|':
		return nil, fmt.Errorf("syntax: unexpected %q at rune %d", r, p.pos)
	default:
		p.pos++
		return &Node{Kind: KLiteral, Runes: []rune{r}}, nil
	}
}

func (p *parser) parseGroup() (*Node, error) {
	start := p.pos
	p.pos++ // '('
	if p.hasPrefix("?=") || p.hasPrefix("?!") || p.hasPrefix("?<=") || p.hasPrefix("?<!") {
		negate, behind := false, false
		switch {
		case p.hasPrefix("?="):
			p.pos += 2
		case p.hasPrefix("?!"):
			p.pos += 2
			negate = true
		case p.hasPrefix("?<="):
			p.pos += 3
			behind = true
		case p.hasPrefix("?<!"):
			p.pos += 3
			negate, behind = true, true
		}
		child, err := p.parseAlt()
		if err != nil {
			return nil, err
		}
		if err := p.expect(')', start); err != nil {
			return nil, err
		}
		return &Node{Kind: KLook, Child: child, Negate: negate, Behind: behind}, nil
	}
	nonCap := false
	name := ""
	switch {
	case p.hasPrefix("?:"):
		p.pos += 2
		nonCap = true
	case p.hasPrefix("?P<"):
		p.pos += 3
		n, err := p.readName()
		if err != nil {
			return nil, err
		}
		name = n
	case p.hasPrefix("?<"):
		p.pos += 2
		n, err := p.readName()
		if err != nil {
			return nil, err
		}
		name = n
	}
	child, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if err := p.expect(')', start); err != nil {
		return nil, err
	}
	if nonCap {
		return child, nil
	}
	idx := len(p.groups) + 1
	if name != "" {
		if existing, dup := p.names[name]; dup {
			return nil, fmt.Errorf("syntax: duplicate group name %q (already group %d)", name, existing)
		}
		p.names[name] = idx
	}
	p.groups = append(p.groups, GroupInfo{Index: idx, Name: name})
	return &Node{Kind: KGroup, Child: child, Group: idx, Name: name}, nil
}

func (p *parser) expect(r rune, start int) error {
	if p.peek() != r {
		return fmt.Errorf("syntax: unterminated group starting at rune %d", start)
	}
	p.pos++
	return nil
}

func (p *parser) readName() (string, error) {
	start := p.pos
	for {
		r := p.peek()
		if r == 0 {
			return "", fmt.Errorf("syntax: unterminated group name at rune %d", start)
		}
		if r == '>' {
			if p.pos == start {
				return "", fmt.Errorf("syntax: empty group name at rune %d", start)
			}
			name := string(p.src[start:p.pos])
			p.pos++
			return name, nil
		}
		p.pos++
	}
}
