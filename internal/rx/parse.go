package rx

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type parser struct {
	src        []byte
	pos        int
	flags      Flags
	groups     []GroupInfo
	names      map[string]int
	refs       []refPending
	nextID     int
	restores   []flagRestore
	openGroups []groupMark
	depth      int
}

type flagRestore struct {
	depth int
	flags Flags
}

type refPending struct {
	node *Node
	name string
	off  int
}

// Parse compiles source text into an AST and resolves group numbering.
func Parse(src string) (pat *Pattern, err error) {
	p := &parser{src: []byte(src), names: map[string]int{}}
	p.groups = append(p.groups, GroupInfo{}) // index 0 placeholder
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
				pat = nil
				return
			}
			panic(r)
		}
	}()
	beg := p.pos
	root := p.parseDisj()
	if p.pos < len(p.src) {
		if p.src[p.pos] == ')' {
			return nil, &SyntaxError{Msg: "unmatched ')'", Offset: p.pos}
		}
		return nil, &SyntaxError{Msg: "unexpected trailing input", Offset: p.pos}
	}
	if root == nil {
		root = p.newNode(NEmpty, beg, p.pos)
	}
	for _, rp := range p.refs {
		if rp.name != "" {
			idx, ok := p.names[rp.name]
			if !ok {
				return nil, &SemanticError{Msg: "backreference to undefined named group: " + rp.name, Offset: rp.off}
			}
			rp.node.RefIndex = idx
			continue
		}
		if rp.node.RefIndex < 1 || rp.node.RefIndex >= len(p.groups) {
			return nil, &SemanticError{Msg: "backreference to nonexistent group", Offset: rp.off}
		}
	}
	return &Pattern{Source: src, Root: root, Groups: p.groups, NextID: p.nextID, Flags: p.flags}, nil
}

func (p *parser) newNode(k NodeKind, beg, end int) *Node {
	p.nextID++
	return &Node{Kind: k, ID: p.nextID, Beg: beg, End: end, Max: -1, Flags: p.flags}
}

func (p *parser) parseDisj() *Node {
	beg := p.pos
	alts := []*Node{p.parseSeq()}
	for p.peek() == '|' {
		p.pos++
		alts = append(alts, p.parseSeq())
	}
	if len(alts) == 1 {
		return alts[0]
	}
	n := p.newNode(NAlt, beg, p.pos)
	n.Children = alts
	return n
}

func (p *parser) parseSeq() *Node {
	beg := p.pos
	var items []*Node
	for {
		c := p.peek()
		if c == -1 || c == '|' || c == ')' {
			break
		}
		atom := p.parseAtom()
		atom = p.parseQuant(atom)
		items = append(items, atom)
	}
	if len(items) == 0 {
		return p.newNode(NEmpty, beg, p.pos)
	}
	if len(items) == 1 {
		return items[0]
	}
	n := p.newNode(NConcat, beg, p.pos)
	n.Children = items
	return n
}

func (p *parser) peek() int {
	if p.pos >= len(p.src) {
		return -1
	}
	return int(p.src[p.pos])
}

func (p *parser) parseAtom() *Node {
	beg := p.pos
	c := p.peek()
	switch c {
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClassAtomic(beg)
	case '.':
		p.pos++
		return p.newNode(NDot, beg, p.pos)
	case '^':
		p.pos++
		n := p.newNode(NAnchor, beg, p.pos)
		n.Anchor = AnchorBOL
		return n
	case '$':
		p.pos++
		n := p.newNode(NAnchor, beg, p.pos)
		n.Anchor = AnchorEOL
		return n
	case '*', '+', '?':
		panicErr(&SyntaxError{Msg: "quantifier with nothing to repeat", Offset: beg})
	case ')':
		panicErr(&SyntaxError{Msg: "unmatched ')'", Offset: beg})
	case '\\':
		return p.parseEscapeAtom(beg)
	}
	if c == '{' {
		if _, _, _, ok := p.scanBraceQuant(); ok {
			panicErr(&SyntaxError{Msg: "quantifier with nothing to repeat", Offset: beg})
		}
	}
	if c == -1 {
		panicErr(&SyntaxError{Msg: "unexpected end of pattern", Offset: beg})
	}
	r, size := utf8.DecodeRune(p.src[p.pos:])
	p.pos += size
	n := p.newNode(NChar, beg, p.pos)
	n.Rune = r
	return n
}

func panicErr(err error) { panic(err) }

func (p *parser) parseQuant(atom *Node) *Node {
	c := p.peek()
	if c != '*' && c != '+' && c != '?' && c != '{' {
		return atom
	}
	var min, max int
	var unbounded, ok bool
	qbeg := p.pos
	switch c {
	case '*':
		p.pos++
		min, max, unbounded = 0, 0, true
	case '+':
		p.pos++
		min, max, unbounded = 1, 0, true
	case '?':
		p.pos++
		min, max = 0, 1
	case '{':
		min, max, unbounded, ok = p.scanBraceQuant()
		if !ok {
			return atom
		}
	}
	greedy := true
	if p.peek() == '?' {
		p.pos++
		greedy = false
	} else if p.peek() == '+' {
		panicErr(&SyntaxError{Msg: "possessive quantifiers are not supported", Offset: p.pos})
	}
	if atom.Kind == NRepeat {
		panicErr(&SyntaxError{Msg: "nested quantifier", Offset: qbeg})
	}
	if !unbounded && max < min {
		panicErr(&SyntaxError{Msg: "quantifier range out of order", Offset: qbeg})
	}
	n := p.newNode(NRepeat, atom.Beg, p.pos)
	n.Min, n.Greedy = min, greedy
	n.End = p.pos
	if unbounded {
		n.Max = -1
	} else {
		n.Max = max
	}
	n.Children = []*Node{atom}
	n.End = p.pos
	return n
}

// scanBraceQuant consumes {n}, {n,}, {n,m} when syntactically valid.
func (p *parser) scanBraceQuant() (min, max int, unbounded, ok bool) {
	save := p.pos
	if p.peek() != '{' {
		return 0, 0, false, false
	}
	j := save + 1
	readDigits := func() (int, bool) {
		start := j
		for j < len(p.src) && p.src[j] >= '0' && p.src[j] <= '9' {
			j++
		}
		if j == start {
			return 0, false
		}
		v, err := strconv.Atoi(string(p.src[start:j]))
		return v, err == nil
	}
	lo, hasLo := readDigits()
	if !hasLo {
		p.pos = save
		return 0, 0, false, false
	}
	if j >= len(p.src) {
		p.pos = save
		return 0, 0, false, false
	}
	switch p.src[j] {
	case '}':
		j++
		p.pos = j
		return lo, lo, false, true
	case ',':
		j++
		if j < len(p.src) && p.src[j] == '}' {
			j++
			p.pos = j
			return lo, 0, true, true
		}
		hi, hasHi := readDigits()
		if !hasHi || j >= len(p.src) || p.src[j] != '}' {
			p.pos = save
			return 0, 0, false, false
		}
		j++
		p.pos = j
		return lo, hi, false, true
	default:
		p.pos = save
		return 0, 0, false, false
	}
}

func (p *parser) parseGroup() *Node {
	beg := p.pos
	p.pos++ // consume (
	kind := NGroup
	negLook := false
	if p.peek() == '?' {
		p.pos++
		switch p.peek() {
		case ':':
			p.pos++
		case '=':
			p.pos++
			kind = NLookahead
		case '!':
			p.pos++
			kind = NLookahead
			negLook = true
		case '<':
			p.pos++
			switch p.peek() {
			case '=':
				p.pos++
				kind = NLookbehind
			case '!':
				p.pos++
				kind = NLookbehind
				negLook = true
			default:
				if !isGroupNameStart(p.peek()) {
					panicErr(&SyntaxError{Msg: "invalid group opener '(?<' (use (?<name>...), (?<=...) or (?<!...))", Offset: beg})
				}
				name, ok := p.scanGroupName()
				if !ok {
					panicErr(&SyntaxError{Msg: "invalid group name after '(<'", Offset: beg})
				}
				if _, dup := p.names[name]; dup {
					panicErr(&SemanticError{Msg: "duplicate group name: " + name, Offset: beg})
				}
				_ = p.openCapturingGroup(beg, name)
			}
		case 'P':
			p.pos++
			if p.peek() != '<' {
				panicErr(&SyntaxError{Msg: "expected '(?P<name>...)'", Offset: beg})
			}
			p.pos++
			name, ok := p.scanGroupName()
			if !ok {
				panicErr(&SyntaxError{Msg: "invalid group name after '(?P<'", Offset: beg})
			}
			if _, dup := p.names[name]; dup {
				panicErr(&SemanticError{Msg: "duplicate group name: " + name, Offset: beg})
			}
			_ = p.openCapturingGroup(beg, name)
		case 'i', 'm', 's', '-':
			p.parseFlagGroup(beg)
		default:
			panicErr(&SyntaxError{Msg: "unsupported group modifier", Offset: beg})
		}
	} else {
		_ = p.openCapturingGroup(beg, "")
	}

	p.depth++
	child := p.parseDisj()
	p.depth--
	if p.peek() != ')' {
		panicErr(&SyntaxError{Msg: "unterminated group", Offset: p.pos})
	}
	p.pos++

	switch kind {
	case NLookahead, NLookbehind:
		n := p.newNode(kind, beg, p.pos)
		n.Negate = negLook
		n.Children = []*Node{child}
		return n
	}

	var mark *groupMark
	if top := len(p.openGroups) - 1; top >= 0 && p.openGroups[top].beg == beg {
		mark = &p.openGroups[top]
	}
	n := p.newNode(NGroup, beg, p.pos)
	n.Children = []*Node{child}
	if mark != nil {
		p.groups[mark.index].End = p.pos
		n.Name = mark.name
		n.RefIndex = mark.index
		p.openGroups = p.openGroups[:len(p.openGroups)-1]
	}
	// Scoped non-capturing/flag group close: restore flags.
	if len(p.restores) > 0 && p.restores[len(p.restores)-1].depth == p.depth {
		r := p.restores[len(p.restores)-1]
		p.restores = p.restores[:len(p.restores)-1]
		p.flags = r.flags
	}
	return n
}

type groupMark struct {
	index int
	depth int
	beg   int
	name  string
}

func (p *parser) openCapturingGroup(beg int, name string) int {
	idx := len(p.groups)
	p.groups = append(p.groups, GroupInfo{Index: idx, Name: name, Beg: beg})
	if name != "" {
		p.names[name] = idx
	}
	p.openGroups = append(p.openGroups, groupMark{index: idx, depth: p.depth, beg: beg, name: name})
	return idx
}

// scanGroupName reads name> after (?< or (?P<, ending at '>'.
func (p *parser) scanGroupName() (string, bool) {
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '>' {
			name := string(p.src[start:p.pos])
			p.pos++
			if name == "" {
				return "", false
			}
			return name, true
		}
		if c == 0 || (p.pos > start && !(isNameByte(c) || c == '_')) {
			if p.pos == start && isAlpha(c) {
				// first byte fine
			} else if !isGroupNameByte(c) {
				return "", false
			}
		}
		p.pos++
	}
	return "", false
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isNameByte(c byte) bool {
	return isAlpha(c) || isDigit(c)
}
func isGroupNameByte(c byte) bool {
	return isAlpha(c) || isDigit(c) || c == '_'
}

func isGroupNameStart(c int) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// parseFlagGroup handles (?flags:...) and standalone (?flags).
func (p *parser) parseFlagGroup(beg int) {
	saved := p.flags
	add, del := Flags{}, Flags{}
	on := true
	seen := false
	for {
		c := p.peek()
		if c == 'i' || c == 'm' || c == 's' {
			seen = true
			f := &add
			if !on {
				f = &del
			}
			switch c {
			case 'i':
				f.I = true
			case 'm':
				f.M = true
			case 's':
				f.S = true
			}
			p.pos++
			continue
		}
		if c == '-' {
			on = false
			p.pos++
			continue
		}
		break
	}
	if !seen {
		panicErr(&SyntaxError{Msg: "empty flag list in group", Offset: beg})
	}
	if del.I {
		p.flags.I = false
	}
	if del.M {
		p.flags.M = false
	}
	if del.S {
		p.flags.S = false
	}
	if add.I {
		p.flags.I = true
	}
	if add.M {
		p.flags.M = true
	}
	if add.S {
		p.flags.S = true
	}
	if p.peek() == ':' {
		p.pos++
		p.restores = append(p.restores, flagRestore{depth: p.depth, flags: saved})
		return
	}
	if p.peek() != ')' {
		panicErr(&SyntaxError{Msg: "malformed flag group", Offset: beg})
	}
	// (?i) global switch: do not consume ')' here; flags persist.
}

func (p *parser) parseEscapeAtom(beg int) *Node {
	p.pos++ // consume backslash
	if p.pos >= len(p.src) {
		panicErr(&SyntaxError{Msg: "trailing backslash", Offset: beg})
	}
	c := p.src[p.pos]
	mk := func(k NodeKind) *Node {
		p.pos++
		n := p.newNode(k, beg, p.pos)
		return n
	}
	switch {
	case c == 'A':
		n := mk(NAnchor)
		n.Anchor = AnchorBOS
		return n
	case c == 'z':
		n := mk(NAnchor)
		n.Anchor = AnchorEOS
		return n
	case c == 'b':
		n := mk(NAnchor)
		n.Anchor = AnchorWB
		return n
	case c == 'B':
		n := mk(NAnchor)
		n.Anchor = AnchorNWB
		return n
	case c == 'd' || c == 'D' || c == 'w' || c == 'W' || c == 's' || c == 'S':
		p.pos++
		n := p.newNode(NClass, beg, p.pos)
		n.Class = shorthandClass(c)
		return n
	case c == 'p' || c == 'P':
		p.pos++
		neg := c == 'P'
		name := p.scanPropName(beg)
		n := p.newNode(NProperty, beg, p.pos)
		n.Name = name
		n.Negate = neg
		return n
	case c == 'k':
		p.pos++
		name := p.scanBackrefName(beg)
		n := p.newNode(NBackref, beg, p.pos)
		p.refs = append(p.refs, refPending{node: n, name: name, off: beg})
		return n
	case isDigit(c):
		return p.parseNumericEscape(beg)
	case c == 'Q':
		return p.parseQuote(beg)
	case c == 'x':
		r := p.scanHex(beg)
		n := p.newNode(NChar, beg, p.pos)
		n.Rune = r
		return n
	}
	// Simple escapes or escaped literal.
	switch c {
	case 'n':
		p.pos++
		n := p.newNode(NChar, beg, p.pos)
		n.Rune = '\n'
		return n
	case 't':
		p.pos++
		n := p.newNode(NChar, beg, p.pos)
		n.Rune = '\t'
		return n
	case 'r':
		p.pos++
		n := p.newNode(NChar, beg, p.pos)
		n.Rune = '\r'
		return n
	case 'f':
		p.pos++
		n := p.newNode(NChar, beg, p.pos)
		n.Rune = '\f'
		return n
	case 'v':
		p.pos++
		n := p.newNode(NChar, beg, p.pos)
		n.Rune = '\v'
		return n
	case 'a':
		p.pos++
		n := p.newNode(NChar, beg, p.pos)
		n.Rune = 0x07
		return n
	case '0':
		r := p.scanOctal(beg, 3)
		n := p.newNode(NChar, beg, p.pos)
		n.Rune = r
		return n
	}
	if strings.ContainsRune(`\\.+*?()|[]{}^$# /`, rune(c)) {
		p.pos++
		n := p.newNode(NChar, beg, p.pos)
		n.Rune = rune(c)
		return n
	}
	panicErr(&SyntaxError{Msg: "invalid escape sequence: \\" + string(c), Offset: beg})
	return nil
}

func (p *parser) parseNumericEscape(beg int) *Node {
	// Consume the maximal run of digits, then decide backref vs octal.
	start := p.pos
	for p.pos < len(p.src) && isDigit(p.src[p.pos]) {
		p.pos++
	}
	digits := string(p.src[start:p.pos])
	// A leading non-zero digit names a group if a group with that number
	// exists; numbers greater than group count fall back to octal decoding
	// (limited to 3 octal digits), matching documented rules.
	if digits[0] != '0' {
		if v, err := strconv.Atoi(digits); err == nil && v < len(p.groups) {
			n := p.newNode(NBackref, beg, p.pos)
			n.RefIndex = v
			return n
		}
		p.pos = start
		r := p.scanOctal(beg, 3)
		n := p.newNode(NChar, beg, beg)
		n.End = p.pos
		n.Rune = r
		return n
	}
	p.pos = start
	r := p.scanOctal(beg, 3)
	n := p.newNode(NChar, beg, p.pos)
	n.Rune = r
	return n
}

func (p *parser) scanOctal(beg int, max int) rune {
	v := 0
	n := 0
	for n < max && p.pos < len(p.src) {
		c := p.src[p.pos]
		if c < '0' || c > '7' {
			break
		}
		v = v*8 + int(c-'0')
		n++
		p.pos++
	}
	if n == 0 {
		panicErr(&SyntaxError{Msg: "expected octal digit", Offset: beg})
	}
	if v > 0xFF && max <= 3 {
		panicErr(&SyntaxError{Msg: "octal escape out of range", Offset: beg})
	}
	return rune(v)
}

func (p *parser) scanHex(beg int) rune {
	// \xHH or \x{HHHH}
	p.pos++ // consume x
	if p.peek() == '{' {
		p.pos++
		v := 0
		n := 0
		for p.pos < len(p.src) {
			c := p.src[p.pos]
			d, ok := hexVal(c)
			if !ok {
				break
			}
			v = v*16 + d
			n++
			p.pos++
		}
		if n == 0 || p.peek() != '}' {
			panicErr(&SyntaxError{Msg: "malformed \\x{} escape", Offset: beg})
		}
		p.pos++
		if v > 0x10FFFF {
			panicErr(&SyntaxError{Msg: "\\x value out of Unicode range", Offset: beg})
		}
		return rune(v)
	}
	v := 0
	for i := 0; i < 2; i++ {
		if p.pos >= len(p.src) {
			panicErr(&SyntaxError{Msg: "malformed \\x escape", Offset: beg})
		}
		d, ok := hexVal(p.src[p.pos])
		if !ok {
			panicErr(&SyntaxError{Msg: "malformed \\x escape", Offset: beg})
		}
		v = v*16 + d
		p.pos++
	}
	return rune(v)
}

func hexVal(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

func (p *parser) scanPropName(beg int) string {
	if p.peek() != '{' {
		panicErr(&SyntaxError{Msg: "expected '{' after \\p", Offset: beg})
	}
	p.pos++
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != '}' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		panicErr(&SyntaxError{Msg: "unterminated \\p{...}", Offset: beg})
	}
	name := strings.TrimSpace(string(p.src[start:p.pos]))
	p.pos++
	if name == "" {
		panicErr(&SyntaxError{Msg: "empty property name", Offset: beg})
	}
	return name
}

func (p *parser) scanBackrefName(beg int) string {
	if p.peek() != '<' {
		panicErr(&SyntaxError{Msg: "expected '<' after \\k", Offset: beg})
	}
	p.pos++
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != '>' {
		p.pos++
	}
	if p.pos >= len(p.src) {
		panicErr(&SyntaxError{Msg: "unterminated \\k<...>", Offset: beg})
	}
	name := string(p.src[start:p.pos])
	p.pos++
	if name == "" {
		panicErr(&SyntaxError{Msg: "empty backreference name", Offset: beg})
	}
	return name
}

func (p *parser) parseQuote(beg int) *Node {
	p.pos++ // consume Q
	start := p.pos
	for p.pos < len(p.src) {
		if p.src[p.pos] == '\\' && p.pos+1 < len(p.src) && p.src[p.pos+1] == 'E' {
			text := p.src[start:p.pos]
			p.pos += 2
			n := p.newNode(NQuote, beg, p.pos)
			n.Text = string(text)
			return n
		}
		p.pos++
	}
	panicErr(&SyntaxError{Msg: "unterminated \\Q...\\E", Offset: beg})
	return nil
}

func shorthandClass(c byte) *CharClass {
	pos := &POSIXItem{Name: string(c)}
	return &CharClass{POSIX: []POSIXItem{*pos}}
}

func (p *parser) parseClassAtomic(beg int) *Node {
	p.pos++ // consume [
	cc := &CharClass{}
	if p.peek() == '^' {
		cc.Negate = true
		p.pos++
	}
	first := true
	for p.pos < len(p.src) {
		if p.src[p.pos] == ']' && !first {
			p.pos++
			n := p.newNode(NClass, beg, p.pos)
			n.Class = cc
			return n
		}
		first = false
		p.parseClassItem(cc, beg)
	}
	panicErr(&SyntaxError{Msg: "unterminated character class", Offset: beg})
	return nil
}

func (p *parser) parseClassItem(cc *CharClass, beg int) {
	c := p.src[p.pos]
	if c == '[' && p.pos+2 < len(p.src) && p.src[p.pos+1] == ':' {
		p.parsePOSIXItem(cc, beg)
		return
	}
	if c == '\\' {
		if p.pos+1 < len(p.src) {
			switch p.src[p.pos+1] {
			case 'd', 'D', 'w', 'W', 's', 'S':
				cc.POSIX = append(cc.POSIX, POSIXItem{Name: string(p.src[p.pos+1])})
				p.pos += 2
				return
			case 'p', 'P':
				neg := p.src[p.pos+1] == 'P'
				p.pos += 2 // skip backslash and p/P
				name := p.scanPropName(beg)
				cc.Props = append(cc.Props, PropItem{Name: name, Negate: neg})
				return
			}
		}
	}
	lo := p.readClassRune(beg)
	// range?
	if p.pos < len(p.src) && p.src[p.pos] == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] != ']' {
		p.pos++ // consume -
		hi := p.readClassRune(beg)
		if hi < lo {
			panicErr(&SyntaxError{Msg: "character class range out of order", Offset: beg})
		}
		cc.Ranges = append(cc.Ranges, RuneRange{Lo: lo, Hi: hi})
		return
	}
	cc.Literals = append(cc.Literals, lo)
}

func (p *parser) readClassRune(beg int) rune {
	c := p.src[p.pos]
	if c == '\\' {
		p.pos++
		if p.pos >= len(p.src) {
			panicErr(&SyntaxError{Msg: "trailing backslash in class", Offset: beg})
		}
		e := p.src[p.pos]
		switch e {
		case 'n', 't', 'r', 'f', 'v', 'a':
			var r rune
			switch e {
			case 'n':
				r = '\n'
			case 't':
				r = '\t'
			case 'r':
				r = '\r'
			case 'f':
				r = '\f'
			case 'v':
				r = '\v'
			case 'a':
				r = 0x07
			}
			p.pos++
			return r
		case 'x':
			return p.scanHex(beg)
		case '0', '1', '2', '3', '4', '5', '6', '7':
			return p.scanOctal(beg, 3)
		case 'd', 'w', 's', 'D', 'W', 'S', 'p', 'P', 'b':
			if e == 'b' {
				p.pos++
				return '\b'
			}
			panicErr(&SyntaxError{Msg: "shorthand escape cannot be a range endpoint", Offset: beg})
		}
		p.pos++
		return rune(e)
	}
	if c == '-' {
		p.pos++
		return '-'
	}
	r, size := utf8.DecodeRune(p.src[p.pos:])
	p.pos += size
	return r
}

func (p *parser) parsePOSIXItem(cc *CharClass, beg int) {
	neg := false
	p.pos += 2 // consume [:
	if p.peek() == '^' {
		neg = true
		p.pos++
	}
	start := p.pos
	for p.pos < len(p.src) {
		if p.src[p.pos] == ':' && p.pos+1 < len(p.src) && p.src[p.pos+1] == ']' {
			name := string(p.src[start:p.pos])
			p.pos += 2
			if !isPOSIXName(name) {
				panicErr(&SyntaxError{Msg: "unknown POSIX class: " + name, Offset: beg})
			}
			cc.POSIX = append(cc.POSIX, POSIXItem{Name: name, Negate: neg})
			return
		}
		p.pos++
	}
	panicErr(&SyntaxError{Msg: "unterminated POSIX class", Offset: beg})
}

func isPOSIXName(name string) bool {
	switch name {
	case "alnum", "alpha", "blank", "cntrl", "digit", "graph", "lower", "print", "punct", "space", "upper", "xdigit":
		return true
	}
	return false
}
