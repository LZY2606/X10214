// Package ast defines the syntax tree for the trajectory-room regex dialect.
package ast

// Node is implemented by every AST node.
type Node interface{ node() }

// Src describes the half-open byte interval of a node inside the pattern.
type Src struct {
	From int
	To   int
	ID   int
}

// Char matches one literal rune.
type Char struct {
	Src
	Rune rune
}

// Dot matches any rune except '\n'.
type Dot struct{ Src }

// ClassItem is anything allowed inside a character class.
type ClassItem interface{ classItem() }

// RuneRange matches Lo..Hi inclusive.
type RuneRange struct {
	Lo rune
	Hi rune
}

// PerlClass is one of d D w W s S.
type PerlClass struct {
	Kind byte
	Neg  bool
}

// UnicodeProp is \p{Name} / \P{Name}.
type UnicodeProp struct {
	Name string
	Neg  bool
}

// Class is a possibly negated bracket expression.
type Class struct {
	Src
	Items  []ClassItem
	Negate bool
}

// Concat matches its items in order.
type Concat struct {
	Src
	Items []Node
}

// Alt matches the first successful alternative in document order.
type Alt struct {
	Src
	Options []Node
}

// Repeat applies a quantified expression. Max==-1 means unbounded.
type Repeat struct {
	Src
	Expr   Node
	Min    int
	Max    int
	Greedy bool
}

// Anchor kinds.
const (
	AnchorStart    = '^'
	AnchorEnd      = '$'
	AnchorWord     = 'b'
	AnchorNotWord  = 'B'
)

// Anchor is a zero-width assertion.
type Anchor struct {
	Src
	Kind byte
}

// Backref references a capturing group by number or name.
type Backref struct {
	Src
	Index int
	Name  string
}

// Look kinds.
const (
	LookAhead         = '>' // (?=  / (?!
	LookBehind        = '<' // (?<= / (?<!
)

// Group is capturing, non-capturing, atomic or a lookaround.
type Group struct {
	Src
	Index int // 0 for non-capturing
	Name  string
	Body  Node
	// Look is 0 for ordinary groups, otherwise LookAhead/LookBehind.
	Look byte
	Neg  bool
	// Atomic is an atomic group (?>...).
	Atomic bool
}

func (*Char) node()    {}
func (*Dot) node()     {}
func (*Class) node()   {}
func (*Concat) node()  {}
func (*Alt) node()     {}
func (*Repeat) node()  {}
func (*Anchor) node()  {}
func (*Backref) node() {}
func (*Group) node()   {}

func (RuneRange) classItem()   {}
func (*PerlClass) classItem()  {}
func (*UnicodeProp) classItem() {}

// GroupInfo describes a capturing group.
type GroupInfo struct {
	Index int
	Name  string
	Src   Src
}

// Tree is a parsed pattern.
type Tree struct {
	Root       Node
	Groups     []GroupInfo
	Named      map[string]int
	NodeByID   map[int]Node
	Sources    map[int]Src
	PatternLen int
}
