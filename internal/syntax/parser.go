package syntax

import (
	"fmt"
	"strconv"
	"unicode/utf8"
)

const maxRepeatCount = 10000

// Error describes a parse failure at a byte offset.
type Error struct {
	Pos int
	Msg string
}

func (e *Error) Error() string {
	return fmt.Sprintf("syntax error at byte %d: %s", e.Pos, e.Msg)
}

type parser struct {
	src      string
	pos      int
	groups   int
	names    map[string]int
	maxBack  int
	backrefs []*Node // resolved in a post-pass
}

// Parse parses pattern into an AST, assigning capture indices and node IDs.
func Parse(pattern string) (*Pattern, error) {
	p := &parser{src: pattern, names: map[string]int{}}
	root, err := p.parseSeq(0, true)
	if err != nil {
		return nil, err
	}
	if p.pos != len(pattern) {
		return nil, &Error{Pos: p.pos, Msg: "unexpected input"}
	}
	if err := p.resolveBackrefs(root); err != nil {
		return nil, err
	}
	if err := checkLookbehind(root); err != nil {
		return nil, err
	}
	id := 0
	assignIDs(root, &id)
	return &Pattern{
		Root:       root,
		NumGroups:  p.groups,
		Names:      p.names,
		MaxBackref: p.maxBack,
	}, nil
}

func (p *parser) failf(at int, format string, args ...any) error {
	return &Error{Pos: at, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) peek() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

// parseSeq parses an alternation up to ')' or end.
func (p *parser) parseSeq(depth int, top bool) (*Node, error) {
	var branches []*Node
	start := p.pos
	for {
		node, err := p.parseConcat(depth + 1)
		if err != nil {
			return nil, err
		}
		branches = append(branches, node)
		if p.peek() != '|' {
			break
		}
		p.pos++
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &Node{Kind: KAlt, Pos: start, End: p.pos, Children: branches}, nil
}

func (p *parser) parseConcat(depth int) (*Node, error) {
	if depth > 200 {
		return nil, p.failf(p.pos, "group nesting too deep")
	}
	start := p.pos
	var items []*Node
	for {
		c := p.peek()
		if c == 0 || c == '|' || c == ')' {
			break
		}
		atom, err := p.parseAtom(depth)
		if err != nil {
			return nil, err
		}
		atom, err = p.parseQuantifier(atom)
		if err != nil {
			return nil, err
		}
		items = append(items, atom)
	}
	return flattenCat(start, p.pos, items), nil
}

func flattenCat(start, end int, items []*Node) *Node {
	filtered := items[:0]
	for _, n := range items {
		if n.Kind != KEmpty {
			filtered = append(filtered, n)
		}
	}
	if len(filtered) == 0 {
		return &Node{Kind: KEmpty, Pos: start, End: end}
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return &Node{Kind: KCat, Pos: start, End: end, Children: filtered}
}

func (p *parser) parseQuantifier(atom *Node) (*Node, error) {
	c := p.peek()
	var min, max int
	var hasRange bool
	qpos := p.pos
	switch c {
	case '*':
		min, max, hasRange = 0, -1, true
		p.pos++
	case '+':
		min, max, hasRange = 1, -1, true
		p.pos++
	case '?':
		min, max, hasRange = 0, 1, true
		p.pos++
	case '{':
		lo, hi, ok, newPos := p.tryBrace(p.pos)
		if ok {
			min, max, hasRange = lo, hi, true
			p.pos = newPos
		}
	}
	if !hasRange {
		return atom, nil
	}
	if atom.Kind == KAnchor {
		return nil, p.failf(qpos, "quantifier not allowed after anchor %s", atom.Anchor)
	}
	lazy := false
	if p.peek() == '?' {
		lazy = true
		p.pos++
	}
	if min > maxRepeatCount || (max >= 0 && max > maxRepeatCount) {
		return nil, p.failf(qpos, "repeat count exceeds %d", maxRepeatCount)
	}
	if max >= 0 && max < min {
		return nil, p.failf(qpos, "quantifier min > max")
	}
	return &Node{
		Kind: KRepeat, Pos: atom.Pos, End: p.pos,
		Children: []*Node{atom}, Min: min, Max: max, Lazy: lazy,
	}, nil
}

// tryBrace attempts to parse {n}, {n,}, {n,m}. ok=false means a literal '{'.
func (p *parser) tryBrace(pos int) (min, max int, ok bool, newPos int) {
	i := pos + 1
	startNum := i
	for i < len(p.src) && p.src[i] >= '0' && p.src[i] <= '9' {
		i++
	}
	if i == startNum {
		return 0, 0, false, pos
	}
	n1, _ := strconv.Atoi(p.src[startNum:i])
	if i >= len(p.src) {
		return 0, 0, false, pos
	}
	switch p.src[i] {
	case '}':
		return n1, n1, true, i + 1
	case ',':
		i++
		j := i
		for i < len(p.src) && p.src[i] >= '0' && p.src[i] <= '9' {
			i++
		}
		if i < len(p.src) && p.src[i] == '}' {
			if i == j {
				return n1, -1, true, i + 1
			}
			n2, _ := strconv.Atoi(p.src[j:i])
			return n1, n2, true, i + 1
		}
	}
	return 0, 0, false, pos
}

func (p *parser) parseAtom(depth int) (*Node, error) {
	start := p.pos
	c := p.peek()
	switch c {
	case '(':
		return p.parseGroup(depth)
	case '[':
		return p.parseClass()
	case '.':
		p.pos++
		return &Node{Kind: KAny, Pos: start, End: p.pos}, nil
	case '^':
		p.pos++
		return &Node{Kind: KAnchor, Pos: start, End: p.pos, Anchor: ABegin}, nil
	case '$':
		p.pos++
		return &Node{Kind: KAnchor, Pos: start, End: p.pos, Anchor: AEnd}, nil
	case '\\':
		return p.parseEscapeAtom(start)
	case '*', '+', '?':
		return nil, p.failf(start, "unexpected quantifier %q", string(c))
	case ')':
		return nil, p.failf(start, "unexpected ')'")
	default:
		r, size := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += size
		return &Node{Kind: KChar, Pos: start, End: p.pos, Rune: r}, nil
	}
}

func (p *parser) parseGroup(depth int) (*Node, error) {
	start := p.pos
	p.pos++ // consume '('
	var idx int
	var name string
	var nodeKind Kind
	set, clear := FlagSet(0), FlagSet(0)
	var look *lookSpec

	if p.peek() == '?' {
		p.pos++
		switch p.peek() {
		case ':':
			p.pos++
		case '=':
			p.pos++
			look = &lookSpec{dir: LookAhead, neg: false}
		case '!':
			p.pos++
			look = &lookSpec{dir: LookAhead, neg: true}
		case '<':
			p.pos++
			switch p.peek() {
			case '=':
				p.pos++
				look = &lookSpec{dir: LookBehind, neg: false}
			case '!':
				p.pos++
				look = &lookSpec{dir: LookBehind, neg: true}
			default:
				n, err := p.parseGroupName('>')
				if err != nil {
					return nil, err
				}
				name = n
			}
		default:
			s, c2, colon, err := p.parseFlagSet()
			if err != nil {
				return nil, err
			}
			set, clear = s, c2
			if colon {
				nodeKind = KFlags
			} else {
				// Bare (?flags): applies until enclosing group end.
				nodeKind = KFlags
			}
		}
	}

	if look != nil {
		body, err := p.parseSeq(depth+1, false)
		if err != nil {
			return nil, err
		}
		if p.peek() != ')' {
			return nil, p.failf(p.pos, "unterminated lookaround group")
		}
		p.pos++
		width, variable := fixedWidth(body)
		return &Node{
			Kind: KLook, Pos: start, End: p.pos, Children: []*Node{body},
			LookDir: look.dir, LookNeg: look.neg,
			LookWidth: width, LookVariable: variable,
		}, nil
	}

	if name != "" {
		if _, dup := p.names[name]; dup {
			return nil, p.failf(start, "duplicate group name %q", name)
		}
		p.groups++
		idx = p.groups
		p.names[name] = idx
	} else if nodeKind == 0 {
		p.groups++
		idx = p.groups
	}

	body, err := p.parseSeq(depth+1, false)
	if err != nil {
		return nil, err
	}
	if p.peek() != ')' {
		return nil, p.failf(p.pos, "unterminated group")
	}
	p.pos++

	if nodeKind == KFlags {
		return &Node{
			Kind: KFlags, Pos: start, End: p.pos, Children: []*Node{body},
			SetFlags: set, ClearFlags: clear,
		}, nil
	}
	return &Node{
		Kind: KGroup, Pos: start, End: p.pos, Children: []*Node{body},
		GroupIndex: idx, GroupName: name,
	}, nil
}

type lookSpec struct {
	dir LookDir
	neg bool
}

func (p *parser) parseGroupName(close byte) (string, error) {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != close {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return "", p.failf(start, "unterminated named group")
	}
	name := p.src[start:p.pos]
	if !validName(name) {
		return "", p.failf(start, "invalid group name %q", name)
	}
	p.pos++ // close
	return name, nil
}

func validName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(i > 0 && c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

// parseFlagSet parses imsx... after '(?'. Returns sets and whether a ':' followed.
func (p *parser) parseFlagSet() (set, clear FlagSet, colon bool, err error) {
	neg := false
	at := p.pos
	for {
		c := p.peek()
		if c == '-' {
			neg = true
			p.pos++
			continue
		}
		if c == ':' {
			p.pos++
			return set, clear, true, nil
		}
		if c == ')' {
			p.pos++
			return set, clear, false, nil
		}
		var f FlagSet
		switch c {
		case 'i':
			f = FlagI
		case 'm':
			f = FlagM
		case 's':
			f = FlagS
		default:
			return 0, 0, false, p.failf(at, "unsupported or invalid flag %q", string(c))
		}
		if neg {
			clear |= f
		} else {
			set |= f
		}
		p.pos++
	}
}

func (p *parser) parseEscapeAtom(start int) (*Node, error) {
	p.pos++ // consume backslash
	if p.pos >= len(p.src) {
		return nil, p.failf(start, "trailing backslash")
	}
	c := p.src[p.pos]
	switch c {
	case 'A':
		p.pos++
		return &Node{Kind: KAnchor, Pos: start, End: p.pos, Anchor: ABeginText}, nil
	case 'z':
		p.pos++
		return &Node{Kind: KAnchor, Pos: start, End: p.pos, Anchor: AAtEnd}, nil
	case 'b':
		p.pos++
		return &Node{Kind: KAnchor, Pos: start, End: p.pos, Anchor: AWordBoundary}, nil
	case 'B':
		p.pos++
		return &Node{Kind: KAnchor, Pos: start, End: p.pos, Anchor: ANotWordBoundary}, nil
	case 'd', 'D', 's', 'S', 'w', 'W':
		cc := classFromEscape(c)
		p.pos++
		return &Node{Kind: KClass, Pos: start, End: p.pos, Class: cc}, nil
	case 'k':
		p.pos++
		name, err := p.parseKName()
		if err != nil {
			return nil, err
		}
		n := &Node{Kind: KBackref, Pos: start, End: p.pos, BackName: name}
		p.backrefs = append(p.backrefs, n)
		return n, nil
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		digits := p.digits()
		n := &Node{Kind: KBackref, Pos: start, End: p.pos, BackIndex: digits}
		p.backrefs = append(p.backrefs, n)
		if digits > p.maxBack {
			p.maxBack = digits
		}
		return n, nil
	case '0':
		p.pos++
		return &Node{Kind: KChar, Pos: start, End: p.pos, Rune: 0}, nil
	}
	r, err := p.parseSimpleEscape(start)
	if err != nil {
		return nil, err
	}
	return &Node{Kind: KChar, Pos: start, End: p.pos, Rune: r}, nil
}

func (p *parser) digits() int {
	n := 0
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		p.pos++
	}
	return n
}

func (p *parser) parseKName() (string, error) {
	at := p.pos
	if p.pos >= len(p.src) {
		return "", p.failf(at, "incomplete \\k")
	}
	var close byte
	switch p.src[p.pos] {
	case '<':
		close = '>'
	case '\'':
		close = '\''
	default:
		return "", p.failf(at, "expected < or ' after \\k")
	}
	p.pos++
	return p.parseGroupName(close)
}

// parseSimpleEscape parses escape forms that produce a literal rune.
func (p *parser) parseSimpleEscape(start int) (rune, error) {
	c := p.src[p.pos]
	switch c {
	case 'n':
		p.pos++
		return '\n', nil
	case 'r':
		p.pos++
	return '\r', nil
	case 't':
		p.pos++
		return '\t', nil
	case 'f':
		p.pos++
		return '\f', nil
	case 'v':
		p.pos++
		return '\v', nil
	case 'a':
		p.pos++
		return 7, nil
	case 'x':
		p.pos++
		return p.parseHex(2, start)
	case 'u':
		p.pos++
		return p.parseHex(4, start)
	}
	// Any other escaped character is a literal.
	r, size := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += size
	return r, nil
}

func (p *parser) parseHex(n, start int) (rune, error) {
	if p.pos+n > len(p.src) {
		return 0, p.failf(start, "incomplete hex escape")
	}
	v, err := strconv.ParseUint(p.src[p.pos:p.pos+n], 16, 32)
	if err != nil {
		return 0, p.failf(start, "invalid hex escape")
	}
	p.pos += n
	return rune(v), nil
}

func classFromEscape(c byte) *CharClass {
	cc := &CharClass{}
	switch c {
	case 'd':
		cc.Mask |= ClassD
	case 'D':
		cc.Negated = true
		cc.Mask |= ClassD
	case 's':
		cc.Mask |= ClassS
	case 'S':
		cc.Negated = true
		cc.Mask |= ClassS
	case 'w':
		cc.Mask |= ClassW
	case 'W':
		cc.Negated = true
		cc.Mask |= ClassW
	}
	return cc
}

func (p *parser) parseClass() (*Node, error) {
	start := p.pos
	p.pos++ // consume '['
	cc := &CharClass{}
	if p.peek() == '^' {
		cc.Negated = true
		p.pos++
	}
	first := true
	for {
		if p.pos >= len(p.src) {
			return nil, p.failf(start, "unterminated character class")
		}
		if p.peek() == ']' && !first {
			p.pos++
			break
		}
		first = false
		lo, err := p.parseClassElem()
		if err != nil {
			return nil, err
		}
	hi := lo
		if p.peek() == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
			p.pos++ // consume '-'
			hi, err = p.parseClassElem()
			if err != nil {
				return nil, err
			}
			if hi.r < lo.r {
				return nil, p.failf(start, "invalid character range %c-%c", lo.r, hi.r)
			}
		}
		cc.Ranges = append(cc.Ranges, RuneRange{Lo: lo.r, Hi: hi.r})
		cc.Mask |= lo.mask | hi.mask
		if lo.neg || hi.neg {
			// A complemented built-in inside a bracket is applied literally.
			return nil, p.failf(start, "complemented class not allowed inside bracket")
		}
	}
	mergeRanges(cc)
	return &Node{Kind: KClass, Pos: start, End: p.pos, Class: cc}, nil
}

type classElem struct {
	r    rune
	mask ClassMask
	neg  bool
}

func (p *parser) parseClassElem() (classElem, error) {
	start := p.pos
	if p.peek() == '\\' {
		p.pos++
		if p.pos >= len(p.src) {
			return classElem{}, p.failf(start, "trailing backslash in class")
		}
		switch c := p.src[p.pos]; c {
		case 'd', 's', 'w', 'D', 'S', 'W':
			p.pos++
			e := classElem{mask: classMask(c)}
			if c >= 'D' {
				e.neg = true
			}
			return e, nil
		case 'b':
			p.pos++
			return classElem{r: 8}, nil
		default:
			r, err := p.parseSimpleEscape(start)
			if err != nil {
				return classElem{}, err
			}
			return classElem{r: r}, nil
		}
	}
	r, size := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += size
	return classElem{r: r}, nil
}

func classMask(c byte) ClassMask {
	switch c {
	case 'd', 'D':
		return ClassD
	case 's', 'S':
		return ClassS
	case 'w', 'W':
		return ClassW
	}
	return 0
}

func mergeRanges(cc *CharClass) {
	if len(cc.Ranges) < 2 {
		return
	}
	out := cc.Ranges[:0]
	cur := cc.Ranges[0]
	for _, rr := range cc.Ranges[1:] {
		if rr.Lo <= cur.Hi+1 {
			if rr.Hi > cur.Hi {
				cur.Hi = rr.Hi
			}
			continue
		}
		out = append(out, cur)
	cur = rr
	}
	out = append(out, cur)
	cc.Ranges = append([]RuneRange(nil), out...)
}

