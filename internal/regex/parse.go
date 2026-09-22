package regex

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ParseError points at a UTF-8 byte offset inside the pattern.
type ParseError struct {
	Msg  string
	Pos  int
	Near string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("syntax error at byte %d near %q: %s", e.Pos, e.Near, e.Msg)
}

type parser struct {
	src    string
	rs     []rune
	off    []int // rune index -> byte offset
	p      int
	groups int
	flags  Flags
	names  map[string]int
}

// Parse compiles the pattern into an AST, assigning capture indices and
// validating back references / duplicate names. The returned ProgramMeta
// records capture names in index order.
func Parse(pattern string, flags string) (*ProgramMeta, Node, error) {
	if !utf8.ValidString(pattern) {
		return nil, nil, &ParseError{Msg: "pattern is not valid UTF-8", Pos: 0, Near: pattern}
	}
	p := &parser{src: pattern, names: map[string]int{}}
	p.rs = []rune(pattern)
	p.off = make([]int, len(p.rs)+1)
	for i, r := range p.rs {
		p.off[i] = len(string(p.rs[:i]))
		_ = r
	}
	p.off[len(p.rs)] = len(pattern)
	fl, err := parseFlagString(flags, true)
	if err != nil {
		return nil, nil, err
	}
	p.flags = fl
	node, err := p.parseAlt(p.flags)
	if err != nil {
		return nil, nil, err
	}
	if p.p < len(p.rs) {
		return nil, nil, p.errf("unexpected %q", p.rs[p.p])
	}
	meta := &ProgramMeta{
		Pattern:    pattern,
		Flags:      flags,
		NumGroups:  p.groups,
		Names:      map[string]int{},
		GroupNames: make([]string, p.groups+1),
		SyntaxVer:  SyntaxVersion,
		UnicodeVer: UnicodeVersion,
	}
	for n, i := range p.names {
		meta.Names[n] = i
		meta.GroupNames[i] = n
	}
	return meta, node, nil
}

func parseFlagString(s string, allowU bool) (Flags, error) {
	var fl Flags
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case 'i', 'm', 's':
			fl = applyFlags(fl, string(s[i]), "")
		case 'u':
			if !allowU {
				return fl, &ParseError{Msg: "flag u is not allowed here", Pos: i, Near: s}
			}
			fl.U = true
		case '-':
		case ' ':
		default:
			return fl, &ParseError{Msg: fmt.Sprintf("unsupported flag %q", s[i]), Pos: i, Near: s}
		}
	}
	return fl, nil
}

func (p *parser) errf(format string, args ...any) error {
	near := ""
	if p.p < len(p.rs) {
		near = string(p.rs[p.p:min(p.p+8, len(p.rs))])
	}
	pos := 0
	if p.p < len(p.off) {
		pos = p.off[p.p]
	}
	return &ParseError{Msg: fmt.Sprintf(format, args...), Pos: pos, Near: near}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (p *parser) parseAlt(fl Flags) (Node, error) {
	var branches []Node
	for {
		b, err := p.parseConcat(fl)
		if err != nil {
			return nil, err
		}
		branches = append(branches, b)
		if p.peek() != '|' {
			break
		}
		p.advance()
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &Alt{Branches: branches}, nil
}

func (p *parser) parseConcat(fl Flags) (Node, error) {
	var items []Node
	for {
		c := p.peek()
		if c == 0 || c == '|' || c == ')' {
			break
		}
		atom, nf, err := p.parseAtom(fl)
		if err != nil {
			return nil, err
		}
		fl = nf
		atom, err = p.parseQuantifier(atom)
		if err != nil {
			return nil, err
		}
		items = append(items, atom)
	}
	if len(items) == 0 {
		return &Empty{}, nil
	}
	if len(items) == 1 {
		return items[0], nil
	}
	return &Concat{Items: items}, nil
}

func (p *parser) parseQuantifier(node Node) (Node, error) {
	c := p.peek()
	if c != '*' && c != '+' && c != '?' && c != '{' {
		return node, nil
	}
	var mn, mx int
	switch c {
	case '*':
		mn, mx = 0, -1
		p.advance()
	case '+':
		mn, mx = 1, -1
		p.advance()
	case '?':
		mn, mx = 0, 1
		p.advance()
	case '{':
		saved := p.p
		v, ok, err := p.tryBraceQuant()
		if err != nil {
			return nil, err
		}
		if !ok {
			p.p = saved
			return node, nil
		}
		mn, mx = v.mn, v.mx
	}
	greedy, poss := true, false
	if p.peek() == '?' {
		greedy = false
		p.advance()
	} else if p.peek() == '+' {
		poss = true
		p.advance()
	}
	switch n := node.(type) {
	case *Anchor:
		return nil, p.errf("quantifier not allowed after anchor")
	case *Backref:
	default:
		_ = n
	}
	return &Repeat{Child: node, Min: mn, Max: mx, Greedy: greedy, Possessive: poss}, nil
}

type braceSpec struct {
	mn, mx int
	hasMax bool
}

func (p *parser) tryBraceQuant() (braceSpec, bool, error) {
	start := p.p
	if p.peek() != '{' {
		return braceSpec{}, false, nil
	}
	p.advance()
	digits := func() (int, bool) {
		if p.peek() < '0' || p.peek() > '9' {
			return 0, false
		}
		v := 0
		for c := p.peek(); c >= '0' && c <= '9'; c = p.peek() {
			v = v*10 + int(c-'0')
			p.advance()
		}
		return v, true
	}
	a, ok1 := digits()
	if !ok1 {
		p.p = start
		return braceSpec{}, false, nil
	}
	b, ok2 := a, true
	hasMax := false
	if p.peek() == ',' {
		p.advance()
		if c := p.peek(); c >= '0' && c <= '9' {
			b, _ = digits()
		} else {
			b = -1
		}
		hasMax = true
	}
	if p.peek() != '}' {
		p.p = start
		return braceSpec{}, false, nil
	}
	p.advance()
	if hasMax && b != -1 && b < a {
		return braceSpec{}, false, p.errf("quantifier range %d > %d", a, b)
	}
	if !ok2 {
		b = a
	}
	return braceSpec{mn: a, mx: b, hasMax: hasMax || !ok2}, true, nil
}

func (p *parser) parseAtom(fl Flags) (Node, Flags, error) {
	c := p.peek()
	switch c {
	case '(':
		return p.parseGroup(fl)
	case '[':
		cl, err := p.parseClass(fl)
		return cl, fl, err
	case '.':
		p.advance()
		return &Char{Dot: true, Code: -1}, fl, nil
	case '^':
		p.advance()
		return &Anchor{Kind: AnchorSOL}, fl, nil
	case '$':
		p.advance()
		return &Anchor{Kind: AnchorEOL}, fl, nil
	case '\\':
		return p.parseEscape(fl)
	case '*', '+', '?', '}':
		return nil, fl, p.errf("quantifier %q with nothing to repeat", string(c))
	case ')':
		return nil, fl, p.errf("unmatched ')'")
	case 0:
		return nil, fl, p.errf("unexpected end of pattern")
	default:
		p.advance()
		return &Char{Code: c}, fl, nil
	}
}

func (p *parser) parseGroup(fl Flags) (Node, Flags, error) {
	p.advance() // '('
	kind := GroupCapture
	var addFlags, delFlags, name string
	if p.peek() == '?' {
		p.advance()
		q := p.peek()
		switch q {
		case ':':
			p.advance()
			kind = GroupNonCap
		case '>':
			p.advance()
			kind = GroupAtomic
		case '=':
			p.advance()
			kind = GroupLookAhead
		case '!':
			p.advance()
			kind = GroupNegLookAhead
		case '<':
			p.advance()
			switch p.peek() {
			case '=':
				p.advance()
				kind = GroupLookBehind
			case '!':
				p.advance()
				kind = GroupNegLookBehind
			default:
				nm, err := p.parseGroupName()
				if err != nil {
					return nil, fl, err
				}
				name = nm
				kind = GroupCapture
			}
		default:
			add, del, only, err := p.parseInlineFlags()
			if err != nil {
				return nil, fl, err
			}
			fl = applyFlags(fl, add, del)
			addFlags, delFlags = add, del
			if only {
				if p.peek() != ')' {
					return nil, fl, p.errf("expected ')' after flags")
				}
				p.advance()
				kind = GroupFlags
				return &Group{Kind: GroupFlags, AddFlags: addFlags, DelFlags: delFlags}, fl, nil
			}
			kind = GroupNonCap
		}
	}
	idx := 0
	if kind == GroupCapture {
		p.groups++
		idx = p.groups
		if name != "" {
			if old, ok := p.names[name]; ok {
				return nil, fl, p.errf("duplicate group name %q (already group %d)", name, old)
			}
			p.names[name] = idx
		}
	}
	innerFlags := fl
	if addFlags != "" || delFlags != "" {
		innerFlags = applyFlags(fl, addFlags, delFlags)
	}
	body, err := p.parseAlt(innerFlags)
	if err != nil {
		return nil, fl, err
	}
	if p.peek() != ')' {
		return nil, fl, p.errf("unclosed group, expected ')'")
	}
	p.advance()
	return &Group{Child: body, Kind: kind, Index: idx, Name: name, AddFlags: addFlags, DelFlags: delFlags}, fl, nil
}

func (p *parser) parseGroupName() (string, error) {
	var b strings.Builder
	for {
		c := p.peek()
		if c == '>' {
			p.advance()
			break
		}
		if c == 0 {
			return "", p.errf("unterminated group name")
		}
		if b.Len() == 0 && !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_') {
			return "", p.errf("invalid group name start %q", string(c))
		}
		b.WriteRune(c)
		p.advance()
	}
	if b.Len() == 0 {
		return "", p.errf("empty group name")
	}
	return b.String(), nil
}

func (p *parser) parseInlineFlags() (add, del string, only bool, err error) {
	section := 0
	var sb [2]strings.Builder
	for {
		c := p.peek()
		if c == ')' {
			break
		}
		if c == ':' {
			if section == 1 {
				return "", "", false, p.errf("multiple '-' in flags")
			}
			p.advance()
			return sb[0].String(), sb[1].String(), false, nil
		}
		if c == '-' {
			section = 1
			p.advance()
			continue
		}
		if c == 0 {
			return "", "", false, p.errf("unterminated flags group")
		}
		if c != 'i' && c != 'm' && c != 's' && c != 'u' {
			return "", "", false, p.errf("unsupported inline flag %q", string(c))
		}
		sb[section].WriteRune(c)
		p.advance()
	}
	return sb[0].String(), sb[1].String(), true, nil
}

func (p *parser) peek() rune {
	if p.p >= len(p.rs) {
		return 0
	}
	return p.rs[p.p]
}

func (p *parser) advance() rune {
	r := p.peek()
	if r != 0 {
		p.p++
	}
	return r
}

// parseEscape handles a backslash outside character classes.
func (p *parser) parseEscape(fl Flags) (Node, Flags, error) {
	p.advance() // backslash
	c := p.peek()
	if c == 0 {
		return nil, fl, p.errf("trailing backslash")
	}
	switch c {
	case 'd', 'D':
		p.advance()
		return p.shorthandClass('d', c == 'D', fl), fl, nil
	case 'w', 'W':
		p.advance()
		return p.shorthandClass('w', c == 'W', fl), fl, nil
	case 's', 'S':
		p.advance()
		return p.shorthandClass('s', c == 'S', fl), fl, nil
	case 'b':
		p.advance()
		return &Anchor{Kind: AnchorWordB}, fl, nil
	case 'B':
		p.advance()
		return &Anchor{Kind: AnchorNotWordB}, fl, nil
	case 'A':
		p.advance()
		return &Anchor{Kind: AnchorStart}, fl, nil
	case 'z':
		p.advance()
		return &Anchor{Kind: AnchorEnd}, fl, nil
	case 'Z':
		p.advance()
		return &Anchor{Kind: AnchorEndNL}, fl, nil
	case 'k':
		p.advance()
		if p.peek() != '<' {
			return nil, fl, p.errf("expected < after \\k")
		}
		p.advance()
		var b strings.Builder
		for p.peek() != '>' {
			if p.peek() == 0 {
				return nil, fl, p.errf("unterminated \\k<...>")
			}
			b.WriteRune(p.advance())
		}
		p.advance()
		return &Backref{Name: b.String()}, fl, nil
	case '0':
		r, err := p.parseHexEscape(0)
		if err != nil {
			return nil, fl, err
		}
		return &Char{Code: r}, fl, nil
	case 'x':
		p.advance()
		r, err := p.parseHexBrace()
		if err != nil {
			return nil, fl, err
		}
		return &Char{Code: r}, fl, nil
	case 'u':
		p.advance()
		r, err := p.parseHexFixed(4)
		if err != nil {
			return nil, fl, err
		}
		return &Char{Code: r}, fl, nil
	case 'a', 'f', 'n', 'r', 't', 'v', 'e':
		p.advance()
		return &Char{Code: simpleEsc(c)}, fl, nil
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		var n int
		for c := p.peek(); c >= '0' && c <= '9'; c = p.peek() {
			n = n*10 + int(c-'0')
			p.advance()
		}
		return &Backref{Ref: n}, fl, nil
	default:
		// Identity escape: punctuation is literal; letters are errors
		// unless documented.
		p.advance()
		if isAlpha(c) {
			return nil, fl, p.errf("unknown escape \\%s", string(c))
		}
		return &Char{Code: c}, fl, nil
	}
}

func (p *parser) parseHexEscape(_ int) (rune, error) {
	// called after seeing \0: consume '0', then two optional octal digits
	p.advance()
	v := 0
	for k := 0; k < 2; k++ {
		c := p.peek()
		if c < '0' || c > '7' {
			break
		}
		v = v*8 + int(c-'0')
		p.advance()
	}
	return rune(v), nil
}

func (p *parser) parseHexBrace() (rune, error) {
	if p.peek() == '{' {
		p.advance()
		var v int64
		n := 0
		for {
			c := p.peek()
			if c == '}' {
				p.advance()
				break
			}
			if !isHex(c) {
				return 0, p.errf("bad \\x{...} escape")
			}
			v = v*16 + int64(hexVal(c))
			n++
			p.advance()
			if n > 6 || v > 0x10FFFF {
				return 0, p.errf("\\x value out of range")
			}
		}
		return rune(v), nil
	}
	return p.parseHexFixed(2)
}

func (p *parser) parseHexFixed(n int) (rune, error) {
	var v int64
	for i := 0; i < n; i++ {
		c := p.peek()
		if !isHex(c) {
			return 0, p.errf("expected %d hex digits", n)
		}
		v = v*16 + int64(hexVal(c))
		p.advance()
	}
	return rune(v), nil
}

func simpleEsc(c rune) rune {
	switch c {
	case 'a':
		return 0x07
	case 'f':
		return 0x0C
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	case 'v':
		return 0x0B
	case 'e':
		return 0x1B
	}
	return c
}

func isAlpha(c rune) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isHex(c rune) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
func hexVal(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}

var _ = strconv.Itoa

func (p *parser) shorthandClass(which byte, neg bool, fl Flags) Node {
	mk := func(terms ...ClassTerm) Node {
		if neg {
			return &Class{Negated: true, Terms: terms}
		}
		return &Class{Terms: terms}
	}
	rangeTerm := func(lo, hi rune) ClassTerm {
		return ClassTerm{Lo: lo, Hi: hi, Kind: TermRange}
	}
	switch which {
	case 'd':
		if fl.U {
			if neg {
				return &Class{Negated: true, Terms: []ClassTerm{{Kind: TermProp, Prop: "nd"}}}
			}
			return &Class{Terms: []ClassTerm{{Kind: TermProp, Prop: "nd"}}}
		}
		return mk(rangeTerm('0', '9'))
	case 'w':
		if fl.U {
			t := []ClassTerm{{Kind: TermProp, Prop: "l"}, {Kind: TermProp, Prop: "n"}, rangeTerm('_', '_')}
			return &Class{Negated: neg, Terms: t}
		}
		return mk(rangeTerm('a', 'z'), rangeTerm('A', 'Z'), rangeTerm('0', '9'), rangeTerm('_', '_'))
	case 's':
		if fl.U {
			if neg {
				return &Class{Negated: true, Terms: []ClassTerm{{Kind: TermProp, Prop: "whitespace"}}}
			}
			return mk(ClassTerm{Kind: TermProp, Prop: "whitespace"})
		}
		return mk(rangeTerm('\t', '\r'), rangeTerm(' ', ' '))
	}
	return &Empty{}
}

func (p *parser) parseClass(fl Flags) (Node, error) {
	p.advance() // '['
	cl := &Class{}
	if p.peek() == '^' {
		cl.Negated = true
		p.advance()
	}
	first := true
	for {
		c := p.peek()
		if c == 0 {
			return nil, p.errf("unterminated character class")
		}
		if c == ']' && !first {
			p.advance()
			break
		}
		first = false
		t, err := p.parseClassTerm(fl)
		if err != nil {
			return nil, err
		}
		if p.peek() == '-' && p.p+1 < len(p.rs) && p.rs[p.p+1] != ']' {
			p.advance() // '-'
			hi, err := p.parseClassAtom(fl)
			if err != nil {
				return nil, err
			}
			if hi.Kind != TermRange || t.Kind != TermRange {
				return nil, p.errf("range endpoints must be literal characters")
			}
			if hi.Lo < t.Lo {
				return nil, p.errf("character range out of order: %U-%U", t.Lo, hi.Lo)
			}
			t.Hi = hi.Lo
		}
		cl.Terms = append(cl.Terms, t)
	}
	return cl, nil
}

func (p *parser) parseClassTerm(fl Flags) (ClassTerm, error) {
	c := p.peek()
	if c == '\\' {
		p.advance()
		esc := p.peek()
		switch esc {
		case 'd', 'D', 'w', 'W', 's', 'S':
			p.advance()
			node := p.shorthandClass(byte(esc), esc == 'D' || esc == 'W' || esc == 'S', fl).(*Class)
			if len(node.Terms) == 1 {
				return node.Terms[0], nil
			}
			return ClassTerm{}, p.errf("composite shorthand cannot be a range endpoint")
		case 'p', 'P':
			return p.parseUnicodeProp(esc == 'P')
		case 'x':
			p.advance()
			r, err := p.parseHexBrace()
			if err != nil {
				return ClassTerm{}, err
			}
			return ClassTerm{Lo: r, Hi: r, Kind: TermRange}, nil
		case 'u':
			p.advance()
			r, err := p.parseHexFixed(4)
			if err != nil {
				return ClassTerm{}, err
			}
			return ClassTerm{Lo: r, Hi: r, Kind: TermRange}, nil
		case '0':
			r, err := p.parseHexEscape(0)
			if err != nil {
				return ClassTerm{}, err
			}
			return ClassTerm{Lo: r, Hi: r, Kind: TermRange}, nil
		case 'a', 'f', 'n', 'r', 't', 'v', 'e':
			p.advance()
			r := simpleEsc(esc)
			return ClassTerm{Lo: r, Hi: r, Kind: TermRange}, nil
		case 'b':
			p.advance()
			return ClassTerm{Lo: 0x08, Hi: 0x08, Kind: TermRange}, nil
		default:
			p.advance()
			if isAlpha(esc) {
				return ClassTerm{}, p.errf("unknown escape \\%s in class", string(esc))
			}
			return ClassTerm{Lo: esc, Hi: esc, Kind: TermRange}, nil
		}
	}
	return p.parseClassAtom(fl)
}

func (p *parser) parseClassAtom(fl Flags) (ClassTerm, error) {
	c := p.peek()
	if c == '\\' {
		return p.parseClassTerm(fl)
	}
	if c == ']' || c == 0 {
		return ClassTerm{}, p.errf("bad range endpoint")
	}
	p.advance()
	return ClassTerm{Lo: c, Hi: c, Kind: TermRange}, nil
}

func (p *parser) parseUnicodeProp(neg bool) (ClassTerm, error) {
	p.advance() // p/P
	if p.peek() != '{' {
		return ClassTerm{}, p.errf("expected { after \\p")
	}
	p.advance()
	var b strings.Builder
	for p.peek() != '}' {
		if p.peek() == 0 {
			return ClassTerm{}, p.errf("unterminated \\p{...}")
		}
		b.WriteRune(p.advance())
	}
	p.advance()
	name := canonProp(b.String())
	term := ClassTerm{Kind: TermProp, Prop: name}
	if neg {
		// Negated property is represented as class-level negation around
		// a single prop term; caller wraps it. Encode with sentinel prefix.
		term.Prop = "^" + name
	}
	return term, nil
}

// canonProp normalizes \p names: lowercase, drop underscores/spaces,
// accept common aliases.
func canonProp(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, " ", "")
	if i := strings.Index(s, "="); i >= 0 {
		head := s[:i]
		val := s[i+1:]
		if head == "sc" {
			return val
		}
		return val
	}
	aliases := map[string]string{
		"any":      "any",
		"assigned": "assigned",
		"xan":      "alnum",
		"xsp":      "space",
		"xwd":      "word",
		"xd":       "digit",
		"blank":    "blank",
	}
	if v, ok := aliases[s]; ok {
		return v
	}
	return s
}
