// Package syntax defines the versioned regex grammar and builds an AST via a
// recursive descent parser. It never delegates to a host regex engine.
package syntax

import "fmt"

// Version pins the grammar accepted by this build. Every match result carries
// this value together with the Unicode data version actually used.
const Version = "traceroom-regex/v1"

// Flag bits are the inline (?imsUx) modifiers.
const (
	FlagI = 1 << iota // case insensitive (simple case fold via unicode.ToLower)
	FlagM             // ^ and $ also match around internal line boundaries
	FlagS             // dot matches newline
	FlagU             // ungreedy: swap greedy/lazy quantifier meaning
	FlagX             // extended: unescaped whitespace in pattern is ignored
)

// Node is an AST node. SrcLo/SrcHi are byte offsets into the pattern source.
type Node interface {
	srcRange() (int, int)
	nodeKind() string
}

// Seq is concatenation.
type Seq struct {
	Kids         []Node
	SrcLo, SrcHi int
}

// Alt is alternation in documented textual order.
type Alt struct {
	Branches     [][]Node
	SrcLo, SrcHi int
}

// Char is one literal rune.
type Char struct {
	R            rune
	SrcLo, SrcHi int
}

// Class is a bracket character class (or an expanded shorthand such as \d).
type Class struct {
	Negated      bool
	Terms        []ClassTerm
	SrcLo, SrcHi int
}

// ClassTerm is either a rune range (Lo<=Hi, Prop=="") or a property test
// (Prop set, e.g. `\w`, `\pL`), with a per-term negation (e.g. `\P{L}`).
type ClassTerm struct {
	Lo, Hi rune
	Prop   string
	Neg    bool
}

// AnchorKind enumerates zero-width assertions.
type AnchorKind int

const (
	AnchorStart           AnchorKind = iota // \A
	AnchorEnd                               // \z
	AnchorEndHalf                           // \Z : end or before final terminating newline
	AnchorLineStart                         // ^
	AnchorLineEnd                           // $
	AnchorWordBoundary                      // \b
	AnchorNotWordBoundary                   // \B
)

// Anchor is a zero-width assertion.
type Anchor struct {
	Kind         AnchorKind
	SrcLo, SrcHi int
}

// Repeat is a quantified expression.
type Repeat struct {
	Kid          Node
	Min, Max     int // Max == -1 means unbounded
	Greedy       bool
	Possessive   bool
	SrcLo, SrcHi int
}

// Group is a capturing or non-capturing group. CapID is 0 for non-capturing.
type Group struct {
	Kid          Node
	CapID        int
	Name         string
	SrcLo, SrcHi int
}

// Atomic is an atomic group (?>...): once entered, alternatives inside cannot
// be backed out of from outside.
type Atomic struct {
	Kid          Node
	SrcLo, SrcHi int
}

// Flags is (?imsUx:...) or a bare (?imsUx) flag change.
type Flags struct {
	Set, Clear   uint8
	Kid          Node // nil for a bare flag change
	SrcLo, SrcHi int
}

// Look is a lookaround assertion. Behind selects lookbehind; Neg selects the
// negative form.
type Look struct {
	Kid          Node
	Behind, Neg  bool
	SrcLo, SrcHi int
}

// Backref refers to a capture group by number (Name may be set for (?P=n)).
type Backref struct {
	ID           int
	Name         string
	SrcLo, SrcHi int
}

// Dot is the wildcard.
type Dot struct {
	SrcLo, SrcHi int
}

func (n *Seq) srcRange() (int, int)     { return n.SrcLo, n.SrcHi }
func (n *Alt) srcRange() (int, int)     { return n.SrcLo, n.SrcHi }
func (n *Char) srcRange() (int, int)    { return n.SrcLo, n.SrcHi }
func (n *Class) srcRange() (int, int)   { return n.SrcLo, n.SrcHi }
func (n *Anchor) srcRange() (int, int)  { return n.SrcLo, n.SrcHi }
func (n *Repeat) srcRange() (int, int)  { return n.SrcLo, n.SrcHi }
func (n *Group) srcRange() (int, int)   { return n.SrcLo, n.SrcHi }
func (n *Atomic) srcRange() (int, int)  { return n.SrcLo, n.SrcHi }
func (n *Flags) srcRange() (int, int)   { return n.SrcLo, n.SrcHi }
func (n *Look) srcRange() (int, int)    { return n.SrcLo, n.SrcHi }
func (n *Backref) srcRange() (int, int) { return n.SrcLo, n.SrcHi }
func (n *Dot) srcRange() (int, int)     { return n.SrcLo, n.SrcHi }

func (n *Seq) nodeKind() string     { return "seq" }
func (n *Alt) nodeKind() string     { return "alt" }
func (n *Char) nodeKind() string    { return "char" }
func (n *Class) nodeKind() string   { return "class" }
func (n *Anchor) nodeKind() string  { return "anchor" }
func (n *Repeat) nodeKind() string  { return "repeat" }
func (n *Group) nodeKind() string   { return "group" }
func (n *Atomic) nodeKind() string  { return "atomic" }
func (n *Flags) nodeKind() string   { return "flags" }
func (n *Look) nodeKind() string    { return "look" }
func (n *Backref) nodeKind() string { return "backref" }
func (n *Dot) nodeKind() string     { return "dot" }

// Pattern is a fully parsed pattern plus capture metadata.
type Pattern struct {
	Root     Node
	Source   string
	NumCaps  int // number of capture groups (group 0 excluded)
	Names    map[string]int
	CapNames map[int]string
	Flags    uint8
}

// Range exposes source offsets for formatting.
func RangeOf(n Node) (int, int) { return n.srcRange() }

// Describe renders an anchor kind for UI/tests.
func (a AnchorKind) String() string {
	switch a {
	case AnchorStart:
		return "\\A"
	case AnchorEnd:
		return "\\z"
	case AnchorEndHalf:
		return "\\Z"
	case AnchorLineStart:
		return "^"
	case AnchorLineEnd:
		return "$"
	case AnchorWordBoundary:
		return "\\b"
	case AnchorNotWordBoundary:
		return "\\B"
	}
	return fmt.Sprintf("anchor(%d)", int(a))
}
