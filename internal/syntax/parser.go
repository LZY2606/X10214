package syntax

import (
	"fmt"
	"strconv"
	"unicode/utf8"
)

// Error describes a parse failure with a pattern byte offset.
type Error struct {
	Pos int
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("pattern error at byte %d: %s", e.Pos, e.Msg) }

type parser struct {
	src    string
	pos    int
	groups []Group
	names  map[string]int
}

// Parse compiles a pattern under SyntaxVersion.
func Parse(src string) (*Pattern, error) {
	p := &parser{src: src, names: map[string]int{}}
	root, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if p.pos != len(src) {
		return nil, &Error{Pos: p.pos, Msg: "unexpected ')' without matching '('"}
	}
	pat := &Pattern{Root: root, Groups: p.groups, NumGroups: len(p.groups)}
	if err := validate(pat); err != nil {
		return nil, err
	}
	return pat, nil
}

func (p *parser) errf(pos int, f string, a ...any) error {
	return &Error{Pos: pos, Msg: fmt.Sprintf(f, a...)}
}

func (p *parser) parseAlternation() (*Atom, error) {
	var branches [][]*Atom
	var cur []*Atom
	for {
		node, err := p.parseConcatSeq()
		if err != nil {
			return nil, err
		}
		cur = node
		if p.pos >= len(p.src) || p.src[p.pos] == ')' {
			branches = append(branches, cur)
			break
		}
		if p.src[p.pos] == '|' {
			p.pos++
			branches = append(branches, cur)
			cur = nil
			continue
		}
		return nil, p.errf(p.pos, "unexpected character %q", p.src[p.pos])
	}
	if len(branches) == 1 {
		return concatNode(branches[0], 0, p.pos), nil
	}
	a := &Atom{Kind: KindAlt, Pos: 0, End: p.pos}
	for _, b := range branches {
		a.Nodes = append(a.Nodes, concatNode(b, 0, p.pos))
	}
	return a, nil
}

func concatNode(nodes []*Atom, pos, end int) *Atom {
	if len(nodes) == 1 {
		return nodes[0]
	}
	return &Atom{Kind: KindConcat, Nodes: nodes, Pos: pos, End: end}
}

func (p *parser) parseConcatSeq() ([]*Atom, error) {
	var nodes []*Atom
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '|' || c == ')' {
			break
		}
		atom, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		atom, err = p.parseQuantifier(atom)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, atom)
	}
	return nodes, nil
}

func (p *parser) parseQuantifier(atom *Atom) (*Atom, error) {
	if p.pos >= len(p.src) {
		return atom, nil
	}
	c := p.src[p.pos]
	var min, max int
	infinite := false
	qpos := p.pos
	switch c {
	case '*':
		min, max, infinite = 0, 0, true
		p.pos++
	case '+':
		min, max, infinite = 1, 0, true
		p.pos++
	case '?':
		min, max = 0, 1
		p.pos++
	default:
		if c == '{' {
			n, m, inf, ok := p.tryBrace()
			if ok {
				min, max, infinite = n, m, inf
				qpos = p.pos
				_ = qpos
			} else {
				return atom, nil
			}
		} else {
			return atom, nil
		}
	}
	if min > max && !infinite {
		return nil, p.errf(qpos, "quantifier minimum exceeds maximum")
	}
	mode := Greedy
	if p.pos < len(p.src) && p.src[p.pos] == '?' {
		mode = Lazy
		p.pos++
	}
	return &Atom{Kind: KindQuant, Pos: atom.Pos, End: atom.End, Target: atom,
		Min: min, Max: max, Mode: mode}, nil
}

// tryBrace consumes a well-formed {n}, {n,} or {n,m}; otherwise leaves input.
func (p *parser) tryBrace() (min, max int, infinite, ok bool) {
	start := p.pos
	p.pos++ // '{'
	readNum := func() (int, bool) {
		s := p.pos
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
		if s == p.pos {
			return 0, false
		}
		n, _ := strconv.Atoi(p.src[s:p.pos])
		return n, true
	}
	n, gotN := readNum()
	if !gotN {
		p.pos = start
		return
	}
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		return n, n, false, true
	}
	if p.pos >= len(p.src) || p.src[p.pos] != ',' {
		p.pos = start
		return
	}
	p.pos++ // ','
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		return n, 0, true, true
	}
	m, gotM := readNum()
	if !gotM || p.pos >= len(p.src) || p.src[p.pos] != '}' {
		p.pos = start
		return
	}
	p.pos++
	if n > m {
		p.pos = start
		return
	}
	return n, m, false, true
}

func (p *parser) parseAtom() (*Atom, error) {
	pos := p.pos
	c := p.src[p.pos]
	switch c {
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass(pos)
	case '.':
		p.pos++
		return &Atom{Kind: KindAny, Pos: pos, End: p.pos}, nil
	case '^':
		p.pos++
		return &Atom{Kind: KindAnchor, Anchor: AnchorBOL, Pos: pos, End: p.pos}, nil
	case '$':
		p.pos++
		return &Atom{Kind: KindAnchor, Anchor: AnchorEOL, Pos: pos, End: p.pos}, nil
	case '\\':
		return p.parseEscapeAtom(pos)
	case '*', '+', '?':
		return nil, p.errf(pos, "quantifier %q with nothing to repeat", string(c))
	case ')':
		return nil, p.errf(pos, "unexpected ')'")
	default:
		r, sz := utf8.DecodeRuneInString(p.src[p.pos:])
		p.pos += sz
		return &Atom{Kind: KindLiteral, Value: r, Pos: pos, End: p.pos}, nil
	}
}

func (p *parser) parseGroup() (*Atom, error) {
	pos := p.pos
	p.pos++ // '('
	node := &Atom{Kind: KindGroup, Pos: pos}
	capture := true
	if p.pos+1 < len(p.src) && p.src[p.pos] == '?' {
		switch p.src[p.pos+1] {
		case ':':
			capture = false
			p.pos += 2
		case '=':
			node.Kind = KindLook
			node.LookAhead = true
			p.pos += 2
			return p.finishLook(node, pos)
		case '!':
			node.Kind = KindLook
			node.LookAhead = true
			node.LookNeg = true
			p.pos += 2
			return p.finishLook(node, pos)
		case '<':
			if p.pos+2 < len(p.src) && p.src[p.pos+2] == '=' {
				node.Kind = KindLook
				p.pos += 3
				return p.finishLook(node, pos)
			}
			if p.pos+2 < len(p.src) && p.src[p.pos+2] == '!' {
				node.Kind = KindLook
				node.LookNeg = true
				p.pos += 3
				return p.finishLook(node, pos)
			}
			return p.parseNamedGroup(node, pos)
		default:
			return nil, p.errf(p.pos, "unsupported group flag %q", p.src[p.pos+1])
		}
	}
	if capture {
		node.Capture = true
		node.Index = len(p.groups) + 1
		p.groups = append(p.groups, Group{Index: node.Index, Pos: pos})
	}
	body, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if p.pos >= len(p.src) || p.src[p.pos] != ')' {
		return nil, p.errf(len(p.src), "unterminated group, missing ')'")
	}
	p.pos++
	node.Target = body
	node.End = p.pos
	return node, nil
}

func (p *parser) parseNamedGroup(node *Atom, pos int) (*Atom, error) {
	p.pos += 2 // (?<
	start := p.pos
	for p.pos < len(p.src) {
		ch := p.src[p.pos]
		if ch == '>' {
			break
		}
		ok := ch == '_' ||
			(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(p.pos > start && ch >= '0' && ch <= '9')
		if !ok {
			return nil, p.errf(p.pos, "invalid character in group name")
		}
		p.pos++
	}
	if p.pos >= len(p.src) || p.pos == start {
		return nil, p.errf(start, "expected group name and '>'")
	}
	name := p.src[start:p.pos]
	p.pos++ // '>'
	if existing, dup := p.names[name]; dup {
		return nil, p.errf(pos, "duplicate group name %q (first at index %d)", name, existing)
	}
	node.Capture = true
	node.Index = len(p.groups) + 1
	node.Name = name
	p.names[name] = node.Index
	p.groups = append(p.groups, Group{Index: node.Index, Name: name, Pos: pos})
	body, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if p.pos >= len(p.src) || p.src[p.pos] != ')' {
		return nil, p.errf(len(p.src), "unterminated named group, missing ')'")
	}
	p.pos++
	node.Target = body
	node.End = p.pos
	return node, nil
}

func (p *parser) finishLook(node *Atom, pos int) (*Atom, error) {
	body, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if p.pos >= len(p.src) || p.src[p.pos] != ')' {
		return nil, p.errf(len(p.src), "unterminated lookaround, missing ')'")
	}
	p.pos++
	node.Target = body
	node.End = p.pos
	min, max, bounded := Width(body)
	_ = min
	if node.LookAhead {
		node.LookMax = -1
	} else {
		if !bounded {
			return nil, p.errf(pos, "lookbehind requires a bounded fixed-width pattern")
		}
		if containsBackref(body) {
			return nil, p.errf(pos, "backreferences are not allowed inside lookbehind")
		}
		node.LookMax = max
	}
	return node, nil
}
