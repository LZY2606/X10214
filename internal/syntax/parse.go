package syntax

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ParseError describes a grammar failure at a byte offset.
type ParseError struct {
	Pos Pos
	Msg string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("syntax error at offset %d: %s", e.Pos, e.Msg)
}

type parser struct {
	src   string
	pos   Pos
	flags Flags
	rx    *Regex
}

// Parse parses pattern with the pinned grammar version.
func Parse(pattern string) (*Regex, error) {
	p := &parser{
		src:   pattern,
		rx:    &Regex{Pattern: pattern, GroupNames: map[string]int{}},
		flags: Flags{},
	}
	root, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if p.pos < Pos(len(p.src)) {
		return nil, &ParseError{Pos: p.pos, Msg: "unexpected input"}
	}
	p.rx.Root = simplify(root)
	p.rx.Flags = p.flags
	return p.rx, nil
}

func (p *parser) errf(at Pos, format string, args ...any) error {
	return &ParseError{Pos: at, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) peek() byte {
	if int(p.pos) < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

func (p *parser) consume() byte {
	b := p.src[p.pos]
	p.pos++
	return b
}

// parseAlt := parseCat ('|' parseCat)*
func (p *parser) parseAlt() (Node, error) {
	at := p.pos
	opts := []Node{}
	for {
		n, err := p.parseCat()
		if err != nil {
			return nil, err
		}
		opts = append(opts, n)
		if p.peek() != '|' {
			break
		}
		p.pos++
	}
	if len(opts) == 1 {
		return opts[0], nil
	}
	return AltNode{P: at, Options: opts}, nil
}

func (p *parser) parseCat() (Node, error) {
	at := p.pos
	nodes := []Node{}
	for {
		b := p.peek()
		if b == 0 || b == '|' || b == ')' {
			break
		}
		n, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 0 {
		return EmptyNode{P: at}, nil
	}
	if len(nodes) == 1 {
		return nodes[0], nil
	}
	return CatNode{P: at, Nodes: nodes}, nil
}

func simplify(n Node) Node {
	switch x := n.(type) {
	case CatNode:
		if len(x.Nodes) == 0 {
			return EmptyNode{P: x.P}
		}
		if len(x.Nodes) == 1 {
			return simplify(x.Nodes[0])
		}
		flat := make([]Node, 0, len(x.Nodes))
		for _, c := range x.Nodes {
			c = simplify(c)
			if cc, ok := c.(CatNode); ok {
				flat = append(flat, cc.Nodes...)
			} else {
				flat = append(flat, c)
			}
		}
		x.Nodes = flat
		return x
	case AltNode:
		for i := range x.Options {
			x.Options[i] = simplify(x.Options[i])
		}
		return x
	case GroupNode:
		x.Body = simplify(x.Body)
		return x
	case RepNode:
		x.X = simplify(x.X)
		return x
	}
	return n
}

func isQuantStart(b byte) bool {
	return b == '*' || b == '+' || b == '?' || b == '{'
}

func (p *parser) parseAtom() (Node, error) {
	at := p.pos
	b := p.peek()
	switch b {
	case '(':
		n, err := p.parseGroup()
		if err != nil {
			return nil, err
		}
		return p.parseRepeat(n)
	case '[':
		n, err := p.parseClass()
		if err != nil {
			return nil, err
		}
		return p.parseRepeat(n)
	case '.':
		p.pos++
		return p.parseRepeat(DotNode{P: at})
	case '^':
		p.pos++
		return AnchorNode{P: at, Kind: AnchorLineStart}, nil
	case '$':
		p.pos++
		return AnchorNode{P: at, Kind: AnchorLineEnd}, nil
	case '\\':
		n, err := p.parseEscapeAtom()
		if err != nil {
			return nil, err
		}
		return p.parseRepeat(n)
	case '*', '+', '?':
		return nil, p.errf(at, "quantifier %q with nothing to repeat", string(b))
	case '{':
		if _, _, ok := p.scanBoundedAt(at); ok {
			return nil, p.errf(at, "bounded quantifier with nothing to repeat")
		}
		r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += Pos(sz)
		return p.parseRepeat(LitNode{P: at, R: r})
	case 0:
		return nil, p.errf(at, "unexpected end of pattern")
	default:
		r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += Pos(sz)
		return p.parseRepeat(LitNode{P: at, R: r})
	}
}

func (p *parser) parseRepeat(n Node) (Node, error) {
	for {
		at := p.pos
		b := p.peek()
		var r RepNode
		has := false
		switch {
		case b == '*':
			p.pos++
			r = RepNode{P: at, X: n, Min: 0, Max: -1}
			has = true
		case b == '+':
			p.pos++
			r = RepNode{P: at, X: n, Min: 1, Max: -1}
			has = true
		case b == '?':
			p.pos++
			r = RepNode{P: at, X: n, Min: 0, Max: 1}
			has = true
		case b == '{':
			if mn, mx, ok := p.scanBoundedAt(at); ok {
				if mn > RepeatExpand || mx > RepeatExpand {
					return nil, p.errf(at, "bounded repetition exceeds %d", RepeatExpand)
				}
				r = RepNode{P: at, X: n, Min: mn, Max: mx}
				has = true
			}
		}
		if !has {
			return n, nil
		}
		switch p.peek() {
		case '?':
			r.Lazy = true
			p.pos++
		case '+':
			r.Possessive = true
			p.pos++
		}
		n = r
	}
}

// scanBoundedAt parses {n}, {n,} and {n,m} without consuming when malformed.
func (p *parser) scanBoundedAt(at Pos) (min, max int, ok bool) {
	s := p.src[at:]
	if len(s) < 2 || s[0] != '{' {
		return 0, 0, false
	}
	i := 1
	j := i
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i || j-i > 4 {
		return 0, 0, false
	}
	min64, _ := strconv.Atoi(s[i:j])
	i = j
	mx := min64
	if i < len(s) && s[i] == ',' {
		i++
		k := i
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k == i {
			mx = -1
		} else {
			if k-i > 4 {
				return 0, 0, false
			}
			v, _ := strconv.Atoi(s[i:k])
			mx = v
		}
		i = k
	}
	if i >= len(s) || s[i] != '}' {
		return 0, 0, false
	}
	if mx >= 0 && mx < min64 {
		return 0, 0, false
	}
	p.pos = at + Pos(i+1)
	return min64, mx, true
}

func (p *parser) parseGroup() (Node, error) {
	at := p.pos
	p.pos++ // '('
	g := GroupNode{P: at}
	saved := p.flags
	if p.peek() == '?' {
		p.pos++
		c := p.peek()
		switch {
		case c == ':':
			p.pos++
			g.Kind = GroupNonCapture
		case c == '>':
			p.pos++
			g.Kind = GroupAtomic
		case c == '=':
			p.pos++
			g.Kind = GroupLookahead
			g.Local = saved
		case c == '!':
			p.pos++
			g.Kind = GroupNegLookahead
			g.Local = saved
		case c == 'i' || c == 's' || c == 'm' || c == 'u' || c == '-':
			on, off, scoped, err := p.parseFlagDir()
			if err != nil {
				return nil, err
			}
			if scoped {
				g.Kind = GroupNonCapture
				g.Local = saved
				p.flags = saved.with(on, off)
			} else {
				p.flags = p.flags.with(on, off)
				fn := FlagNode{P: at, On: on, Off: off, Flags: p.flags}
				body, err := p.finishDirectiveBody()
				if err != nil {
					return nil, err
				}
				if _, empty := body.(EmptyNode); empty {
					return fn, nil
				}
				g2 := GroupNode{P: at, Kind: GroupNonCapture, Local: saved,
					Body: CatNode{P: at, Nodes: []Node{fn, body}}}
				return g2, nil
			}
		case c == 'P' || c == '<' || c == '\'':
			name, kind, err := p.parseNamedOrLook(c, saved)
			if err != nil {
				return nil, err
			}
			g.Kind = kind
			g.Name = name
		default:
			return nil, p.errf(at, "unsupported group directive ?%c", c)
		}
	} else {
		g.Kind = GroupCapture
	}
	if g.Kind == GroupCapture || g.Kind == GroupNamed {
		p.rx.NumGroups++
		g.Index = p.rx.NumGroups
		if g.Kind == GroupNamed {
			p.rx.GroupNames[g.Name] = g.Index
		}
	}
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if p.peek() != ')' {
		return nil, p.errf(p.pos, "missing closing parenthesis")
	}
	p.pos++
	g.Body = body
	p.flags = saved
	return g, nil
}

func (p *parser) finishDirectiveBody() (Node, error) {
	body, err := p.parseAlt()
	if err != nil {
		return nil, err
	}
	if p.peek() != ')' {
		return nil, p.errf(p.pos, "missing closing parenthesis")
	}
	p.pos++
	// The directive stays in effect until the enclosing group closes; its
	// flags were already applied by the caller.
	return body, nil
}

func (p *parser) parseFlagDir() (on, off string, scoped bool, err error) {
	var b strings.Builder
	for p.peek() != 0 && p.peek() != ')' && p.peek() != ':' {
		b.WriteByte(p.consume())
	}
	cur := p.peek()
	if cur == ':' {
		p.pos++
		scoped = true
	} else if cur != ')' {
		return "", "", false, p.errf(p.pos, "malformed flag directive")
	}
	spec := b.String()
	parts := strings.SplitN(spec, "-", 2)
	on = parts[0]
	if len(parts) == 2 {
		off = parts[1]
	}
	if spec == "-" || !validFlags(on+off) {
		return "", "", false, p.errf(p.pos, "unsupported flag in %q", spec)
	}
	return on, off, scoped, nil
}

func validFlags(s string) bool {
	for _, c := range s {
		switch c {
		case 'i', 's', 'm', 'u':
		default:
			return false
		}
	}
	return true
}

func (p *parser) parseNamedOrLook(c byte, saved Flags) (name string, kind GroupKind, err error) {
	switch c {
	case 'P':
		// (?P<name> ... )
		p.pos++ // P
		if p.peek() != '<' {
			return "", 0, p.errf(p.pos, "expected < after ?P")
		}
		p.pos++
		name, err = p.readName('>')
		return name, GroupNamed, err
	case '<':
		p.pos++
		switch p.peek() {
		case '=':
			p.pos++
			return "", GroupLookbehind, nil
		case '!':
			p.pos++
			return "", GroupNegLookbehind, nil
		default:
			name, err = p.readName('>')
			return name, GroupNamed, err
		}
	case '\'':
		p.pos++
		name, err = p.readName('\'')
		return name, GroupNamed, err
	}
	return "", 0, p.errf(p.pos, "malformed group")
}

func (p *parser) readName(close byte) (string, error) {
	var b strings.Builder
	for p.peek() != 0 && p.peek() != close {
		b.WriteByte(p.consume())
	}
	if p.peek() != close {
		return "", p.errf(p.pos, "unterminated group name")
	}
	p.pos++
	name := b.String()
	if name == "" {
		return "", p.errf(p.pos, "empty group name")
	}
	if existing, dup := p.rx.GroupNames[name]; dup {
		return "", p.errf(p.pos, "duplicate group name %q (already group %d)", name, existing)
	}
	return name, nil
}

func (p *parser) parseClass() (Node, error) {
	at := p.pos
	p.pos++ // '['
	cn := ClassNode{P: at}
	if p.peek() == '^' {
		cn.Negate = true
		p.pos++
	}
	if p.peek() == ']' { // ] is literal first
		p.pos++
		cn.Items = append(cn.Items, ClassItem{Single: true, R: ']'})
	}
	for p.peek() != 0 && p.peek() != ']' {
		item, err := p.parseClassItem()
		if err != nil {
			return nil, err
		}
		cn.Items = append(cn.Items, item)
	}
	if p.peek() != ']' {
		return nil, p.errf(p.pos, "unterminated character class")
	}
	p.pos++
	if len(cn.Items) == 0 {
		return nil, p.errf(at, "empty character class")
	}
	return cn, nil
}

func (p *parser) parseClassItem() (ClassItem, error) {
	item, err := p.readClassItem()
	if err != nil {
		return ClassItem{}, err
	}
	if item.Single && p.peek() == '-' && p.pos+1 < Pos(len(p.src)) && p.src[p.pos+1] != ']' {
		p.pos++ // '-'
		hi, err := p.readClassItem()
		if err != nil {
			return ClassItem{}, err
		}
		if !hi.Single {
			return ClassItem{}, p.errf(p.pos, "property escape cannot end a range")
		}
		if item.R > hi.R {
			return ClassItem{}, p.errf(p.pos, "invalid character range %c-%c", item.R, hi.R)
		}
		return ClassItem{Range: true, Lo: item.R, Hi: hi.R}, nil
	}
	return item, nil
}

func (p *parser) readClassItem() (ClassItem, error) {
	at := p.pos
	if p.peek() == '\\' {
		p.pos++
		return p.parseClassEscape(at)
	}
	if p.peek() == 0 {
		return ClassItem{}, p.errf(at, "unterminated character class")
	}
	r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
	p.pos += Pos(sz)
	return ClassItem{Single: true, R: r}, nil
}
