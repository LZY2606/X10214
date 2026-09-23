// Package regex implements a self-contained regular expression engine for the
// 捕获轨迹室 (capture trace room). It never delegates matching to the host
// regexp package: parsing, AST construction, compilation and execution are
// all handled here.
//
// Syntax version: gsb-syntax/1.0
//
//	alternation      a|b|c            (branches tried left to right)
//	concatenation    abc
//	groups           (...)  (?:...)  (?<name>...)  (?P<name>...)
//	backreferences   \1..\9  \k<name> (unset groups match the empty string)
//	quantifiers      * + ? {n} {n,} {n,m}, lazy suffix ?, n,m <= 1000
//	lookaround       (?=...) (?!...) (?<=...) (?<!...)
//	anchors          ^ $ \A \z \b \B   (^/$ are absolute text anchors)
//	dot              .  (any code point except U+000A)
//	classes          [abc] [^a-z] \d \D \w \W \s \S \p{L} \p{Greek} ...
//	escapes          \n \t \r \f \v \xNN \x{NNNN} and escaped punctuation
//
// Documented priority order: alternation branches are tried left to right;
// greedy quantifiers prefer iterating, lazy quantifiers prefer exiting;
// the search scans start positions left to right and the first match in
// that order wins. All optimizations must preserve this observable order.
package regex

import (
	"fmt"
	"strings"
)

// SyntaxVersion is pinned into every result produced by this package.
const SyntaxVersion = "gsb-syntax/1.0"

// Node is the interface implemented by all AST nodes.
type Node interface{ node() }

type Alt struct{ Branches []Node }
type Concat struct{ Nodes []Node }
type Literal struct{ R rune }
type Class struct {
	Ranges  [][2]rune // inclusive rune ranges
	Negated bool
}
type Dot struct{}
type Anchor struct{ Kind AnchorKind }
type Group struct {
	Index int    // 0 means non-capturing
	Name  string // empty for unnamed
	Child Node
}
type Backref struct {
	Index int
	Name  string
}
type Quant struct {
	Child Node
	Min   int
	Max   int // -1 means unbounded
	Lazy  bool
}
type Look struct {
	Ahead    bool
	Positive bool
	Child    Node
}

func (Alt) node()     {}
func (Concat) node()  {}
func (Literal) node() {}
func (Class) node()   {}
func (Dot) node()     {}
func (Anchor) node()  {}
func (Group) node()   {}
func (Backref) node() {}
func (Quant) node()   {}
func (Look) node()    {}

type AnchorKind int

const (
	AnchorBeginText AnchorKind = iota // ^ or \A
	AnchorEndText                     // $ or \z
	AnchorWordBoundary                // \b
	AnchorNotWordBoundary             // \B
)

func (k AnchorKind) String() string {
	switch k {
	case AnchorBeginText:
		return "begin-text"
	case AnchorEndText:
		return "end-text"
	case AnchorWordBoundary:
		return "word-boundary"
	case AnchorNotWordBoundary:
		return "not-word-boundary"
	}
	return "?"
}

// Regex is a parsed pattern.
type Regex struct {
	Root      Node
	NumGroups int
	Names     map[string]int
}

// ASTString renders the AST as an indented tree for display.
func (re *Regex) ASTString() string {
	var b strings.Builder
	writeNode(&b, re.Root, 0)
	return b.String()
}

func writeNode(b *strings.Builder, n Node, depth int) {
	indent := strings.Repeat("  ", depth)
	switch t := n.(type) {
	case Alt:
		fmt.Fprintf(b, "%sAlt\n", indent)
		for _, c := range t.Branches {
			writeNode(b, c, depth+1)
		}
	case Concat:
		fmt.Fprintf(b, "%sConcat\n", indent)
		for _, c := range t.Nodes {
			writeNode(b, c, depth+1)
		}
	case Literal:
		fmt.Fprintf(b, "%sLiteral %q\n", indent, t.R)
	case Class:
		fmt.Fprintf(b, "%sClass %s\n", indent, classString(t))
	case Dot:
		fmt.Fprintf(b, "%sDot\n", indent)
	case Anchor:
		fmt.Fprintf(b, "%sAnchor %s\n", indent, t.Kind)
	case Group:
		if t.Index == 0 {
			fmt.Fprintf(b, "%sGroup (non-capturing)\n", indent)
		} else if t.Name != "" {
			fmt.Fprintf(b, "%sGroup #%d <%s>\n", indent, t.Index, t.Name)
		} else {
			fmt.Fprintf(b, "%sGroup #%d\n", indent, t.Index)
		}
		writeNode(b, t.Child, depth+1)
	case Backref:
		if t.Name != "" {
			fmt.Fprintf(b, "%sBackref <%s> (#%d)\n", indent, t.Name, t.Index)
		} else {
			fmt.Fprintf(b, "%sBackref #%d\n", indent, t.Index)
		}
	case Quant:
		max := fmt.Sprintf("%d", t.Max)
		if t.Max < 0 {
			max = "inf"
		}
		lazy := ""
		if t.Lazy {
			lazy = " lazy"
		}
		fmt.Fprintf(b, "%sQuant {%d,%s}%s\n", indent, t.Min, max, lazy)
		writeNode(b, t.Child, depth+1)
	case Look:
		kind := "lookahead"
		if !t.Ahead {
			kind = "lookbehind"
		}
		pol := "positive"
		if !t.Positive {
			pol = "negative"
		}
		fmt.Fprintf(b, "%sLook %s %s\n", indent, pol, kind)
		writeNode(b, t.Child, depth+1)
	}
}

func classString(c Class) string {
	var b strings.Builder
	if c.Negated {
		b.WriteString("[^")
	} else {
		b.WriteString("[")
	}
	for _, r := range c.Ranges {
		if r[0] == r[1] {
			fmt.Fprintf(&b, "%q", r[0])
		} else {
			fmt.Fprintf(&b, "%q-%q", r[0], r[1])
		}
	}
	b.WriteString("]")
	return b.String()
}

// Match reports whether r is in the class.
func (c Class) Match(r rune) bool {
	in := false
	for _, rg := range c.Ranges {
		if r >= rg[0] && r <= rg[1] {
			in = true
			break
		}
	}
	return in != c.Negated
}
