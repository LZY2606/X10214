// Package regex implements a self-contained regular expression engine with
// explicit capture-trace semantics. It does not use the host "regexp"
// package for matching; every construct (character classes, quantifiers,
// alternation, anchors, named groups, backreferences, lookaround) is
// executed by the project's own backtracking VM.
package regex

import "unicode"

// SyntaxVersion is pinned into every result so traces are reproducible.
const SyntaxVersion = "capturelab-syntax/1.0"

// UnicodeDataVersion records the Unicode data tables in use.
var UnicodeDataVersion = "Unicode " + unicode.Version

// Kind identifies an AST node kind.
type Kind string

const (
	KindEmpty     Kind = "empty"
	KindLiteral   Kind = "literal"
	KindDot       Kind = "dot"
	KindClass     Kind = "class"
	KindConcat    Kind = "concat"
	KindAlternate Kind = "alternate"
	KindRepeat    Kind = "repeat"
	KindGroup     Kind = "group"
	KindAnchor    Kind = "anchor"
	KindBackref   Kind = "backref"
	KindLook      Kind = "look"
)

// AnchorKind identifies zero-width assertions.
type AnchorKind string

const (
	AnchorStartText AnchorKind = "start_text" // \A
	AnchorEndText   AnchorKind = "end_text"   // \z
	AnchorLineStart AnchorKind = "line_start" // ^
	AnchorLineEnd   AnchorKind = "line_end"   // $
	AnchorWordB     AnchorKind = "word_boundary"
	AnchorNotWordB  AnchorKind = "not_word_boundary"
)

// LookKind identifies lookaround variants.
type LookKind string

const (
	LookAhead     LookKind = "lookahead"
	LookAheadNeg  LookKind = "lookahead_neg"
	LookBehind    LookKind = "lookbehind"
	LookBehindNeg LookKind = "lookbehind_neg"
)

// Node is a single AST node. Which fields are set depends on Kind.
type Node struct {
	Kind     Kind       `json:"kind"`
	Text     string     `json:"text,omitempty"`     // literal text
	Fold     bool       `json:"fold,omitempty"`     // case-insensitive (flag i)
	DotAll   bool       `json:"dotAll,omitempty"`   // dot matches \n (flag s)
	Class    *Class     `json:"class,omitempty"`    // character class
	Children []*Node    `json:"children,omitempty"` // concat / alternate
	Child    *Node      `json:"child,omitempty"`    // repeat / group / look
	Min      int        `json:"min,omitempty"`
	Max      int        `json:"max"` // -1 = unbounded
	Lazy     bool       `json:"lazy,omitempty"`
	Index    int        `json:"index,omitempty"` // group/backref index (group: 0 = non-capturing)
	Name     string     `json:"name,omitempty"`  // group name / backref name
	Anchor   AnchorKind `json:"anchor,omitempty"`
	Multi    bool       `json:"multi,omitempty"` // ^/$ multiline (flag m)
	Look     LookKind   `json:"look,omitempty"`
}
