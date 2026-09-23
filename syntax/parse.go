package syntax

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseError describes a pattern syntax error with a byte offset.
type ParseError struct {
	Pos int
	Msg string
}

func (e *ParseError) Error() string { return fmt.Sprintf("syntax error at %d: %s", e.Pos, e.Msg) }

type parser struct {
	src   string
	pos   int
	flags Flags
	// branchGroups records the groups present in each top-level branch of the
	// nearest enclosing (?|...) reset group.
	groups    []string // 1-based; group 0 is the whole match
	names     map[string]int
	guardSeq  int
}

// Parse builds the AST for pattern using the pinned grammar.
func Parse(pattern string) (Node, *Info, error) {
	p := &parser{src: pattern, names: map[string]int{}, groups: []string{""}}
	n, err := p.parseAlt()
	if err != nil {
		return nil, nil, err
	}
	if p.pos != len(p.src) {
		return nil, nil, &ParseError{Pos: p.pos, Msg: "unexpected input"}
	}
	info := &Info{Flags: p.flags, Names: p.names, NumGroups: len(p.groups) - 1}
	return n, info, nil
}

// Info records parse-time metadata used by the compiler and engine.
type Info struct {
	Flags     Flags
	Names     map[string]int
	NumGroups int
}

func (p *parser) errf(at int, f string, a ...any) error {
	return &ParseError{Pos: at, Msg: fmt.Sprintf(f, a...)}
}

func (p *parser) parseAlt() (Node, error) {
	var branches []Node
	for {
		b, err := p.parseConcat()
		if err != nil {
			return nil, err
		}
		branches = append(branches, b)
		if p.peek() != '|' {
			break
		}
		p.pos++
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &Alt{Branches: branches}, nil
}

func (p *parser) parseConcat() (Node, error) {
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
		n, err = p.parseQuant(n)
		if err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	if len(items) == 0 {
		return &Concat{}, nil
	}
	if len(items) == 1 {
		return items[0], nil
	}
	return &Concat{Items: items}, nil
}

func (p *parser) parseQuant(child Node) (Node, error) {
	c := p.peek()
	if c != '*' && c != '+' && c != '?' && c != '{' {
		return child, nil
	}
	start := p.pos
	var q Quant
	switch c {
	case '*':
		p.pos++
		q.Min, q.Max = 0, -1
	case '+':
		p.pos++
		q.Min, q.Max = 1, -1
	case '?':
		p.pos++
		q.Min, q.Max = 0, 1
	case '{':
		lo, hi, ok, err := p.parseBrace()
		if err != nil {
			return nil, err
		}
		if !ok {
			return child, nil // '{' is a literal when not a valid quantifier
		}
		q.Min, q.Max = lo, hi
	}
	q.Greedy = true
	if p.peek() == '?' {
		p.pos++
		q.Greedy = false
	} else if p.peek() == '+' {
		p.pos++
		q.Possessive = true
	}
	q.Child = child
	q.Empty = canBeEmpty(child)
	q.Guard = p.nextGuard()
	if !q.Possessive && !canBeEmpty(child) {
		// Guard ids are still unique even when no zero-width check is needed;
		// the compiler omits the guard instructions for non-empty children.
	}
	_ = start
	return &q, nil
}

func (p *parser) parseBrace() (lo, hi int, ok bool, err error) {
	save := p.pos
	p.pos++ // '{'
	start := p.pos
	digits := func() (int, bool) {
		s := p.pos
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
		if p.pos == s {
			return 0, false
		}
		v, _ := strconv.Atoi(p.src[s:p.pos])
		return v, true
	}
	loV, hasLo := digits()
	hiV := loV
	hasHi := false
	if p.peek() == ',' {
		p.pos++
		hiV, hasHi = digits()
		if !hasHi {
			hiV = -1
			hasHi = true
		}
	} else {
		hasHi = true
	}
	if p.peek() != '}' || !hasLo {
		p.pos = save
		return 0, 0, false, nil
	}
	p.pos++
	if !hasHi {
		hiV = loV
	}
	if hiV != -1 && hiV < loV {
		return 0, 0, false, p.errf(start, "repeat range out of order")
	}
	return loV, hiV, true, nil
}

func (p *parser) parseAtom() (Node, error) {
	c := p.peek()
	switch c {
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '.':
		p.pos++
		return &CharClass{Negate: true, Terms: []ClassTerm{{Kind: ClassSingle, Lo: '\n', Hi: '\n'}}}, nil
	case '^':
		p.pos++
		return &Anchor{Kind: AnchorStartLine}, nil
	case '$':
		p.pos++
		return &Anchor{Kind: AnchorEndLine}, nil
	case '\\':
		return p.parseEscape()
	case '*', '+', '?', '}':
		return nil, p.errf(p.pos, "unexpected %q", string(rune(c)))
	case ')':
		return nil, p.errf(p.pos, "unexpected ')'")
	default:
		r, sz := p.decode()
		p.pos += sz
		return p.foldLiteral(&Literal{R: r}), nil
	}
}

func (p *parser) parseGroup() (Node, error) {
	start := p.pos
	p.pos++ // '('
	g := &Group{}
	if p.peek() == '?' {
		p.pos++
		switch p.peek() {
		case ':':
			p.pos++
		case '>':
			p.pos++
			body, err := p.parseUntilClose()
			if err != nil {
				return nil, err
			}
			return &Atomic{Body: body}, nil
		case '=':
			p.pos++
			body, err := p.parseUntilClose()
			if err != nil {
				return nil, err
			}
			return &Lookaround{Negative: false, Behind: false, Body: body}, nil
		case '!':
			p.pos++
			body, err := p.parseUntilClose()
			if err != nil {
				return nil, err
			}
			return &Lookaround{Negative: true, Behind: false, Body: body}, nil
		case '<':
			p.pos++
			switch p.peek() {
			case '=':
				p.pos++
				body, err := p.parseUntilClose()
				if err != nil {
					return nil, err
				}
				return &Lookaround{Negative: false, Behind: true, Body: body}, nil
			case '!':
				p.pos++
				body, err := p.parseUntilClose()
				if err != nil {
					return nil, err
				}
				return &Lookaround{Negative: true, Behind: true, Body: body}, nil
			}
			name, err := p.parseGroupName()
			if err != nil {
				return nil, err
			}
			g.Capture = true
			g.Name = name
		case '|':
			p.pos++
			g.IndexReset = true
			if p.peek() == ':' {
				p.pos++
			}
		default:
			set, local, err := p.parseFlagSet()
			if err != nil {
				return nil, err
			}
			if p.peek() == ':' {
				p.pos++
				g.Flags = &set
				if local {
					// flag scope handled by applyFlags wrapper below
				}
			} else if p.peek() != ')' {
				return nil, p.errf(p.pos, "expected ':' or ')'")
			}
		}
	}
	if g.Capture {
		g.Index = p.addGroup(g.Name, start)
	}
	prev := p.flags
	if g.Flags != nil {
		p.flags = mergeFlags(prev, *g.Flags)
	}
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if g.Flags != nil {
		p.flags = prev
	}
	if p.peek() != ')' {
		return nil, p.errf(p.pos, "missing ')'")
	}
	p.pos++
	g.Body = body
	if !g.Capture && g.Flags == nil && !g.IndexReset {
		return body, nil
	}
	return g, nil
}

func (p *parser) parseFlagSet() (Flags, bool, error) {
	// Parsed after "(?". local is true for the scoped form "(?i:...)".
	var f Flags
	on := true
	saw := false
	for {
		c := p.peek()
		switch c {
		case 'i', 'm', 's':
			setFlag(&f, c, on)
			saw = true
			p.pos++
		case '-':
			on = false
			p.pos++
		case ':', ')':
			local := c == ':'
			if !saw {
				return Flags{}, false, p.errf(p.pos, "empty flag group")
			}
			return f, local, nil
		default:
			return Flags{}, false, p.errf(p.pos, "bad flag %q", string(rune(c)))
		}
	}
}

func setFlag(f *Flags, c rune, on bool) {
	b := f
	switch c {
	case 'i':
		b.I = on
	case 'm':
		b.M = on
	case 's':
		b.S = on
	}
	*f = b
}

func (p *parser) parseUntilClose() (Node, error) {
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if p.peek() != ')' {
		return nil, p.errf(p.pos, "missing ')'")
	}
	p.pos++
	return body, nil
}

func (p *parser) parseGroupName() (string, error) {
	// Assumes current char is the first name char after "(?<".
	s := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != '>' {
		if p.src[p.pos] == '(' || p.src[p.pos] == ')' {
			return "", p.errf(p.pos, "bad group name")
		}
		p.pos++
	}
	name := p.src[s:p.pos]
	if name == "" || p.peek() != '>' {
		return "", p.errf(p.pos, "bad group name")
	}
	p.pos++
	return name, nil
}

func (p *parser) addGroup(name string, pos int) int {
	idx := len(p.groups)
	p.groups = append(p.groups, name)
	if name != "" {
		if _, dup := p.names[name]; dup {
			_ = pos
		}
		p.names[name] = idx
	}
	return idx
}

func (p *parser) foldLiteral(n Node) Node { return n }

// decode reads one code point at p.pos. Invalid UTF-8 bytes decode to
// utf8.RuneError with size 1; such units are distinguishable inside the engine
// via Unit.Bad.
func (p *parser) decode() (r rune, sz int) {
	return decodeRune(p.src, p.pos)
}

func (p *parser) peek() int {
	if p.pos >= len(p.src) {
		return -1
	}
	return int(p.src[p.pos])
}

func (p *parser) nextGuard() int {
	p.guardSeq++
	return p.guardSeq
}

func (p *parser) parseEscape() (Node, error) {
	start := p.pos
	p.pos++ // backslash
	if p.pos >= len(p.src) {
		return nil, p.errf(start, "trailing backslash")
	}
	c := p.src[p.pos]
	switch c {
	case 'd', 'D', 'w', 'W', 's', 'S':
		p.pos++
		kind := map[byte]ClassTermKind{'d': ClassDigit, 'D': ClassNotDigit, 'w': ClassWord, 'W': ClassNotWord, 's': ClassSpace, 'S': ClassNotSpace}[c]
		return &CharClass{Terms: []ClassTerm{{Kind: kind, ASCII: true}}}, nil
	case 'p', 'P':
		p.pos++
		neg := c == 'P'
		name, err := p.parseUnicodeProp()
		if err != nil {
			return nil, err
		}
		kind := ClassUnicode
		if neg {
			kind = ClassNotUnicode
		}
		return &CharClass{Terms: []ClassTerm{{Kind: kind, Unicode: name}}}, nil
	case 'b':
		p.pos++
		return &Anchor{Kind: AnchorWordBoundary}, nil
	case 'B':
		p.pos++
		return &Anchor{Kind: AnchorNotWordBoundary}, nil
	case 'A':
		p.pos++
		return &Anchor{Kind: AnchorStartText}, nil
	case 'z':
		p.pos++
		return &Anchor{Kind: AnchorEndText}, nil
	case 'Z':
		p.pos++
		return &Anchor{Kind: AnchorEndTextOptNL}, nil
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return p.parseBackref(start)
	case 'k':
		p.pos++
		if p.peek() != '<' {
			return nil, p.errf(p.pos, "expected < after \\k")
		}
		p.pos++
		name, err := p.parseGroupName()
		if err != nil {
			return nil, err
		}
		return &Ref{Name: name}, nil
	case 'x':
		p.pos++
		r, err := p.parseHexEscape()
		if err != nil {
			return nil, err
		}
		return &Literal{R: r}, nil
	case 'u':
		p.pos++
		r, err := p.parseUNEscape()
		if err != nil {
			return nil, err
		}
		return &Literal{R: r}, nil
	case 'a':
		p.pos++
		return &Literal{R: '\a'}, nil
	case 'f':
		p.pos++
		return &Literal{R: '\f'}, nil
	case 'n':
		p.pos++
		return &Literal{R: '\n'}, nil
	case 'r':
		p.pos++
		return &Literal{R: '\r'}, nil
	case 't':
		p.pos++
		return &Literal{R: '\t'}, nil
	case 'v':
		p.pos++
		return &Literal{R: '\v'}, nil
	default:
		r, sz := p.decode()
		p.pos += sz
		return &Literal{R: r}, nil
	}
}

func (p *parser) parseBackref(start int) (Node, error) {
	s := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	n, _ := strconv.Atoi(p.src[s:p.pos])
	if n == 0 {
		return nil, p.errf(start, `\0 not supported; use \x00`)
	}
	return &Ref{Index: n}, nil
}

func (p *parser) parseHexEscape() (rune, error) {
	var sb strings.Builder
	for i := 0; i < 2; i++ {
		if !isHex(byte(p.peek())) {
			return 0, p.errf(p.pos, "bad \\x escape")
		}
		sb.WriteByte(p.src[p.pos])
		p.pos++
	}
	v, _ := strconv.ParseInt(sb.String(), 16, 32)
	return rune(v), nil
}

func (p *parser) parseUNEscape() (rune, error) {
	if p.peek() == '{' {
		p.pos++
		s := p.pos
		for p.peek() != '}' && p.peek() != -1 {
			p.pos++
		}
		if p.peek() != '}' {
			return 0, p.errf(p.pos, "missing } in \\u escape")
		}
		hex := p.src[s:p.pos]
		p.pos++
		v, err := strconv.ParseInt(hex, 16, 32)
		if err != nil || v < 0 || v > 0x10FFFF {
			return 0, p.errf(s, "bad \\u code point")
		}
		return rune(v), nil
	}
	var sb strings.Builder
	for i := 0; i < 4; i++ {
		if !isHex(byte(p.peek())) {
			return 0, p.errf(p.pos, "bad \\u escape")
		}
		sb.WriteByte(p.src[p.pos])
		p.pos++
	}
	v, _ := strconv.ParseInt(sb.String(), 16, 32)
	return rune(v), nil
}

func (p *parser) parseUnicodeProp() (string, error) {
	if p.peek() != '{' {
		return "", p.errf(p.pos, "property must be braced: \\p{...}")
	}
	p.pos++
	s := p.pos
	for p.peek() != '}' && p.peek() != -1 {
		p.pos++
	}
	if p.peek() != '}' {
		return "", p.errf(p.pos, "missing } in property")
	}
	name := strings.TrimSpace(p.src[s:p.pos])
	p.pos++
	if name == "" {
		return "", p.errf(s, "empty property")
	}
	return name, nil
}

func isHex(b int) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

func (p *parser) parseClass() (Node, error) {
	start := p.pos
	p.pos++ // '['
	cc := &CharClass{}
	if p.peek() == '^' {
		cc.Negate = true
		p.pos++
	}
	if p.peek() == ']' {
		// A leading ] is a literal member.
		r, sz := p.decode()
		p.pos += sz
		cc.Terms = append(cc.Terms, ClassTerm{Kind: ClassSingle, Lo: r, Hi: r})
	}
	for p.peek() != ']' && p.peek() != -1 {
		t, err := p.parseClassTerm()
		if err != nil {
			return nil, err
		}
		if p.peek() == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
			p.pos++ // '-'
			hiT, err := p.parseClassTerm()
			if err != nil || hiT.Kind != ClassSingle {
				return nil, p.errf(start, "bad character range")
			}
			if t.Kind != ClassSingle {
				return nil, p.errf(start, "range start must be a literal")
			}
			if t.Lo > hiT.Lo {
				return nil, p.errf(start, "range out of order")
			}
			t.Kind, t.Hi = ClassRange, hiT.Lo
		}
		cc.Terms = append(cc.Terms, t)
	}
	if p.peek() != ']' {
		return nil, p.errf(start, "unterminated character class")
	}
	p.pos++
	return cc, nil
}

func (p *parser) parseClassTerm() (ClassTerm, error) {
	c := p.peek()
	if c == '\\' {
		p.pos++
		if p.pos >= len(p.src) {
			return ClassTerm{}, p.errf(p.pos, "trailing backslash")
		}
		esc := p.src[p.pos]
		switch esc {
		case 'd', 'D', 'w', 'W', 's', 'S':
			p.pos++
			kind := map[byte]ClassTermKind{'d': ClassDigit, 'D': ClassNotDigit, 'w': ClassWord, 'W': ClassNotWord, 's': ClassSpace, 'S': ClassNotSpace}[esc]
			return ClassTerm{Kind: kind, ASCII: true}, nil
		case 'p', 'P':
			p.pos++
			neg := esc == 'P'
			name, err := p.parseUnicodeProp()
			if err != nil {
				return ClassTerm{}, err
			}
			kind := ClassUnicode
			if neg {
				kind = ClassNotUnicode
			}
			return ClassTerm{Kind: kind, Unicode: name}, nil
		case 'n', 'r', 't', 'f', 'v', 'a', '0', 'x', 'u':
			// Reuse atom escape by temporarily rewinding.
			p.pos--
			n, err := p.parseEscape()
			if err != nil {
				return ClassTerm{}, err
			}
			lit, ok := n.(*Literal)
			if !ok {
				return ClassTerm{}, p.errf(p.pos, "unsupported escape in class")
			}
			return ClassTerm{Kind: ClassSingle, Lo: lit.R, Hi: lit.R}, nil
		default:
			r, sz := p.decode()
			p.pos += sz
			return ClassTerm{Kind: ClassSingle, Lo: r, Hi: r}, nil
		}
	}
	r, sz := p.decode()
	p.pos += sz
	return ClassTerm{Kind: ClassSingle, Lo: r, Hi: r}, nil
}

func decodeRune(s string, i int) (rune, int) {
	b := s[i]
	switch {
	case b < 0x80:
		return rune(b), 1
	case b < 0xC2:
		return 0xFFFD, 1
	}
	r := rune(b & 0x1F)
	need := 0
	switch {
	case b < 0xE0:
		need = 1
	case b < 0xF0:
		need = 2
	default:
		need = 3
	}
	if i+need >= len(s) {
		return 0xFFFD, 1
	}
	for k := 1; k <= need; k++ {
		c := s[i+k]
		if c&0xC0 != 0x80 {
			return 0xFFFD, 1
		}
		r = r<<6 | rune(c&0x3F)
	}
	if need == 2 && r < 0x800 || need == 3 && (r < 0x10000 || r > 0x10FFFF) {
		return 0xFFFD, 1
	}
	return r, need + 1
}

// canBeEmpty reports whether node can accept the empty string. Used to decide
// which quantifiers need zero-width iteration guards.
func canBeEmpty(n Node) bool {
	switch x := n.(type) {
	case *Literal, *CharClass, *Anchor, *Ref, *Lookaround:
		return false
	case *Concat:
		for _, it := range x.Items {
			if !canBeEmpty(it) {
				return false
			}
		}
		return true
	case *Alt:
		for _, b := range x.Branches {
			if canBeEmpty(b) {
				return true
			}
		}
		return false
	case *Group:
		return canBeEmpty(x.Body)
	case *Atomic:
		return canBeEmpty(x.Body)
	case *Quant:
		return x.Min == 0 || canBeEmpty(x.Child)
	}
	return false
}

func mergeFlags(base, over Flags) Flags {
	return Flags{I: over.I || base.I, M: over.M || base.M, S: over.S || base.S}
}
