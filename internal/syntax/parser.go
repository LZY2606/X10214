package syntax

import (
	"fmt"
	"unicode/utf8"
)

// ParseError describes a grammar failure with a rune offset.
type ParseError struct {
	Pos    int    `json:"pos"` // byte offset in source
	RunePos int  `json:"runePos"`
	Msg    string `json:"msg"`
}

func (e *ParseError) Error() string { return fmt.Sprintf("pattern error at rune %d: %s", e.RunePos, e.Msg) }

type parser struct {
	src    string
	runes  []rune
	pos    int // rune index
	byteAt []int // byte offset of each rune
	groups []Group
	nextID int
	flags  FlagSet
}

// Parse compiles the pattern text into an AST with capture metadata.
func Parse(src string) (*Pattern, error) {
	p := &parser{src: src, flags: FlagSet{U: true}}
	p.runes = []rune(src)
	p.byteAt = make([]int, len(p.runes)+1)
	off := 0
	for i, r := range p.runes {
		p.byteAt[i] = off
		off += utf8.RuneLen(r)
	}
	p.byteAt[len(p.runes)] = off
	// Slot 0 is the implicit whole-match group.
	p.groups = []Group{{Index: 0}}
	root, err := p.parseAlternation(p.flags)
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.runes) {
		return nil, p.errf("unexpected character %q", p.runes[p.pos])
	}
	if root == nil {
		root = p.newNode(NEmpty)
	}
	return &Pattern{
		Version: SyntaxVersion,
		Root:    root,
		Groups:  p.groups,
		Flags:   p.flags,
		Source:  src,
	}, nil
}

func (p *parser) errf(format string, args ...any) error {
	off := len(p.src)
	if p.pos < len(p.byteAt) {
		off = p.byteAt[p.pos]
	}
	return &ParseError{Pos: off, RunePos: p.pos, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) newNode(k NodeKind) *Node {
	p.nextID++
	n := &Node{ID: p.nextID, Kind: k}
	n.Max = -1
	return n
}

func (p *parser) peek() (rune, bool) {
	if p.pos >= len(p.runes) {
		return 0, false
	}
	return p.runes[p.pos], true
}

func (p *parser) eat(r rune) bool {
	if c, ok := p.peek(); ok && c == r {
		p.pos++
		return true
	}
	return false
}

// parseAlternation: sequence ('|' sequence)*
func (p *parser) parseAlternation(flags FlagSet) (*Node, error) {
	var branches []*Node
	for {
		seq, err := p.parseSequence(flags)
		if err != nil {
			return nil, err
		}
		if seq == nil {
			seq = p.newNode(NEmpty)
		}
		branches = append(branches, seq)
		if !p.eat('|') {
			break
		}
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	n := p.newNode(NAlt)
	n.Children = branches
	return n, nil
}

// parseSequence consumes atoms until it hits | ) or end.
func (p *parser) parseSequence(flags FlagSet) (*Node, error) {
	var items []*Node
	for {
		r, ok := p.peek()
		if !ok || r == '|' || r == ')' {
			break
		}
		atom, err := p.parseAtom(flags)
		if err != nil {
			return nil, err
		}
		atom, err = p.parseQuantifier(atom, flags)
		if err != nil {
			return nil, err
		}
		items = append(items, atom)
	}
	if len(items) == 0 {
		return nil, nil
	}
	if len(items) == 1 {
		return items[0], nil
	}
	n := p.newNode(NConcat)
	n.Children = items
	return n, nil
}
