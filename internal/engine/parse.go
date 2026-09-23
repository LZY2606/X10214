package engine

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ParseError pins a grammar failure to a source offset.
type ParseError struct {
	Pos int
	Msg string
}

func (e *ParseError) Error() string { return fmt.Sprintf("pattern error at %d: %s", e.Pos, e.Msg) }

type parser struct {
	src   string
	pos   int
	flags FlagState
	group int
	names map[string]int
	groups []GroupInfo
}

// Parse compiles no instructions; it only produces the AST and capture table.
func Parse(pattern string) (*Pattern, error) {
	p := &parser{
		src:    pattern,
		names:  map[string]int{},
		groups: []GroupInfo{{Index: 0}},
	}
	root, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.src) {
		return nil, &ParseError{Pos: p.pos, Msg: "unexpected trailing characters"}
	}
	return &Pattern{Root: root, Groups: p.groups, Names: p.names, Source: pattern, Flags: p.flags}, nil
}

func (p *parser) errf(at int, format string, a ...any) error {
	return &ParseError{Pos: at, Msg: fmt.Sprintf(format, a...)}
}

func (p *parser) peek() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) eat(c byte) bool {
	if p.peek() == c {
		p.pos++
		return true
	}
	return false
}

// parseSeq parses until ) | or end, restoring flags on exit so that a bare
// flag directive scopes to the remainder of this sequence only.
func (p *parser) parseSeq() (Node, error) {
	saved := p.flags
	begin := p.pos
	var nodes []Node
	for {
		c := p.peek()
		if c == 0 || c == '|' || c == ')' {
			break
		}
		n, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	p.flags = saved
	return p.mkConcat(begin, p.pos, nodes), nil
}

func (p *parser) parseAlt() (Node, error) {
	begin := p.pos
	first, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	if p.peek() != '|' {
		return first, nil
	}
	branches := []Node{first}
	for p.eat('|') {
		b, err := p.parseSeq()
		if err != nil {
			return nil, err
		}
		branches = append(branches, b)
	}
	return &Alt{baseNode: baseNode{begin, p.pos}, Branches: branches}, nil
}

func (p *parser) mkConcat(s, e int, nodes []Node) Node {
	if len(nodes) == 0 {
		return &Concat{baseNode: baseNode{s, e}}
	}
	if len(nodes) == 1 {
		return nodes[0]
	}
	return &Concat{baseNode: baseNode{s, e}, Nodes: nodes}
}
