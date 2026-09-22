package regex

import (
	"fmt"
	"strings"
	"unicode"
)

// flags holds inline flag state.
type flags struct {
	fold      bool // i
	multiLine bool // m
	dotAll    bool // s
}

type parser struct {
	src        []rune
	pos        int
	fl         flags
	numGroups  int
	names      map[string]int
	maxBackref int
}

// Parse parses a pattern into an AST. Returned also is the number of
// capturing groups and the name->index map.
func Parse(pattern string) (*Node, int, map[string]int, error) {
	p := &parser{src: []rune(pattern), names: map[string]int{}}
	p.fl = flags{}
	node, err := p.parseAlt()
	if err != nil {
		return nil, 0, nil, err
	}
	if p.pos != len(p.src) {
		return nil, 0, nil, fmt.Errorf("syntax error at %d: unexpected %q", p.pos, p.src[p.pos])
	}
	if p.maxBackref > p.numGroups {
		return nil, 0, nil, fmt.Errorf("backreference \\%d refers to non-existent group", p.maxBackref)
	}
	return node, p.numGroups, p.names, nil
}

func (p *parser) errf(format string, args ...any) error {
	return fmt.Errorf("syntax error at %d: %s", p.pos, fmt.Sprintf(format, args...))
}

func (p *parser) peek() rune {
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return -1
}

func (p *parser) next() rune {
	if p.pos < len(p.src) {
		r := p.src[p.pos]
		p.pos++
		return r
	}
	return -1
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
	return &Node{Kind: KAlt, Children: branches}, nil
}

func (p *parser) parseConcat() (*Node, error) {
	var items []*Node
	for {
		r := p.peek()
		if r == -1 || r == '|' || r == ')' {
			break
		}
		n, err := p.parseRepeat()
		if err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	switch len(items) {
	case 0:
		return &Node{Kind: KEmpty}, nil
	case 1:
		return items[0], nil
	}
	return &Node{Kind: KConcat, Children: items}, nil
}

func (p *parser) parseRepeat() (*Node, error) {
	atom, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	for {
		var min, max int
		switch p.peek() {
		case '*':
			p.pos++
			min, max = 0, -1
		case '+':
			p.pos++
			min, max = 1, -1
		case '?':
			p.pos++
			min, max = 0, 1
		case '{':
			save := p.pos
			lo, hi, ok := p.parseCounted()
			if !ok {
				p.pos = save
				return atom, nil
			}
			min, max = lo, hi
		default:
			return atom, nil
		}
		greedy := true
		if p.peek() == '?' {
			p.pos++
			greedy = false
		}
		atom = &Node{Kind: KRepeat, Children: []*Node{atom}, Min: min, Max: max, Greedy: greedy}
	}
}

// parseCounted parses {n}, {n,}, {n,m}. pos is at '{'.
func (p *parser) parseCounted() (int, int, bool) {
	i := p.pos + 1
	start := i
	for i < len(p.src) && p.src[i] >= '0' && p.src[i] <= '9' {
		i++
	}
	if i == start {
		return 0, 0, false
	}
	lo := atoiRunes(p.src[start:i])
	hi := lo
	if i < len(p.src) && p.src[i] == ',' {
		i++
		hs := i
		for i < len(p.src) && p.src[i] >= '0' && p.src[i] <= '9' {
			i++
		}
		if i == hs {
			hi = -1
		} else {
			hi = atoiRunes(p.src[hs:i])
		}
	}
	if i >= len(p.src) || p.src[i] != '}' {
		return 0, 0, false
	}
	if hi >= 0 && hi < lo {
		return 0, 0, false
	}
	p.pos = i + 1
	return lo, hi, true
}

func atoiRunes(rs []rune) int {
	n := 0
	for _, r := range rs {
		n = n*10 + int(r-'0')
		if n > 1_000_000 {
			n = 1_000_000
		}
	}
	return n
}

func (p *parser) parseAtom() (*Node, error) {
	r := p.next()
	switch r {
	case -1:
		return nil, p.errf("unexpected end of pattern")
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '.':
		return &Node{Kind: KAny, DotAll: p.fl.dotAll}, nil
	case '^':
		if p.fl.multiLine {
			return &Node{Kind: KAnchor, Anchor: ABeginLine}, nil
		}
		return &Node{Kind: KAnchor, Anchor: ABeginText}, nil
	case '$':
		if p.fl.multiLine {
			return &Node{Kind: KAnchor, Anchor: AEndLine}, nil
		}
		return &Node{Kind: KAnchor, Anchor: AEndText}, nil
	case '\\':
		return p.parseEscape(false)
	case '*', '+', '?':
		return nil, p.errf("missing repeat argument before %q", r)
	case ')':
		return nil, p.errf("unexpected ')'")
	}
	return &Node{Kind: KLiteral, Rune: r, Fold: p.fl.fold}, nil
}

// parseGroup is entered after '('.
func (p *parser) parseGroup() (*Node, error) {
	if p.peek() == '?' {
		p.pos++
		r := p.next()
		switch {
		case r == ':':
			return p.finishGroup(0, "")
		case r == '=':
			return p.finishLook(true, false)
		case r == '!':
			return p.finishLook(true, true)
		case r == '<':
			r2 := p.next()
			if r2 == '=' {
				return p.finishLook(false, false)
			}
			if r2 == '!' {
				return p.finishLook(false, true)
			}
			name, err := p.readName('>')
			if err != nil {
				return nil, err
			}
			return p.finishNamedGroup(name)
		case r == 'P':
			if p.next() != '<' {
				return nil, p.errf("expected (?P<name>")
			}
			name, err := p.readName('>')
			if err != nil {
				return nil, err
			}
			return p.finishNamedGroup(name)
		case r == 'i' || r == 'm' || r == 's' || r == '-':
			return p.parseFlags(r)
		}
		return nil, p.errf("unsupported group syntax (?%c", r)
	}
	return p.finishGroup(-1, "")
}

// finishGroup parses ")...". index -1 means allocate a capturing index.
func (p *parser) finishGroup(index int, name string) (*Node, error) {
	if index == -1 {
		p.numGroups++
		index = p.numGroups
		if name != "" {
			p.names[name] = index
		}
	}
	saved := p.fl
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if p.next() != ')' {
		return nil, p.errf("missing closing )")
	}
	p.fl = saved
	return &Node{Kind: KGroup, Children: []*Node{body}, Index: index, Name: name}, nil
}

func (p *parser) finishNamedGroup(name string) (*Node, error) {
	if _, dup := p.names[name]; dup {
		return nil, p.errf("duplicate group name %q", name)
	}
	return p.finishGroup(-1, name)
}

func (p *parser) finishLook(ahead, negative bool) (*Node, error) {
	saved := p.fl
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if p.next() != ')' {
		return nil, p.errf("missing closing )")
	}
	p.fl = saved
	return &Node{Kind: KLook, Children: []*Node{body}, Ahead: ahead, Negative: negative}, nil
}

// parseFlags handles (?flags) and (?flags:...). first is the first flag rune.
func (p *parser) parseFlags(first rune) (*Node, error) {
	saved := p.fl
	neg := false
	r := first
	for {
		switch r {
		case 'i':
			if neg {
				p.fl.fold = false
			} else {
				p.fl.fold = true
			}
		case 'm':
			if neg {
				p.fl.multiLine = false
			} else {
				p.fl.multiLine = true
			}
		case 's':
			if neg {
				p.fl.dotAll = false
			} else {
				p.fl.dotAll = true
			}
		case '-':
			if neg {
				return nil, p.errf("double - in flags")
			}
			neg = true
		default:
			return nil, p.errf("unknown flag %q", r)
		}
		r = p.next()
		if r == ')' {
			// Bare flags: apply to the rest of the enclosing group.
			return &Node{Kind: KEmpty}, nil
		}
		if r == ':' {
			body, err := p.parseAlt()
			if err != nil {
				return nil, err
			}
			if p.next() != ')' {
				return nil, p.errf("missing closing )")
			}
			p.fl = saved
			return &Node{Kind: KGroup, Children: []*Node{body}, Index: 0}, nil
		}
	}
}

func (p *parser) readName(term rune) (string, error) {
	var b strings.Builder
	for {
		r := p.next()
		if r == -1 {
			return "", p.errf("unterminated group name")
		}
		if r == term {
			break
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return "", p.errf("invalid character %q in group name", r)
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "", p.errf("empty group name")
	}
	return b.String(), nil
}

// parseEscape handles a backslash escape. inClass reports whether we are
// inside a character class.
func (p *parser) parseEscape(inClass bool) (*Node, error) {
	r := p.next()
	switch r {
	case -1:
		return nil, p.errf("trailing backslash")
	case 'd':
		return classNode(perlDigit, false), nil
	case 'D':
		return classNode(perlDigit, true), nil
	case 'w':
		return classNode(perlWord, false), nil
	case 'W':
		return classNode(perlWord, true), nil
	case 's':
		return classNode(perlSpace, false), nil
	case 'S':
		return classNode(perlSpace, true), nil
	case 'b':
		if inClass {
			return &Node{Kind: KLiteral, Rune: '\b'}, nil
		}
		return &Node{Kind: KAnchor, Anchor: AWordBoundary}, nil
	case 'B':
		if inClass {
			return nil, p.errf("\\B not supported in character class")
		}
		return &Node{Kind: KAnchor, Anchor: ANonWordBoundary}, nil
	case 'A':
		if inClass {
			return nil, p.errf("\\A not supported in character class")
		}
		return &Node{Kind: KAnchor, Anchor: ABeginText}, nil
	case 'z':
		if inClass {
			return nil, p.errf("\\z not supported in character class")
		}
		return &Node{Kind: KAnchor, Anchor: AEndText}, nil
	case 'n':
		return &Node{Kind: KLiteral, Rune: '\n'}, nil
	case 't':
		return &Node{Kind: KLiteral, Rune: '\t'}, nil
	case 'r':
		return &Node{Kind: KLiteral, Rune: '\r'}, nil
	case 'f':
		return &Node{Kind: KLiteral, Rune: '\f'}, nil
	case 'v':
		return &Node{Kind: KLiteral, Rune: '\v'}, nil
	case '0':
		return &Node{Kind: KLiteral, Rune: 0}, nil
	case 'x':
		return p.parseHex()
	case 'p', 'P':
		return p.parseUnicodeClass(r == 'P')
	case 'k':
		if inClass {
			return nil, p.errf("backreference not supported in character class")
		}
		if p.next() != '<' {
			return nil, p.errf("expected \\k<name>")
		}
		name, err := p.readName('>')
		if err != nil {
			return nil, err
		}
		idx, ok := p.names[name]
		if !ok {
			return nil, p.errf("unknown group name %q in backreference", name)
		}
		return &Node{Kind: KBackref, Index: idx}, nil
	}
	if r >= '1' && r <= '9' {
		if inClass {
			return nil, p.errf("backreference not supported in character class")
		}
		idx := int(r - '0')
		if idx > p.maxBackref {
			p.maxBackref = idx
		}
		return &Node{Kind: KBackref, Index: idx}, nil
	}
	if unicode.IsLetter(r) {
		return nil, p.errf("invalid escape \\%c", r)
	}
	return &Node{Kind: KLiteral, Rune: r, Fold: p.fl.fold}, nil
}

func (p *parser) parseHex() (*Node, error) {
	if p.peek() == '{' {
		p.pos++
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] != '}' {
			p.pos++
		}
		if p.pos >= len(p.src) {
			return nil, p.errf("unterminated \\x{...}")
		}
		var v int64
		for _, r := range p.src[start:p.pos] {
			d, ok := hexVal(r)
			if !ok {
				return nil, p.errf("invalid hex digit %q", r)
			}
			v = v*16 + int64(d)
			if v > unicode.MaxRune {
				return nil, p.errf("hex value out of range")
			}
		}
		p.pos++
		return &Node{Kind: KLiteral, Rune: rune(v), Fold: p.fl.fold}, nil
	}
	var v int64
	for i := 0; i < 2; i++ {
		d, ok := hexVal(p.next())
		if !ok {
			return nil, p.errf("expected two hex digits after \\x")
		}
		v = v*16 + int64(d)
	}
	return &Node{Kind: KLiteral, Rune: rune(v), Fold: p.fl.fold}, nil
}

func hexVal(r rune) (int, bool) {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0'), true
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10, true
	case r >= 'A' && r <= 'F':
		return int(r-'A') + 10, true
	}
	return 0, false
}

// parseUnicodeClass parses \p{Name} or \pN.
func (p *parser) parseUnicodeClass(negate bool) (*Node, error) {
	var name string
	if p.peek() == '{' {
		p.pos++
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] != '}' {
			p.pos++
		}
		if p.pos >= len(p.src) {
			return nil, p.errf("unterminated \\p{...}")
		}
		name = string(p.src[start:p.pos])
		p.pos++
	} else {
		r := p.next()
		if r == -1 {
			return nil, p.errf("expected name after \\p")
		}
		name = string(r)
	}
	cl, err := unicodeClass(name)
	if err != nil {
		return nil, p.errf("%v", err)
	}
	cl.Negate = cl.Negate != negate
	return &Node{Kind: KClass, Class: cl}, nil
}
