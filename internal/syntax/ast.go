// Package syntax parses patterns into an AST. It never uses the regexp
// package; every construct is handled by the project's own execution model.
package syntax

import "unicode"

// SyntaxVersion is pinned into every execution result.
const SyntaxVersion = "pgsb-syntax/1.0"

// UnicodeVersion reports the Unicode data version used for \p{...} classes
// and case folding (the Go standard library's unicode tables).
const UnicodeVersion = unicode.Version

type Node interface{ node() }

// Empty matches the empty string (e.g. an empty alternation branch).
type Empty struct{}

// Literal is a sequence of literal runes. Fold enables simple case folding.
type Literal struct {
	Runes []rune
	Fold  bool
}

// Class is a character class: a union of inclusive rune ranges, optionally
// negated. Perl classes (\d \s \w ...) and Unicode tables (\p{...}) are
// expanded to ranges at parse time.
type Class struct {
	Negated bool
	Ranges  [][2]rune
	Fold    bool
}

// Any matches any single rune (excluding '\n' unless DotAll).
type Any struct {
	DotAll bool
}

// Alt is an ordered alternation. Branches are tried left to right; this
// order is part of the documented semantics and preserved by optimizations.
type Alt struct {
	Branches []Node
}

// Concat is a sequence of nodes.
type Concat struct {
	Nodes []Node
}

// Repeat repeats Child between Min and Max times. Max < 0 means unbounded.
// Greedy selects the greedy (default) or lazy preference order.
type Repeat struct {
	Child  Node
	Min    int
	Max    int
	Greedy bool
}

// Group is a capturing (Index > 0) or non-capturing (Index == 0) group.
type Group struct {
	Child Node
	Index int
	Name  string
}

// Backref references a previously captured group by index or name.
type Backref struct {
	Index int
	Name  string
	Fold  bool
}

// AnchorKind identifies zero-width assertions about position.
type AnchorKind int

const (
	BOL      AnchorKind = iota // ^  (multiline aware)
	EOL                        // $
	BOT                        // \A
	EOT                        // \z
	WordB                      // \b
	NotWordB                   // \B
)

// Anchor is a zero-width positional assertion.
type Anchor struct {
	Kind      AnchorKind
	Multiline bool
}

// Look is a lookaround assertion. Behind selects lookbehind, Negate the
// negative form. Lookbehind is variable-length: it is evaluated by trying
// every start position that ends exactly at the current position.
type Look struct {
	Child  Node
	Behind bool
	Negate bool
}

func (Empty) node()   {}
func (Literal) node() {}
func (Class) node()   {}
func (Any) node()     {}
func (Alt) node()     {}
func (Concat) node()  {}
func (Repeat) node()  {}
func (Group) node()   {}
func (Backref) node() {}
func (Anchor) node()  {}
func (Look) node()    {}

// AST bundles the root node with group metadata.
type AST struct {
	Root     Node
	NumGroup int // number of capturing groups (group 0 is the whole match)
	Names    map[string]int
}
