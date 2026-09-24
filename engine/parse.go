package engine

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type parser struct {
	src    string
	pos    int
	groups int
	names  map[string]int
	gnames map[int]string
	nextID int
}

// Parse parses a pattern dialect into an AST.
func Parse(pattern string) (*Pattern, error) {
	p := &parser{src: pattern, names: map[string]int{}, gnames: map[int]string{}}
	ast, err := p.parseExpr(0, "")
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.src) {
		return nil, fmt.Errorf("unexpected character %q at %d", string(p.src[p.pos]), p.pos)
	}
	if ast == nil {
		ast = p.newNode(NEmpty)
	}
	return &Pattern{AST: ast, NumGroups: p.groups, Names: p.names, GroupNames: p.gnames}, nil
}

func (p *parser) newNode(k NodeKind) *Node {
	n := &Node{ID: p.nextID, Kind: k, Min: -1, Max: -1, Greedy: true}
	p.nextID++
	return n
}

func (p *parser) parseExpr(depth int, flags string) (*Node, error) {
	first, err := p.parseConcat(depth, flags)
	if err != nil {
		return nil, err
	}
	if p.pos >= len(p.src) || p.src[p.pos] != '|' {
		if first == nil {
			return p.newNode(NEmpty), nil
		}
		return first, nil
	}
	branches := []*Node{}
	add := func(n *Node) {
		if n == nil {
			n = p.newNode(NEmpty)
		}
		branches = append(branches, n)
	}
	add(first)
	for p.pos < len(p.src) && p.src[p.pos] == '|' {
		p.pos++
		b, err := p.parseConcat(depth, flags)
		if err != nil {
			return nil, err
		}
		add(b)
	}
	n := p.newNode(NAlt)
	n.Children = branches
	return n, nil
}

func (p *parser) parseConcat(depth int, flags string) (*Node, error) {
	var kids []*Node
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == ')' {
			if depth == 0 {
				return nil, fmt.Errorf("unmatched ')' at %d", p.pos)
			}
			break
		}
		if c == '|' {
			break
		}
		atom, err := p.parseAtom(depth, flags)
		if err != nil {
			return nil, err
		}
		if atom == nil {
			break
		}
		if atom.Kind == NFlags {
			flags = mergeFlags(flags, atom.Flags)
		}
		kids = append(kids, atom)
	}
	if len(kids) == 0 {
		return nil, nil
	}
	if len(kids) == 1 && kids[0].Kind != NFlags {
		return kids[0], nil
	}
	n := p.newNode(NConcat)
	n.Children = kids
	return n, nil
}

func (p *parser) parseAtom(depth int, flags string) (*Node, error) {
	if p.pos >= len(p.src) {
		return nil, nil
	}
	c := p.src[p.pos]
	var atom *Node
	var err error
	switch c {
	case '(':
		atom, err = p.parseGroup(depth)
	case '[':
		atom, err = p.parseClass()
	case '.':
		p.pos++
		n := p.newNode(NDot)
		n.Flags = flags
		atom = n
	case '^':
		p.pos++
		n := p.newNode(NAnchor)
		n.Anchor = "bol"
		n.Flags = flags
		atom = n
	case '$':
		p.pos++
		n := p.newNode(NAnchor)
		n.Anchor = "eol"
		n.Flags = flags
		atom = n
	case '\\':
		atom, err = p.parseEscape(flags)
	case '*', '+', '?':
		return nil, fmt.Errorf("quantifier %q with nothing to repeat at %d", string(c), p.pos)
	case '{':
		if _, _, ok := p.peekBrace(); ok {
			return nil, fmt.Errorf("quantifier %q with nothing to repeat at %d", string(c), p.pos)
		}
		p.pos++
		n := p.newNode(NChar)
		n.R = '{'
		n.Flags = flags
		atom = n
	case '|', ')':
		return nil, nil
	default:
		p.pos++
		r, _ := utf8.DecodeRuneInString(p.src[p.pos-1:])
		n := p.newNode(NChar)
		n.R = r
		n.Flags = flags
		atom = n
	}
	if err != nil {
		return nil, err
	}
	atom, err = p.maybeQuantifier(atom)
	if err != nil {
		return nil, err
	}
	return atom, nil
}

func (p *parser) maybeQuantifier(atom *Node) (*Node, error) {
	if p.pos >= len(p.src) {
		return atom, nil
	}
	var min, max int
	var hasQ bool
	switch p.src[p.pos] {
	case '*':
		min, max, hasQ = 0, -1, true
	case '+':
		min, max, hasQ = 1, -1, true
	case '?':
		min, max, hasQ = 0, 1, true
	case '{':
		a, b, ok := p.peekBrace()
		if !ok {
			return atom, nil
		}
		min, max, hasQ = a, b, true
	default:
		return atom, nil
	}
	p.pos += p.braceLen()
	greedy := true
	if p.pos < len(p.src) && p.src[p.pos] == '?' {
		greedy = false
		p.pos++
	} else if p.pos < len(p.src) && p.src[p.pos] == '+' {
		// possessive: unsupported as distinct semantics; treat as greedy.
		p.pos++
	}
	if atom.Kind == NAnchor {
		return nil, fmt.Errorf("quantifier applied to anchor")
	}
	n := p.newNode(NRepeat)
	n.Children = []*Node{atom}
	n.Min, n.Max, n.Greedy = min, max, greedy
	return n, nil
}

func (p *parser) peekBrace() (int, int, bool) {
	j := p.pos + 1
	a, na := 0, 0
	for j < len(p.src) && p.src[j] >= '0' && p.src[j] <= '9' {
		a = a*10 + int(p.src[j]-'0')
		na++
		j++
	}
	hasComma := false
	if j < len(p.src) && p.src[j] == ',' {
		hasComma = true
		j++
	}
	b, nb := 0, 0
	for j < len(p.src) && p.src[j] >= '0' && p.src[j] <= '9' {
		b = b*10 + int(p.src[j]-'0')
		nb++
		j++
	}
	if j >= len(p.src) || p.src[j] != '}' {
		return 0, 0, false
	}
	if na == 0 && !hasComma {
		return 0, 0, false // "{}" and friends are literal
	}
	if hasComma {
		if nb == 0 {
			b = -1
		}
	} else {
		b = a
	}
	if b != -1 && b < a {
		return 0, 0, false
	}
	return a, b, true
}

func (p *parser) braceLen() int {
	if p.src[p.pos] != '{' {
		return 1
	}
	j := p.pos + 1
	for j < len(p.src) && p.src[j] != '}' {
		j++
	}
	return j - p.pos + 1
}

func (p *parser) parseGroup(depth int) (*Node, error) {
	start := p.pos
	p.pos++ // consume (
	noncap := false
	var idx int
	var name string
	var grpFlags string
	ahead, positive := true, true
	isLook := false
	if p.pos < len(p.src) && p.src[p.pos] == '?' {
		p.pos++
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("dangling '?' in group at %d", start)
		}
		switch p.src[p.pos] {
		case ':':
			p.pos++
			noncap = true
		case '=':
			p.pos++
			noncap, isLook, ahead, positive = true, true, true, true
		case '!':
			p.pos++
			noncap, isLook, ahead, positive = true, true, true, false
		case '<':
			p.pos++
			if p.pos >= len(p.src) {
				return nil, fmt.Errorf("dangling '?<' at %d", start)
			}
			switch p.src[p.pos] {
			case '=':
				p.pos++
				noncap, isLook, ahead, positive = true, true, false, true
			case '!':
				p.pos++
				noncap, isLook, ahead, positive = true, true, false, false
			default:
				nm, err := p.scanName()
				if err != nil {
					return nil, err
				}
				name = nm
			}
		default:
			fl, hasColon, err := p.scanFlags()
			if err != nil {
				return nil, err
			}
			grpFlags = fl
			noncap = hasColon
		}
	}
	if !noncap {
		p.groups++
		idx = p.groups
		if name != "" {
			if _, dup := p.names[name]; dup {
				return nil, fmt.Errorf("duplicate group name %q", name)
			}
			p.names[name] = idx
			p.gnames[idx] = name
		}
	}
	body, err := p.finishGroup(depth+1, grpFlags)
	if err != nil {
		return nil, err
	}
	if isLook {
		n := p.newNode(NLook)
		n.Children = []*Node{body}
		n.Ahead, n.Positive = ahead, positive
		return n, nil
	}
	if noncap {
		if grpFlags == "" {
			return body, nil
		}
		n := p.newNode(NFlags)
		n.Flags = grpFlags
		n.Children = []*Node{body}
		return n, nil
	}
	g := p.newNode(NGroup)
	g.Children = []*Node{body}
	g.GroupIdx, g.GroupNm = idx, name
	return g, nil
}

func (p *parser) finishGroup(depth int, flags string) (*Node, error) {
	body, err := p.parseExpr(depth, flags)
	if err != nil {
		return nil, err
	}
	if p.pos >= len(p.src) || p.src[p.pos] != ')' {
		return nil, fmt.Errorf("unmatched '('")
	}
	p.pos++
	return body, nil
}

func (p *parser) scanName() (string, error) {
	begin := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != '>' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("unterminated group name")
	}
	nm := p.src[begin:p.pos]
	if nm == "" {
		return "", fmt.Errorf("empty group name")
	}
	if !(isAlpha(nm[0]) || nm[0] == '_') {
		return "", fmt.Errorf("invalid group name %q", nm)
	}
	for i := 1; i < len(nm); i++ {
		if !(isAlnum(nm[i]) || nm[i] == '_') {
			return "", fmt.Errorf("invalid group name %q", nm)
		}
	}
	p.pos++ // consume >
	return nm, nil
}

func (p *parser) scanFlags() (string, bool, error) {
	begin := p.pos
	neg := false
	var sb strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '-' {
			neg = true
			p.pos++
			continue
		}
		if c == ')' || c == ':' {
			break
		}
		if c != 'i' && c != 's' && c != 'm' {
			return "", false, fmt.Errorf("unsupported flag %q", string(c))
		}
		if neg {
			sb.WriteByte(byte(unicodeUpper(c)))
		} else {
			sb.WriteByte(c)
		}
		p.pos++
	}
	hasColon := false
	if p.pos < len(p.src) && p.src[p.pos] == ':' {
		hasColon = true
		p.pos++
	}
	_ = begin
	return sb.String(), hasColon, nil
}

func unicodeUpper(c byte) byte {
	return c - 32
}

func isAlpha(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isAlnum(c byte) bool { return isAlpha(c) || (c >= '0' && c <= '9') }

func mergeFlags(base, add string) string {
	set := map[byte]bool{}
	for i := 0; i < len(base); i++ {
		set[base[i]] = true
	}
	for i := 0; i < len(add); i++ {
		c := add[i]
		if c >= 'A' && c <= 'Z' {
			delete(set, c+32)
		} else {
			set[c] = true
		}
	}
	var b strings.Builder
	for _, c := range []byte{'i', 's', 'm'} {
		if set[c] {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func (p *parser) parseEscape(flags string) (*Node, error) {
	start := p.pos
	p.pos++ // backslash
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("trailing backslash at %d", start)
	}
	c := p.src[p.pos]
	switch {
	case c >= '1' && c <= '9':
		var n int
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			n = n*10 + int(p.src[p.pos]-'0')
			p.pos++
		}
		node := p.newNode(NBackref)
		node.RefIdx = n
		node.Flags = flags
		return node, nil
	case c == 'k':
		p.pos++
		if p.pos >= len(p.src) || p.src[p.pos] != '<' {
			return nil, fmt.Errorf("expected < after \\k")
		}
		nm, err := p.scanName()
		if err != nil {
			return nil, err
		}
		node := p.newNode(NRef)
		node.GroupNm = nm
		node.Flags = flags
		return node, nil
	case c == 'A':
		p.pos++
		n := p.newNode(NAnchor)
		n.Anchor = "start"
		return n, nil
	case c == 'z':
		p.pos++
		n := p.newNode(NAnchor)
		n.Anchor = "end"
		return n, nil
	case c == 'b':
		p.pos++
		n := p.newNode(NAnchor)
		n.Anchor = "wb"
		return n, nil
	case c == 'B':
		p.pos++
		n := p.newNode(NAnchor)
		n.Anchor = "nwb"
		return n, nil
	case c == 'd' || c == 'D' || c == 'w' || c == 'W' || c == 's' || c == 'S':
		p.pos++
		n := p.newNode(NChar)
		n.R = -1
		n.Class = []ClassTerm{{Kind: "perl", R1: rune(c)}}
		return n, nil
	case c == 'p' || c == 'P':
		neg := c == 'P'
		p.pos++
		name, err := p.readUnicodeName()
		if err != nil {
			return nil, err
		}
		n := p.newNode(NChar)
		n.R = -1
		n.Class = []ClassTerm{{Kind: "cat", Name: name, Neg: neg}}
		return n, nil
	}
	r, sz := p.decodeSimpleEscape()
	if sz > 0 {
		p.pos += sz
		n := p.newNode(NChar)
		n.R = r
		n.Flags = flags
		return n, nil
	}
	p.pos++
	r2, _ := utf8.DecodeRuneInString(p.src[p.pos-1:])
	n := p.newNode(NChar)
	n.R = r2
	n.Flags = flags
	return n, nil
}

func (p *parser) readUnicodeName() (string, error) {
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("incomplete \\p")
	}
	if p.src[p.pos] == '{' {
		p.pos++
		begin := p.pos
		for p.pos < len(p.src) && p.src[p.pos] != '}' {
			p.pos++
		}
		if p.pos >= len(p.src) {
			return "", fmt.Errorf("unterminated \\p{...}")
		}
		nm := p.src[begin:p.pos]
		p.pos++
		return nm, nil
	}
	return p.src[p.pos : p.pos+1], nil
}

// decodeSimpleEscape decodes escapes beginning just after the backslash
// (p.pos points at the escape char). Returns size consumed (including the
// escape char); size 0 means "treat next rune literally".
func (p *parser) decodeSimpleEscape() (rune, int) {
	c := p.src[p.pos]
	switch c {
	case 'n':
		return '\n', 1
	case 'r':
		return '\r', 1
	case 't':
		return '\t', 1
	case 'f':
		return 0x0c, 1
	case 'v':
		return 0x0b, 1
	case 'a':
		return 0x07, 1
	case '0':
		return 0, 1
	case 'x':
		if p.pos+1 < len(p.src) && p.src[p.pos+1] == '{' {
			j := p.pos + 2
			v, nd := 0, 0
			for j < len(p.src) && isHex(p.src[j]) {
				v = v*16 + hexVal(p.src[j])
				j++
				nd++
			}
			if nd > 0 && j < len(p.src) && p.src[j] == '}' {
				return rune(v), j - p.pos + 1
			}
		}
		if p.pos+2 < len(p.src) && isHex(p.src[p.pos+1]) && isHex(p.src[p.pos+2]) {
			return rune(hexVal(p.src[p.pos+1])*16 + hexVal(p.src[p.pos+2])), 3
		}
	}
	return 0, 0
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}

func (p *parser) parseClass() (*Node, error) {
	start := p.pos
	p.pos++ // [
	neg := false
	if p.pos < len(p.src) && (p.src[p.pos] == '^') {
		neg = true
		p.pos++
	}
	var terms []ClassTerm
	first := true
	for p.pos < len(p.src) && p.src[p.pos] != ']' {
		if first && p.pos < len(p.src) && p.src[p.pos] == ']' {
			break
		}
		first = false
		t, err := p.parseClassTerm()
		if err != nil {
			return nil, err
		}
		terms = append(terms, t...)
	}
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("unterminated character class at %d", start)
	}
	p.pos++ // ]
	n := p.newNode(NClass)
	n.Class = terms
	n.ClassNeg = neg
	return n, nil
}

func (p *parser) parseClassTerm() ([]ClassTerm, error) {
	// leading ] allowed, - allowed except as middle of range
	if p.src[p.pos] == '\\' {
		p.pos++
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("trailing backslash in class")
		}
		c := p.src[p.pos]
		switch c {
		case 'd', 'D', 'w', 'W', 's', 'S':
			p.pos++
			return []ClassTerm{{Kind: "perl", R1: rune(c)}}, nil
		case 'p', 'P':
			p.pos++
			nm, err := p.readUnicodeName()
			if err != nil {
				return nil, err
			}
			return []ClassTerm{{Kind: "cat", Name: nm, Neg: c == 'P'}}, nil
		case 'x', 'n', 'r', 't', 'f', 'v', 'a', '0':
			r, sz := p.decodeSimpleEscape()
			p.pos += sz
			return p.classRuneOrRange(r)
		default:
			p.pos++
			r, _ := utf8.DecodeRuneInString(p.src[p.pos-1:])
			return p.classRuneOrRange(r)
		}
	}
	// POSIX classes [:alpha:] etc.
	if p.pos+1 < len(p.src) && p.src[p.pos] == '[' && p.src[p.pos+1] == ':' {
		end := p.pos + 2
		for end < len(p.src) && !(p.src[end] == ':' && end+1 < len(p.src) && p.src[end+1] == ']') {
			end++
		}
		if end < len(p.src) {
			name := p.src[p.pos+2 : end]
			p.pos = end + 2
			return []ClassTerm{{Kind: "posix", Name: name}}, nil
		}
	}
	begin := p.pos
	r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += sz
	_ = begin
	return p.classRuneOrRange(r)
}

// classRuneOrRange appends a range if the just-read rune is followed by '-'
// and another rune (the '-' is not at the class end).
func (p *parser) classRuneOrRange(r rune) ([]ClassTerm, error) {
	if p.pos+1 < len(p.src) && p.src[p.pos] == '-' && p.src[p.pos+1] != ']' {
		p.pos++ // consume -
		var r2 rune
		if p.src[p.pos] == '\\' {
			p.pos++
			c := p.src[p.pos]
			switch c {
			case 'd', 'D', 'w', 'W', 's', 'S':
				return nil, fmt.Errorf("cannot use \\%c as range end", c)
			case 'x', 'n', 'r', 't', 'f', 'v', 'a', '0':
				v, sz := p.decodeSimpleEscape()
				p.pos += sz
				r2 = v
			default:
				p.pos++
				v, _ := utf8.DecodeRuneInString(p.src[p.pos-1:])
				r2 = v
			}
		} else {
			v, sz := utf8.DecodeRuneInString(p.src[p.pos:])
			p.pos += sz
			r2 = v
		}
		if r2 < r {
			return nil, fmt.Errorf("invalid range %q-%q", string(r), string(r2))
		}
		return []ClassTerm{{Kind: "range", R1: r, R2: r2}}, nil
	}
	return []ClassTerm{{Kind: "rune", R1: r}}, nil
}
