package syntax

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseError 携带源码字节位置。
type ParseError struct {
	Pos int
	Msg string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("pattern:%d: %s", e.Pos, e.Msg)
}

// ParseResult 包含 AST、命名组与版本钉。
type ParseResult struct {
	AST        *Node
	NumGroups  int
	Names      map[string]int
	Syntax     string
	UnicodeVer string
	Raw        string
}

type parser struct {
	src string
	pos int

	nextID int
	groups []string // 1-based 索引 -> 名字（匿名组为空）

	backrefs []backrefRef
}

type backrefRef struct {
	node *Node
	name string
	num  int
}

// Parse 按 CTR-1.0 语法解析 pattern。
func Parse(pattern string) (*ParseResult, error) {
	p := &parser{src: pattern, nextID: 1}
	ast, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.src) {
		return nil, &ParseError{Pos: p.pos, Msg: "unexpected trailing characters"}
	}
	if err := p.resolveBackrefs(); err != nil {
		return nil, err
	}
	names := make(map[string]int, len(p.groups))
	for i, n := range p.groups {
		if n != "" {
			names[n] = i + 1
		}
	}
	return &ParseResult{
		AST:        ast,
		NumGroups:  len(p.groups),
		Names:      names,
		Syntax:     SyntaxVersion,
		UnicodeVer: UnicodeDataVersion,
		Raw:        pattern,
	}, nil
}

func (p *parser) newNode(t NodeType, from, to int) *Node {
	n := &Node{Type: t, ID: p.nextID, SrcFrom: from, SrcTo: to}
	p.nextID++
	return n
}

func (p *parser) errf(at int, format string, args ...any) error {
	return &ParseError{Pos: at, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) peek() int {
	if p.pos >= len(p.src) {
		return -1
	}
	return int(p.src[p.pos])
}

func (p *parser) parseAlternation() (*Node, error) {
	from := p.pos
	var alts []*Node
	first, err := p.parseSequence()
	if err != nil {
		return nil, err
	}
	alts = append(alts, first)
	for p.peek() == '|' {
		p.pos++
		a, err := p.parseSequence()
		if err != nil {
			return nil, err
		}
		alts = append(alts, a)
	}
	if len(alts) == 1 {
		return alts[0], nil
	}
	n := p.newNode(NAlt, from, p.pos)
	n.Children = alts
	return n, nil
}

func (p *parser) parseSequence() (*Node, error) {
	from := p.pos
	var items []*Node
	for {
		c := p.peek()
		if c == -1 || c == '|' || c == ')' {
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
		if atom.Type != NEmpty {
			items = append(items, atom)
		}
	}
	switch len(items) {
	case 0:
		return p.newNode(NEmpty, from, p.pos), nil
	case 1:
		return items[0], nil
	default:
		n := p.newNode(NConcat, from, p.pos)
		n.Children = items
		return n, nil
	}
}

func (p *parser) parseQuantifier(atom *Node) (*Node, error) {
	c := p.peek()
	q, ok := Quant{}, false
	from := p.pos
	switch {
	case c == '*':
		p.pos++
		q, ok = Quant{Min: 0, Max: -1}, true
	case c == '+':
		p.pos++
		q, ok = Quant{Min: 1, Max: -1}, true
	case c == '?':
		p.pos++
		q, ok = Quant{Min: 0, Max: 1}, true
	case c == '{':
		qq, has, err := p.tryBraceQuant()
		if err != nil {
			return nil, err
		}
		if has {
			q, ok = qq, true
		}
	}
	if !ok {
		return atom, nil
	}
	if atom.Type == NAnchor || atom.Type == NLookahead || atom.Type == NLookbehind {
		return nil, p.errf(from, "quantifier not allowed on %s", atom.Type)
	}
	if p.peek() == '?' {
		p.pos++
		q.Lazy = true
	}
	if q.Max >= 0 && q.Max < q.Min {
		return nil, p.errf(from, "invalid quantifier: max < min")
	}
	n := p.newNode(NRepeat, from, p.pos)
	n.Child = atom
	n.Quant = q
	return n, nil
}

func (p *parser) tryBraceQuant() (Quant, bool, error) {
	start := p.pos // '{'
	j := start + 1
	for j < len(p.src) && p.src[j] >= '0' && p.src[j] <= '9' {
		j++
	}
	if j == start+1 {
		return Quant{}, false, nil
	}
	minV, _ := strconv.Atoi(p.src[start+1 : j])
	maxV, bounded := minV, true
	k := j
	if k < len(p.src) && p.src[k] == ',' {
		k++
		d0 := k
		for k < len(p.src) && p.src[k] >= '0' && p.src[k] <= '9' {
			k++
		}
		if k == d0 {
			bounded = false
			maxV = -1
		} else {
			maxV, _ = strconv.Atoi(p.src[d0:k])
		}
	}
	if k >= len(p.src) || p.src[k] != '}' {
		return Quant{}, false, nil // 不是合法量词，{ 按字面量处理
	}
	p.pos = k + 1
	return Quant{Min: minV, Max: maxV, HasBound: bounded}, true, nil
}

func (p *parser) parseAtom() (*Node, error) {
	c := p.peek()
	from := p.pos
	switch c {
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '.':
		p.pos++
		n := p.newNode(NCharClass, from, p.pos)
		n.Ranges = ComplementRanges([]RuneRange{{Lo: '\n', Hi: '\n'}, {Lo: '\r', Hi: '\r'}})
		return n, nil
	case '^':
		p.pos++
		return p.anchorNode(from, "bos"), nil
	case '$':
		p.pos++
		return p.anchorNode(from, "eos"), nil
	case '\\':
		return p.parseEscape()
	case '*', '+', '?':
		return nil, p.errf(from, "unexpected quantifier %q", string(byte(c)))
	case ')', ']', '}':
		return nil, p.errf(from, "unexpected %q", string(byte(c)))
	case '|', -1:
		return p.newNode(NEmpty, from, from), nil
	default:
		r, size := decodeOneRune(p.src, p.pos)
		p.pos += size
		n := p.newNode(NCharClass, from, p.pos)
		n.Ranges = []RuneRange{{Lo: r, Hi: r}}
		n.Single = true
		return n, nil
	}
}

func (p *parser) anchorNode(from int, kind string) *Node {
	n := p.newNode(NAnchor, from, p.pos)
	n.AnchorKind = kind
	return n
}

func (p *parser) parseGroup() (*Node, error) {
	from := p.pos
	p.pos++ // '('
	negative := false
	if p.peek() == '?' {
		p.pos++
		c := p.peek()
		switch {
		case c == ':':
			p.pos++
			body, err := p.parseAlternation()
			if err != nil {
				return nil, err
			}
			if err := p.expectClose(from); err != nil {
				return nil, err
			}
			return body, nil
		case c == '=':
			p.pos++
			return p.parseLook(from, NLookahead, false)
		case c == '!':
			p.pos++
			return p.parseLook(from, NLookahead, true)
		case c == '<' && p.pos+1 < len(p.src) && (p.src[p.pos+1] == '=' || p.src[p.pos+1] == '!'):
			negative = p.src[p.pos+1] == '!'
			p.pos += 2
			return p.parseLook(from, NLookbehind, negative)
		case c == '<':
			return p.parseNamedCapture(from)
		default:
			return nil, p.errf(from, "unsupported group syntax '?%c'", c)
		}
	}
	// 普通捕获组
	idx := p.addGroup("")
	n := p.newNode(NCapture, from, from)
	n.Index = idx
	body, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if err := p.expectClose(from); err != nil {
		return nil, err
	}
	n.Child = body
	n.SrcTo = p.pos
	return n, nil
}

func (p *parser) parseNamedCapture(from int) (*Node, error) {
	p.pos++ // '<'
	nameStart := p.pos
	for p.pos < len(p.src) {
		b := p.src[p.pos]
		if b == '>' {
			break
		}
		ok := b == '_' || (b >= '0' && b <= '9') ||
			(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
		if !ok {
			return nil, p.errf(p.pos, "invalid character in group name")
		}
		p.pos++
	}
	name := p.src[nameStart:p.pos]
	if name == "" {
		return nil, p.errf(nameStart, "empty capture group name")
	}
	if p.peek() != '>' {
		return nil, p.errf(p.pos, "unterminated group name")
	}
	p.pos++ // '>'
	for i, ex := range p.groups {
		if ex == name {
			return nil, p.errf(from, "duplicate capture group name %q (index %d)", name, i+1)
		}
	}
	idx := p.addGroup(name)
	n := p.newNode(NCapture, from, from)
	n.Index = idx
	n.Name = name
	body, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if err := p.expectClose(from); err != nil {
		return nil, err
	}
	n.Child = body
	n.SrcTo = p.pos
	return n, nil
}

func (p *parser) parseLook(from int, t NodeType, neg bool) (*Node, error) {
	body, err := p.parseAlternation()
	if err != nil {
		return nil, err
	}
	if err := p.expectClose(from); err != nil {
		return nil, err
	}
	n := p.newNode(t, from, p.pos)
	n.Child = body
	n.Negative = neg
	return n, nil
}

func (p *parser) expectClose(from int) error {
	if p.peek() != ')' {
		return p.errf(p.pos, "missing ')' for group opened at %d", from)
	}
	p.pos++
	return nil
}

func (p *parser) addGroup(name string) int {
	p.groups = append(p.groups, name)
	return len(p.groups)
}

func (p *parser) resolveBackrefs() error {
	for _, br := range p.backrefs {
		if br.name != "" {
			idx := 0
			for i, n := range p.groups {
				if n == br.name {
					idx = i + 1
					break
				}
			}
			if idx == 0 {
				return p.errf(br.node.SrcFrom, "unknown named reference %q", br.name)
			}
			br.node.RefIndex = idx
		} else {
			if br.num > len(p.groups) {
				return p.errf(br.node.SrcFrom, "backreference %d exceeds number of groups (%d)", br.num, len(p.groups))
			}
			br.node.RefIndex = br.num
		}
	}
	return nil
}

var _ = strings.TrimSpace
