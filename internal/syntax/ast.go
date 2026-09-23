// Package syntax parses Capture Trace Regex (CTR-1.0) patterns into an AST.
package syntax

// SyntaxVersion pins the grammar every result is interpreted under.
const SyntaxVersion = "CTR-1.0"

// Kind identifies an AST node kind.
type Kind string

const (
	KindConcat   Kind = "concat"
	KindAlt      Kind = "alternation"
	KindQuant    Kind = "quantifier"
	KindClass    Kind = "charclass"
	KindLiteral  Kind = "literal"
	KindAny      Kind = "any"
	KindAnchor   Kind = "anchor"
	KindGroup    Kind = "group"
	KindBackref  Kind = "backref"
	KindLook     Kind = "lookaround"
	KindProperty Kind = "property"
)

// Quant values.
type Quant int

const (
	Greedy Quant = 1
	Lazy   Quant = -1
)

// Anchor kinds.
type AnchorKind string

const (
	AnchorBOL AnchorKind = "bol" // ^
	AnchorEOL AnchorKind = "eol" // $
	AnchorWB  AnchorKind = "wb"  // \b
	AnchorNWB AnchorKind = "nwb" // \B
)

// Atom is a single AST node.
type Atom struct {
	Kind      Kind
	Pos       int // pattern byte offset where the node starts
	End       int
	Value     rune          // literal
	Anchor    AnchorKind    // anchor
	Members   []ClassMember // class members
	Negated   bool          // class or property negation
	Min       int           // quantifier min
	Max       int           // quantifier max (-1 = infinity)
	Mode      Quant         // quantifier greediness
	Target    *Atom         // quantifier target / group body / lookaround body
	Index     int           // capture index / backref target
	Name      string        // named group
	Capture   bool          // group captures
	LookNeg   bool          // negative lookaround
	LookAhead bool          // lookahead vs lookbehind
	LookMax   int           // lookbehind bounded max width (-1 unbounded)
	Prop      string        // \p{...}
	Nodes     []*Atom       // concat / alternation children
}

// Group describes one capture slot produced by the parser.
type Group struct {
	Index int
	Name  string
	Pos   int
}

// Pattern is the fully parsed result.
type Pattern struct {
	Root      *Atom
	Groups    []Group
	NumGroups int // number of capture groups (index 0 is whole match)
}
