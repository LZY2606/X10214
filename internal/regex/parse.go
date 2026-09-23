package regex

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ParseError points at a byte offset in the pattern.
type ParseError struct {
	Offset int    `json:"offset"`
	Msg    string `json:"msg"`
}

func (e *ParseError) Error() string { return fmt.Sprintf("parse error at %d: %s", e.Offset, e.Msg) }

type parser struct {
	src   string
	pos   int
	flags Flags
	// outer flags captured by flag-scoped groups.
	flagStack []Flags
	nextGroup int
	names     map[int]string
	usedNames map[string]int
}

// Parse compiles the textual pattern into an AST. initialFlags are the flags
// active at pattern start (in CT/1.0 flags are normally embedded as (?ims)).
func Parse(src string, initialFlags Flags) (*AST, error) {
	p := &parser{
		src:       src,
		flags:     initialFlags,
		nextGroup: 1,
		names:     map[int]string{},
		usedNames: map[string]int{},
	}
	root, err := p.parseAlt(initialFlags, false)
	if err != nil {
		return nil, err
	}
	if p.pos != len(src) {
		return nil, &ParseError{Offset: p.pos, Msg: "unexpected trailing input"}
	}
	return &AST{
		Root:       root,
		Src:        src,
		Flags:      initialFlags,
		GroupCount: p.nextGroup - 1,
		GroupNames: p.names,
	}, nil
}

func (p *parser) err(at int, format string, args ...any) error {
	return &ParseError{Offset: at, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) peek() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) at(s string) bool { return strings.HasPrefix(p.src[p.pos:], s) }

// parseAlt parses a sequence of alternatives until ')' or end.
// baseFlags are the flags in force entering this branch scope.
func (p *parser) parseAlt(baseFlags Flags, inGroup bool) (*Node, error) {
	start := p.pos
	var branches [][]*Node
	var seq []*Node
	flush := func() {
		branches = append(branches, seq)
		seq = nil
	}
	for {
		if p.pos >= len(p.src) {
			flush()
			break
		}
		c := p.src[p.pos]
		if c == ')' {
			if !inGroup {
				return nil, p.err(p.pos, "unmatched ')'")
			}
			flush()
			break
		}
		if c == '|' {
			p.pos++
			flush()
			p.flags = baseFlags
			continue
		}
		atom, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		if atom != nil {
			seq = append(seq, atom)
		}
	}
	return p.buildAlt(start, p.pos, branches), nil
}

func (p *parser) buildAlt(start, end int, branches [][]*Node) *Node {
	alts := make([]*Node, len(branches))
	for i, s := range branches {
		alts[i] = p.buildSeq(s)
	}
	var root *Node
	if len(alts) == 1 {
		root = alts[0]
	} else {
		root = &Node{Kind: KAlt, Children: alts, Src: TextRange{start, end}}
	}
	if root == nil {
		root = &Node{Kind: KEmpty, Src: TextRange{start, end}}
	}
	root.Src = TextRange{start, end}
	return root
}

func (p *parser) buildSeq(seq []*Node) *Node {
	if len(seq) == 0 {
		return &Node{Kind: KEmpty}
	}
	if len(seq) == 1 {
		return seq[0]
	}
	return &Node{Kind: KConcat, Children: seq}
}

func (p *parser) parseAtom() (*Node, error) {
	start := p.pos
	c := p.src[p.pos]
	var node *Node
	var err error
	switch {
	case c == '(':
		node, err = p.parseGroup()
	case c == '[':
		node, err = p.parseClass()
	case c == '.':
		p.pos++
		node = &Node{Kind: KAny, Src: TextRange{start, p.pos}}
	case c == '^':
		p.pos++
		node = &Node{Kind: KBeginLine, Src: TextRange{start, p.pos}}
	case c == '$':
		p.pos++
		node = &Node{Kind: KEndLine, Src: TextRange{start, p.pos}}
	case c == '\\':
		node, err = p.parseEscape()
	case c == '*' || c == '+' || c == '?':
		return nil, p.err(start, "quantifier with nothing to repeat")
	case c == '|' || c == ')':
		return nil, p.err(start, "unexpected %q", c)
	default:
		r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += sz
		node = &Node{Kind: KChar, Char: r, Src: TextRange{start, p.pos}}
	}
	if err != nil {
		return nil, err
	}
	return p.parseQuantifier(node)
}

func (p *parser) parseGroup() (*Node, error) {
	start := p.pos
	p.pos++ // consume '('
	kind := '('
	capture := true
	name := ""
	var on, off Flags
	if p.pos < len(p.src) && p.src[p.pos] == '?' {
		p.pos++
		if p.pos >= len(p.src) {
			return nil, p.err(p.pos, "dangling '?' in group")
		}
		d := p.src[p.pos]
		switch {
		case d == ':':
			p.pos++
			capture = false
			kind = ':'
		case d == '=':
			p.pos++
			return p.parseLookaround(start, true, false)
		case d == '!':
			p.pos++
			return p.parseLookaround(start, false, false)
		case d == '<' && p.pos+1 < len(p.src) && (p.src[p.pos+1] == '=' || p.src[p.pos+1] == '!'):
			behind := true
			positive := p.src[p.pos+1] == '='
			p.pos += 2
			return p.parseLookaround(start, positive, behind)
		case d == '<':
			p.pos++
			nm, err := p.readName('>')
			if err != nil {
				return nil, err
			}
			name = nm
			kind = 'n'
		case d == 'P':
			// (?P<name>...)
			p.pos++
			if p.pos >= len(p.src) || p.src[p.pos] != '<' {
				return nil, p.err(p.pos, "expected < in (?P<...>)")
			}
			p.pos++
			nm, err := p.readName('>')
			if err != nil {
				return nil, err
			}
			name = nm
			kind = 'n'
		case d == '\'':
			p.pos++
			nm, err := p.readName('\'')
			if err != nil {
				return nil, err
			}
			name = nm
			kind = 'n'
		case d == 'i' || d == 'm' || d == 's' || d == '-' || d == ')':
			on, off, err = p.readInlineFlags()
			if err != nil {
				return nil, err
			}
			if p.pos < len(p.src) && p.src[p.pos] == ':' {
				p.pos++
				capture = false
				kind = ':'
			} else {
				if p.peek() != ')' {
					return nil, p.err(p.pos, "flag-only group must end with )")
				}
				// (?ims) sets flags for the remainder of the current group.
				p.flags = (p.flags.With(on)).Without(off)
				// Represent as a non-capturing empty group carrying the flag
				// delta so compilation installs/scopes flags explicitly.
				p.pos++ // consume ')'
				return &Node{
					Kind: KGroup, Capture: false, OnFlags: on, OffFlags: off,
					Children: []*Node{{Kind: KEmpty}},
					Src:      TextRange{start, p.pos},
				}, nil
			}
		default:
			return nil, p.err(p.pos, "unsupported group form ?%c", d)
		}
	}

	idx := 0
	baseFlags := p.flags.With(on).Without(off)
	if capture {
		idx = p.nextGroup
		p.nextGroup++
		if name != "" {
			if other, dup := p.usedNames[name]; dup {
				return nil, p.err(start, "duplicate capture group name %q (also group %d)", name, other)
			}
			p.usedNames[name] = idx
			p.names[idx] = name
		}
	}
	p.flagStack = append(p.flagStack, p.flags)
	p.flags = baseFlags
	body, err := p.parseAlt(baseFlags, true)
	if err != nil {
		return nil, err
	}
	if p.pos >= len(p.src) || p.src[p.pos] != ')' {
		return nil, p.err(p.pos, "unterminated group")
	}
	p.pos++
	p.flags = p.flagStack[len(p.flagStack)-1]
	p.flagStack = p.flagStack[:len(p.flagStack)-1]
	_ = kind
	node := &Node{
		Kind: KGroup, Children: []*Node{body}, Capture: capture,
		Index: idx, Name: name, OnFlags: on, OffFlags: off,
		Src: TextRange{start, p.pos},
	}
	return p.parseQuantifier(node)
}

func (p *parser) readName(close byte) (string, error) {
	from := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != close {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return "", p.err(p.pos, "unterminated group name")
	}
	name := p.src[from:p.pos]
	p.pos++ // consume close
	if name == "" {
		return "", p.err(from, "empty group name")
	}
	return name, nil
}

// readInlineFlags parses ims-ims up to ')' or ':' (p.pos sits on the first
// flag letter). On return p.pos sits at ')' or ':'.
func (p *parser) readInlineFlags() (on, off Flags, err error) {
	cur := &on
	for p.pos < len(p.src) {
		d := p.src[p.pos]
		if d == ')' || d == ':' {
			return on, off, nil
		}
		if d == '-' {
			cur = &off
			p.pos++
			continue
		}
		var f Flags
		switch d {
		case 'i':
			f = FIgnoreCase
		case 'm':
			f = FMultiline
		case 's':
			f = FDotAll
		default:
			return 0, 0, p.err(p.pos, "unsupported flag %q (only i,m,s)", string(d))
		}
		*cur = cur.With(f)
		p.pos++
	}
	return 0, 0, p.err(p.pos, "unterminated inline flags")
}

func (p *parser) parseLookaround(start int, positive, behind bool) (*Node, error) {
	baseFlags := p.flags
	p.flagStack = append(p.flagStack, p.flags)
	body, err := p.parseAlt(baseFlags, true)
	if err != nil {
		return nil, err
	}
	if p.pos >= len(p.src) || p.src[p.pos] != ')' {
		return nil, p.err(p.pos, "unterminated lookaround")
	}
	p.pos++
	p.flags = p.flagStack[len(p.flagStack)-1]
	p.flagStack = p.flagStack[:len(p.flagStack)-1]
	node := &Node{
		Kind: KLookaround, Children: []*Node{body},
		LA:  &Lookaround{Positive: positive, Behind: behind},
		Src: TextRange{start, p.pos},
	}
	return p.parseQuantifier(node)
}

func (p *parser) parseQuantifier(node *Node) (*Node, error) {
	if p.pos >= len(p.src) {
		return node, nil
	}
	c := p.src[p.pos]
	if c != '*' && c != '+' && c != '?' && c != '{' {
		return node, nil
	}
	min, max, ok := p.tryReadCounts()
	if !ok {
		// Not a quantifier: emit the literal '{' as its own atom. The caller
		// (parseAtom) already consumed node, so wrap into a concat.
		lit := &Node{Kind: KChar, Char: '{', Src: TextRange{p.pos, p.pos + 1}}
		p.pos++
		return &Node{Kind: KConcat, Children: []*Node{node, lit}, Src: TextRange{node.Src.Start, p.pos}}, nil
	}
	if p.pos < len(p.src) && p.src[p.pos] == '+' {
		return nil, p.err(p.pos, "possessive quantifiers are not supported in CT/1.0")
	}
	greedy := true
	if p.pos < len(p.src) && p.src[p.pos] == '?' {
		greedy = false
		p.pos++
	}
	q := &Quant{Min: min, Max: max, Greedy: greedy}
	return &Node{Kind: KRepeat, Children: []*Node{node}, Repeat: q, Src: TextRange{node.Src.Start, p.pos}}, nil
}

// tryReadCounts consumes a valid quantifier starting at p.pos. It returns
// ok=false (without changing p.pos) when a '{' does not form a valid counted
// quantifier, so the brace can be treated as a literal.
func (p *parser) tryReadCounts() (min, max int, ok bool) {
	switch p.src[p.pos] {
	case '*':
		p.pos++
		return 0, -1, true
	case '+':
		p.pos++
		return 1, -1, true
	case '?':
		p.pos++
		return 0, 1, true
	}
	save := p.pos
	p.pos++ // '{'
	n, gotN := p.readDecimal()
	if !gotN {
		p.pos = save
		return 0, 0, false
	}
	min, max = n, n
	if p.pos >= len(p.src) {
		p.pos = save
		return 0, 0, false
	}
	switch p.src[p.pos] {
	case '}':
		p.pos++
		return min, max, true
	case ',':
	default:
		p.pos = save
		return 0, 0, false
	}
	p.pos++ // comma
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		return min, -1, true
	}
	m, gotM := p.readDecimal()
	if !gotM || p.pos >= len(p.src) || p.src[p.pos] != '}' {
		p.pos = save
		return 0, 0, false
	}
	p.pos++
	if m < min {
		// Real syntax error, not a literal brace.
		p.pos = save
		return 0, 0, false
	}
	return min, m, true
}

func (p *parser) readDecimal() (int, bool) {
	from := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	if from == p.pos {
		return 0, false
	}
	n, err := strconv.Atoi(p.src[from:p.pos])
	if err != nil {
		return 0, false
	}
	return n, true
}
