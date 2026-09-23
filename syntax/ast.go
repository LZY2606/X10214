// Package syntax defines the pinned grammar/Unicode versions, the regex AST,
// the recursive-descent parser and the bytecode compiler used by the engine.
//
// Nothing in this package matches input: it only turns pattern source into a
// Program. Matching is performed exclusively by package engine.
package syntax

// GrammarVersion pins the documented dialect ("捕获轨迹室" dialect, revision 1).
const GrammarVersion = "CTR-Grammar/1.0"

// UnicodeVersion pins the Unicode data version. The tables come from the Go
// standard library (unicode, unicode/utf8) whose embedded edition is 15.0.0.
const UnicodeVersion = "15.0.0"

// Flags are dialect toggles attached to a whole pattern.
type Flags struct {
	I bool // (?i): case-insensitive (Unicode simple case folding)
	M bool // (?m): ^/$ also match at line boundaries
	S bool // (?s): dot matches \n
}

// Node is an AST node.
type Node interface{ node() }

type Literal struct{ R rune }
type CharClass struct {
	Negate bool
	Terms  []ClassTerm
}
type Anchor struct{ Kind AnchorKind } // ^ $ \b \B \A \z \Z
type Concat struct{ Items []Node }
type Alt struct{ Branches []Node }
type Group struct {
	Capture bool
	Index   int // 0 when Capture is false
	Name    string
	IndexReset bool // (?|...)
	Flags     *Flags // (?i:...)
	Body      Node
}
type Ref struct {
	Index int
	Name  string
}
type Quant struct {
	Child     Node
	Min, Max  int  // Max == -1 means unbounded
	Greedy    bool
	Possessive bool
	Empty     bool // child provably matches the empty string
	Guard     int  // unique id assigned by the compiler for empty-repeat guards
}
type Lookaround struct {
	Negative bool
	Behind   bool
	Body     Node
}
type Atomic struct{ Body Node } // (?>...)

type AnchorKind int

const (
	AnchorStartText AnchorKind = iota // \A
	AnchorEndText                     // \z
	AnchorEndTextOptNL                // \Z
	AnchorStartLine                   // ^
	AnchorEndLine                     // $
	AnchorWordBoundary                // \b
	AnchorNotWordBoundary             // \B
)

func (*Literal) node()    {}
func (*CharClass) node()  {}
func (*Anchor) node()     {}
func (*Concat) node()     {}
func (*Alt) node()        {}
func (*Group) node()      {}
func (*Ref) node()        {}
func (*Quant) node()      {}
func (*Lookaround) node() {}
func (*Atomic) node()     {}

// ClassTerm is one element of a character class: a single rune, a range,
// a shorthand (\d \w \s and their negations) or a Unicode property.
type ClassTerm struct {
	Kind  ClassTermKind
	Lo, Hi rune // for Range and Single (Lo==Hi)
	Unicode string // property name for UnicodeTerm, e.g. "Letter", "Greek"
	ASCII  bool   // \d \w \s use ASCII semantics by default
}

type ClassTermKind int

const (
	ClassSingle ClassTermKind = iota
	ClassRange
	ClassDigit
	ClassNotDigit
	ClassWord
	ClassNotWord
	ClassSpace
	ClassNotSpace
	ClassUnicode // \p{...}
	ClassNotUnicode // \P{...}
)
