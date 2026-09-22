// Package regex implements a self-contained regular expression engine with
// explicit capture-history tracing and resource budgets. It does not use the
// standard library regexp package for matching.
package regex

// SyntaxVersion is pinned into every run result so persisted outcomes can be
// interpreted against the exact syntax/semantics that produced them.
const SyntaxVersion = "1.0.0"

// Kind identifies an AST node kind.
type Kind int

const (
	KEmpty   Kind = iota // matches the empty string
	KLiteral             // a single rune (Fold enables case-insensitive match)
	KClass               // character class
	KAny                 // '.' (DotAll selects whether '\n' is matched)
	KConcat              // sequence of children
	KAlt                 // alternation of children, left branch preferred
	KRepeat              // Min..Max repetitions (-1 = unbounded), Greedy or lazy
	KGroup               // group: Index > 0 capturing, Index == 0 non-capturing
	KBackref             // backreference to group Index
	KAnchor              // zero-width anchor of kind Anchor
	KLook                // lookaround: Ahead/behind, Negative/positive
)

// AnchorKind selects a zero-width assertion.
type AnchorKind int

const (
	ABeginText AnchorKind = iota // \A
	AEndText                     // \z
	ABeginLine                   // ^ with multiline
	AEndLine                     // $ with multiline
	AWordBoundary                // \b
	ANonWordBoundary             // \B
)

// Class is a set of rune ranges; Negate flips membership.
type Class struct {
	Negate bool       `json:"negate"`
	Ranges [][2]rune  `json:"ranges"`
}

// Node is one AST node.
type Node struct {
	Kind     Kind     `json:"kind"`
	Rune     rune     `json:"rune,omitempty"`
	Fold     bool     `json:"fold,omitempty"`
	Class    *Class   `json:"class,omitempty"`
	DotAll   bool     `json:"dotAll,omitempty"`
	Children []*Node  `json:"children,omitempty"`
	Min      int      `json:"min,omitempty"`
	Max      int      `json:"max,omitempty"`
	Greedy   bool     `json:"greedy,omitempty"`
	Index    int      `json:"index,omitempty"`
	Name     string   `json:"name,omitempty"`
	Anchor   AnchorKind `json:"anchor,omitempty"`
	Ahead    bool     `json:"ahead,omitempty"`
	Negative bool     `json:"negative,omitempty"`
}

func kindName(k Kind) string {
	switch k {
	case KEmpty:
		return "empty"
	case KLiteral:
		return "literal"
	case KClass:
		return "class"
	case KAny:
		return "any"
	case KConcat:
		return "concat"
	case KAlt:
		return "alt"
	case KRepeat:
		return "repeat"
	case KGroup:
		return "group"
	case KBackref:
		return "backref"
	case KAnchor:
		return "anchor"
	case KLook:
		return "look"
	}
	return "unknown"
}

// KindName exposes a stable string name for UI rendering.
func (n *Node) KindName() string { return kindName(n.Kind) }
