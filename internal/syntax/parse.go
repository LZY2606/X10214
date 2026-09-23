package syntax

import (
	"fmt"
	"strconv"
)

// ParseError describes a pattern grammar failure.
type ParseError struct {
	Pos int
	Msg string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("parse error at byte %d: %s", e.Pos, e.Msg)
}

type parser struct {
	src    string
	pos    int
	flags  uint8
	groups int
	names  map[string]int
}

// Parse compiles pattern text into an AST root and the name->index map.
func Parse(src string) (*Node, map[string]int, error) {
	p := &parser{src: src, names: map[string]int{}}
	root, err := p.parseAlternation()
	if err != nil {
		return nil, nil, err
	}
	if p.pos != len(p.src) {
		return nil, nil, p.errf("unexpected %q", p.src[p.pos])
	}
	if err := ResolveBackrefs(root, p.names); err != nil {
		return nil, nil, err
	}
	return root, p.names, nil
}

func (p *parser) errf(format string, args ...any) error {
	return &ParseError{Pos: p.pos, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) parseAlternation() (*Node, error) {
	first := true
	var branches []*Node
	for {
		seq, err := p.parseSequence()
		if err != nil {
			return nil, err
		}
		branches = append(branches, seq)
		if p.peek() != '|' {
			break
		}
		p.pos++
		first = false
	}
	_ = first
	if len(branches) == 1 {
		return branches[0], nil
	}
	n := NewNode(KAlt)
	n.Flags = p.flags
	n.Kids = branches
	n.Source = p.src
	return n, nil
}

func (p *parser) parseSequence() (*Node, error) {
	var items []*Node
start:
	switch p.peek() {
	case -1, '|', ')':
	default:
		atom, err := p.parseQuantified()
		if err != nil {
			return nil, err
		}
		if atom != nil {
			items = append(items, atom)
		}
		goto start
	}
	if len(items) == 0 {
		n := NewNode(KEmpty)
		n.Flags = p.flags
		return n, nil
	}
	if len(items) == 1 {
		return items[0], nil
	}
	n := NewNode(KConcat)
	n.Flags = p.flags
	n.Kids = items
	return n, nil
}

func (p *parser) parseQuantified() (*Node, error) {
	atom, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	for {
		c := p.peek()
		if c != '*' && c != '+' && c != '?' && c != '{' {
			return atom, nil
		}
		min, max, bounded, ok := p.tryQuantifier()
		if !ok {
			return atom, nil
		}
		if atom.Kind == KAnchor || atom.Kind == KLook && !atom.Behind {
			// quantifiers on zero-width assertions are permitted but guarded;
			// keep them to exercise the zero-width guard.
		}
		greedy := true
		if p.peek() == '?' {
			greedy = false
			p.pos++
		}
		r := NewNode(KRepeat)
		r.Min = min
		r.Max = max
		r.Greedy = greedy
		r.Child = atom
		r.Flags = atom.Flags
		_ = bounded
		atom = r
	}
}

func (p *parser) tryQuantifier() (min, max int, bounded, ok bool) {
	save := p.pos
	switch c := p.peek(); c {
	case '*':
		p.pos++
		return 0, -1, false, true
	case '+':
		p.pos++
		return 1, -1, false, true
	case '?':
		p.pos++
		return 0, 1, true, true
	case '{':
		start := p.pos
		p.pos++
		loDigits := p.consumeDigits()
		if loDigits == "" {
			p.pos = save
			return 0, 0, false, false
		}
		lo, _ := strconv.Atoi(loDigits)
		hi := lo
		hasHi := true
		if p.peek() == ',' {
			p.pos++
			hd := p.consumeDigits()
			if hd == "" {
				hi = -1
				hasHi = false
			} else {
				hi, _ = strconv.Atoi(hd)
			}
		}
		if p.peek() != '}' {
			p.pos = save
			return 0, 0, false, false
		}
		p.pos++
		if hasHi && hi < lo {
			p.pos = start
			return 0, 0, false, false
		}
		return lo, hi, true, true
	}
	p.pos = save
	return 0, 0, false, false
}

func (p *parser) consumeDigits() string {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	return p.src[start:p.pos]
}

func (p *parser) peek() int {
	if p.pos >= len(p.src) {
		return -1
	}
	return int(p.src[p.pos])
}

func (p *parser) expect(c byte) error {
	if p.peek() != int(c) {
		return p.errf("expected %q", c)
	}
	p.pos++
	return nil
}

func (p *parser) parseAtom() (*Node, error) {
	c := p.peek()
	switch c {
	case -1, '|', ')':
		return nil, p.errf("unexpected end of branch")
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '.':
		p.pos++
		n := NewNode(KAny)
		n.Flags = p.flags
		return n, nil
	case '^':
		p.pos++
		return p.anchor('^'), nil
	case '$':
		p.pos++
		return p.anchor('$'), nil
	case '\\':
		return p.parseEscape()
	case '*', '+', '?', '}':
		return nil, p.errf("quantifier with no atom")
	default:
		// literal byte; decode rune for source attribution only.
		start := p.pos
		ch := p.src[p.pos]
		p.pos++
		n := NewNode(KChar)
		n.Source = p.src[start:p.pos]
		n.Flags = p.flags
		if ch < utf8RuneSelf {
			n.Rune = rune(ch)
		} else {
			r, sz := decodeRuneInString(p.src[start:])
			n.Rune = r
			if sz > 1 {
				p.pos = start + sz
				n.Source = p.src[start:p.pos]
			} else {
				n.RawByte = int(ch)
			}
		}
		return n, nil
	}
}

const utf8RuneSelf = 0x80

func (p *parser) anchor(a byte) *Node {
	n := NewNode(KAnchor)
	n.Anchor = a
	n.Flags = p.flags
	return n
}

func (p *parser) parseGroup() (*Node, error) {
	start := p.pos
	p.pos++ // (
	positive := true
	behind := false
	capturing := true

	if p.peek() == '?' {
		p.pos++
		switch p.peek() {
		case ':':
			p.pos++
			capturing = false
		case '=', '!':
			p.pos++
			capturing = false
			positive = p.src[p.pos-1] == '='
		case '<':
			p.pos++
			switch p.peek() {
			case '=', '!':
				p.pos++
				capturing = false
				behind = true
				positive = p.src[p.pos-1] == '='
			default:
				return p.parseNamedGroup(start)
			}
		case 'i', 'm', 's', '-':
			return p.parseFlagGroup(start)
		default:
			return nil, p.errf("unsupported group form")
		}
	}

	index := 0
	if capturing {
		p.groups++
		index = p.groups
	}

	body, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	if capturing {
		g := NewNode(KGroup)
		g.Caps = true
		g.Index = index
		g.Child = body
		g.Flags = body.Flags
		g.Source = p.src[start:p.pos]
		return g, nil
	}
	if behind || !positive {
		l := NewNode(KLook)
		l.Positive = positive
		l.Behind = behind
		l.Child = body
		l.Flags = body.Flags
		l.Source = p.src[start:p.pos]
		return l, nil
	}
	return body, nil
}

func (p *parser) parseNamedGroup(start int) (*Node, error) {
	nameStart := p.pos
	for p.pos < len(p.src) && isNameByte(p.src[p.pos]) {
		p.pos++
	}
	name := p.src[nameStart:p.pos]
	if name == "" || p.peek() != '>' {
		return nil, p.errf("invalid named group")
	}
	p.pos++
	p.groups++
	idx := p.groups
	if _, dup := p.names[name]; dup {
		return nil, p.errf("duplicate capture name %q", name)
	}
	p.names[name] = idx
	body, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	g := NewNode(KGroup)
	g.Caps = true
	g.Index = idx
	g.Name = name
	g.Child = body
	g.Flags = body.Flags
	g.Source = p.src[start:p.pos]
	return g, nil
}

func (p *parser) parseFlagGroup(start int) (*Node, error) {
	saved := p.flags
	set, clear, err := p.readFlagSpec()
	if err != nil {
		return nil, err
	}
	p.flags = (p.flags | set) &^ clear
	scoped := false
	if p.peek() == ':' {
		p.pos++
		scoped = true
	}
	body, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	if scoped {
		p.flags = saved
		body.Source = p.src[start:p.pos]
		return body, nil
	}
	body.Source = p.src[start:p.pos]
	return body, nil
}

func (p *parser) readFlagSpec() (set, clear uint8, err error) {
	on := true
	for {
		c := p.peek()
		switch c {
		case 'i':
			if on {
				set |= FlagI
			} else {
				clear |= FlagI
			}
		case 'm':
			if on {
				set |= FlagM
			} else {
				clear |= FlagM
			}
		case 's':
			if on {
				set |= FlagS
			} else {
				clear |= FlagS
			}
		case '-':
			on = false
		case ':', ')', -1:
			if c == -1 {
				return 0, 0, p.errf("unterminated flag group")
			}
			return set, clear, nil
		default:
			return 0, 0, p.errf("bad flag %q", c)
		}
		p.pos++
	}
}

func isNameByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}
