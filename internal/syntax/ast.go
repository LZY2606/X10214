package syntax

// Node is a node in the parsed regular-expression abstract syntax tree.
// Every node carries the byte offset at which it starts in the source
// pattern so that diagnostics and the UI can point back at the grammar.
type Node interface {
	Pos() int
	nodeMarker()
}

// Flags are compile-time on/off switches for the PCRE-like dialect.
type Flags struct {
	I bool // case-insensitive (ASCII letters fold)
	S bool // dotall: '.' matches newline
	M bool // multiline: ^/$ also match at internal line terminators
}

// Mask indicates which of the flag bits are present in a (?...) construct.
type Mask struct {
	I, S, M bool
}

// CharNode matches a single Unicode code point. Rune 0xFFFD with Raw set
// denotes an invalid UTF-8 byte consumed as one replacement unit.
type CharNode struct {
	Offset int
	Rune   rune
	Raw    byte // nonzero when Rune is an invalid-UTF-8 replacement unit
}

// DotNode matches any code point except newline unless the S flag is set.
type DotNode struct{ Offset int }

// ClassNode matches against a bracketed character class.
type ClassNode struct {
	Offset int
	Negate bool
	Items  []ClassItem
}

// ClassItem is either a single rune or an inclusive rune range.
type ClassItem struct {
	Lo, Hi   rune
	RawLo    byte   // nonzero for invalid-UTF-8 singleton represented as 0xFFFD
	Short    byte   // 'd','D','w','W','s','S' shorthand, 0 when a rune range
	Prop     string // Unicode property like "L" or "Greek", empty for range
	PropNeg  bool   // \P{...}
}

// ShorthandNode is a standalone \d \D \w \W \s \S atom.
type ShorthandNode struct {
	Offset int
	Kind   byte
}

// PropNode is a standalone \p{...} or \P{...} atom.
type PropNode struct {
	Offset int
	Name   string
	Negate bool
}

// AnchorNode is a zero-width position assertion.
type AnchorNode struct {
	Offset int
	Kind   AnchorKind
}

type AnchorKind int

const (
	AnchorStart AnchorKind = iota // ^
	AnchorEnd                     // $
	AnchorWord                    // \b
	AnchorNotWord                 // \B
)

// GroupNode is a parenthesised subexpression.
type GroupNode struct {
	Offset  int
	Index   int // 1-based capture number, 0 for non-capturing groups
	Name    string
	Capture bool
	Set     Flags // flags switched on by (?...)
	Clear   Flags // flags switched off by (?-...)
	Mask    Mask
	Body    Node // nil for a flag-only group
}

// ConcatNode is an ordered sequence of child nodes.
type ConcatNode struct {
	Offset int
	Items  []Node
}

// AlternationNode chooses between alternatives in documented order.
type AlternationNode struct {
Offset       int
	Alternatives []Node
}

// QuantNode applies a quantifier to its child.
type QuantNode struct {
	Offset int
	Child  Node
	Min    int
	Max    int // -1 means unbounded
	Greedy bool
}

// BackrefNode refers back to a previously captured group, by number or name.
type BackrefNode struct {
	Offset int
	Index  int
	Name   string
}

// StrNode is a fused run of literal runes produced by the optimizer.
// It never appears in parser output.
type StrNode struct {
	Offset int
	Runes  string
	Flags  Flags
}

// LookaroundNode is a lookahead/lookbehind assertion.
type LookaroundNode struct {
	Offset int
	Behind bool
	Negate bool
	Body   Node
}

func (n *CharNode) Pos() int        { return n.Offset }
func (n *DotNode) Pos() int         { return n.Offset }
func (n *ClassNode) Pos() int       { return n.Offset }
func (n *AnchorNode) Pos() int      { return n.Offset }
func (n *GroupNode) Pos() int       { return n.Offset }
func (n *ConcatNode) Pos() int      { return n.Offset }
func (n *AlternationNode) Pos() int { return n.Offset }
func (n *QuantNode) Pos() int       { return n.Offset }
func (n *BackrefNode) Pos() int     { return n.Offset }
func (n *LookaroundNode) Pos() int { return n.Offset }
func (n *ShorthandNode) Pos() int  { return n.Offset }
func (n *PropNode) Pos() int       { return n.Offset }
func (n *StrNode) Pos() int        { return n.Offset }

func (*CharNode) nodeMarker()        {}
func (*DotNode) nodeMarker()         {}
func (*ClassNode) nodeMarker()       {}
func (*AnchorNode) nodeMarker()      {}
func (*GroupNode) nodeMarker()       {}
func (*ConcatNode) nodeMarker()      {}
func (*AlternationNode) nodeMarker() {}
func (*QuantNode) nodeMarker()       {}
func (*BackrefNode) nodeMarker()     {}
func (*LookaroundNode) nodeMarker()  {}
func (*ShorthandNode) nodeMarker()   {}
func (*PropNode) nodeMarker()        {}
func (*StrNode) nodeMarker()         {}

// Walk visits every node in depth-first order; visiting stops if f returns
// an error and that error is returned to the caller.
func Walk(n Node, f func(Node) error) error {
	if n == nil {
		return nil
	}
	if err := f(n); err != nil {
	return err
	}
	switch t := n.(type) {
	case *ConcatNode:
		for _, c := range t.Items {
			if err := Walk(c, f); err != nil {
				return err
			}
		}
	case *AlternationNode:
		for _, a := range t.Alternatives {
			if err := Walk(a, f); err != nil {
				return err
			}
		}
	case *QuantNode:
		if err := Walk(t.Child, f); err != nil {
			return err
		}
	case *GroupNode:
		if err := Walk(t.Body, f); err != nil {
			return err
		}
	case *LookaroundNode:
		if err := Walk(t.Body, f); err != nil {
			return err
		}
	}
	return nil
}
