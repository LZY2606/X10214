// Package syntax defines the AST and parser for the CTR-1.0 regular
// expression dialect. Every produced program is tagged with a syntax
// version and the Unicode data version it was validated against.
package syntax

import "unicode"

// SyntaxVersion is the dialect revision understood by this build.
const SyntaxVersion = "CTR-1.0"

// UnicodeVersion is the Unicode database revision whose semantics the
// parser/compiler rely on. It follows the Go runtime's bundled tables.
const UnicodeVersion = unicode.Version

// Node is any AST node.
type Node interface {
	ID() int
	setID(int)
	SourceRange() (off, end int) // byte offsets inside the pattern source
}

type baseNode struct {
	id  int
	off int
	end int
}

func (b *baseNode) ID() int                     { return b.id }
func (b *baseNode) setID(id int)                { b.id = id }
func (b *baseNode) SourceRange() (int, int)     { return b.off, b.end }

// Flags is the modifier set active for a node.
type Flags struct {
	I bool // case-insensitive
	M bool // multiline
	S bool // dotall
}

// Empty matches the zero-width string (e.g. empty alternative).
type Empty struct{ baseNode }

// Literal matches one atom (valid rune, or the invalid-byte sentinel).
type Literal struct {
	baseNode
	R     rune
	Bad   bool // true when R represents one invalid UTF-8 byte
	Flags Flags
}

// ClassRange is an inclusive [Lo,Hi] rune interval inside a character class.
type ClassRange struct {
	Lo, Hi rune
}

// Class is a bracketed character class (or the compiled '.' node).
type Class struct {
	baseNode
	Negate bool
	Ranges []ClassRange // rune intervals (never the bad-byte sentinel)
	Perl   []byte       // 'd','D','w','W','s','S' added inside the class
	Bad    bool         // class explicitly matches invalid UTF-8 bytes
	Dot    bool         // this node is '.'
	Flags  Flags
}

// Anchor kinds.
const (
	AnchorStartText = 'A' // \A
	AnchorEndText   = 'z' // \z
	AnchorEndOptNL  = 'Z' // \Z
	AnchorStartLine = '^'
	AnchorEndLine   = '$'
)

// Anchor is a zero-width position assertion.
type Anchor struct {
	baseNode
	Kind  byte
	Flags Flags
}

// WordBoundary is \b or \B.
type WordBoundary struct {
	baseNode
	Negate bool // true == \B
	Flags  Flags
}

// Group is a capturing/non-capturing group, including lookaround/atomic.
type Group struct {
	baseNode
	Index    int // 1-based capture slot; 0 for non-capturing
	Name     string
	Body     Node
	Flags    Flags
	Atomic   bool
	Look     byte // 0, '=', '!', '<' (positive lookbehind), '>' (negative lookbehind)
	Assertion bool
}

// Repeat is a quantified expression. Greedy=false means lazy (*?).
type Repeat struct {
	baseNode
	Child  Node
	Min    int
	Max    int // -1 == unbounded
	Greedy bool
}

// Alternation alternatives are tried strictly in source order.
type Alternation struct {
	baseNode
	Branches []Node
}

// Concat is a sequence of nodes.
type Concat struct {
	baseNode
	Items []Node
}

// Backref is \1 / \g{1} / \k<name>.
type Backref struct {
	baseNode
	Ref   int // resolved 1-based capture index
	Name  string // original reference text
	Flags Flags
}

// Program is a fully parsed pattern.
type Program struct {
	Pattern  string
	Root     Node
	NumCaps  int
	Names    map[string]int
	SyntaxV  string
	UnicodeV string
}
