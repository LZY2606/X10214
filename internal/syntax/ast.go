package syntax

import "fmt"

// NodeKind identifies an AST node.
type NodeKind string

const (
	KindEmpty       NodeKind = "empty"
	KindChar        NodeKind = "char"
	KindAny         NodeKind = "any"
	KindClass       NodeKind = "class"
	KindConcat      NodeKind = "concat"
	KindAlt         NodeKind = "alternation"
	KindGroup       NodeKind = "group"
	KindRepeat      NodeKind = "repeat"
	KindAssertStart NodeKind = "anchor_start"
	KindAssertEnd   NodeKind = "anchor_end"
	KindWordBound   NodeKind = "word_boundary"
	KindBackref     NodeKind = "backref"
	KindLook        NodeKind = "lookaround"
	KindAtomic      NodeKind = "atomic"
)

// Atom is one matching unit. R is a decoded rune for valid UTF-8; when the
// input byte is invalid, RawByte is set (>= 0) and R is RuneError. A Class
// entry with RawByte>=0 compares only the single byte.
type Atom struct {
	R       rune
	RawByte int // -1 unless this atom is an invalid UTF-8 byte
}

// InvalidByte builds the atom for an invalid UTF-8 byte.
func InvalidByte(b byte) Atom { return Atom{R: RuneError, RawByte: int(b)} }

// RuneError is the rune recorded for invalid byte atoms.
const RuneError = rune(0xFFFD)

// Range is one element of a character class: either an atom interval
// (Closed==true) or a POSIX/Unicode class predicate.
type Range struct {
	Lo, Hi Atom
	Closed bool   // true for a-c intervals (and singletons)
	Class  string // \d \w \s and POSIX [:alpha:] etc. ("" means plain range)
	Negate bool   // negated class predicate (\D \W \S, [:^...:])
}

// Node is one AST node. IDs are assigned depth-first, deterministic for a
// given (pattern, grammar version), so traces and breakpoints remain valid
// across restarts.
type Node struct {
	ID   int      `json:"id"`
	Kind NodeKind `json:"kind"`

	// char / any / class
	Atom     Atom     `json:"atom,omitempty"`
	Negated  bool     `json:"negated,omitempty"`
	Ranges   []*Range `json:"ranges,omitempty"`
	CaseFold bool     `json:"case_fold,omitempty"` // node-level case-insensitive matching
	DotAll   bool     `json:"dot_all,omitempty"`   // dot matches newline

	// structural nodes
	Children []*Node `json:"children,omitempty"`

	// group
	Capture bool   `json:"capture,omitempty"`
	Index   int    `json:"index,omitempty"` // 1-based capture index; 0 = non-capturing
	Name    string `json:"name,omitempty"`

	// repeat
	Child    *Node `json:"child,omitempty"`
	Min, Max int   `json:"max,omitempty"` // Max==-1 means unbounded
	Greedy   bool  `json:"greedy,omitempty"`
	Possess  bool  `json:"possessive,omitempty"`

	// backref
	BackName string `json:"backref_name,omitempty"`

	// lookaround
	Ahead bool `json:"ahead,omitempty"` // lookahead vs lookbehind
	Pos   bool `json:"pos,omitempty"`   // positive vs negative
	Bound int  `json:"bound,omitempty"` // lookbehind fixed width (bytes); -1 = lookahead

	// anchors
	AtFinalNL bool `json:"accept_final_newline,omitempty"` // $ before a trailing \n
	WBPos     bool `json:"wb_pos,omitempty"`               // word boundary vs not-boundary

	// source coordinates (byte offsets in the pattern)
	From, To int    `json:"from,omitempty"`
	Src      string `json:"src,omitempty"`
}

// Flags records effective matcher flags, propagated by inline groups.
type Flags struct {
	I bool // case-insensitive
	S bool // dot-all
	M bool // multiline: ^ and $ also match at internal line ends
}

// String renders a compact human-readable form for debugging.
func (n *Node) String() string {
	if n == nil {
		return "<nil>"
	}
	switch n.Kind {
	case KindChar:
		if n.Atom.RawByte >= 0 {
			return fmt.Sprintf("byte(%02x)", n.Atom.RawByte)
		}
		return fmt.Sprintf("%q", string(n.Atom.R))
	case KindAny:
		return "."
	case KindClass:
		return n.Src
	case KindConcat:
		return "concat"
	case KindAlt:
		return "alt"
	case KindGroup:
		if n.Capture {
			return fmt.Sprintf("(%d:%s)", n.Index, n.Name)
		}
		return "(?:)"
	case KindRepeat:
		return fmt.Sprintf("rep{%d,%d}", n.Min, n.Max)
	case KindBackref:
		return "\\" + n.BackName
	case KindLook:
		dir, pol := "behind", "!"
		if n.Ahead {
			dir = "ahead"
		}
		if n.Pos {
			pol = "="
		}
		return fmt.Sprintf("(?%s%s)", pol, dir)
	case KindAtomic:
		return "(?>)"
	}
	return string(n.Kind)
}
