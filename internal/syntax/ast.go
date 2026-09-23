// Package syntax implements the parser, AST, compiler and optimizer for the
// capture-trace-room regular expression engine. It never delegates matching
// to the host regexp package; it only builds the program executed by the
// project's own VM (package engine).
package syntax

import (
	"fmt"
	"strings"
	"unicode"
)

// SyntaxVersion is pinned into every recorded run so results can be
// interpreted against the exact grammar that produced them.
const SyntaxVersion = "pgsb.syntax/1.0"

// UnicodeVersion reports the version of the Unicode data tables used for
// \p{...} classes and word-boundary detection.
const UnicodeVersion = unicode.Version

// Kind enumerates AST node kinds.
type Kind int

const (
	KEmpty Kind = iota
	KLiteral
	KClass
	KAny
	KConcat
	KAlt
	KRepeat
	KGroup
	KBackref
	KAssert
	KAnchor
	KWordBoundary
)

// AssertKind identifies lookaround flavours.
type AssertKind int

const (
	Lookahead AssertKind = iota
	NegLookahead
	Lookbehind
	NegLookbehind
)

// AnchorKind identifies ^ and $.
type AnchorKind int

const (
	BeginText AnchorKind = iota
	EndText
)

// Class is a compiled character class.
type Class struct {
	Ranges  [][2]rune             // inclusive rune ranges
	Cats    []*unicode.RangeTable // Unicode general categories (\p{...})
	Negated bool
}

// Match reports whether r is in the class. Invalid input bytes are encoded
// as negative runes by the engine decoder and never match any class.
func (c *Class) Match(r rune) bool {
	if r < 0 {
		return false
	}
	ok := false
	for _, rg := range c.Ranges {
		if r >= rg[0] && r <= rg[1] {
			ok = true
			break
		}
	}
	if !ok {
		for _, t := range c.Cats {
			if unicode.Is(t, r) {
				ok = true
				break
			}
		}
	}
	if c.Negated {
		return !ok
	}
	return ok
}

func (c *Class) String() string {
	var b strings.Builder
	if c.Negated {
		b.WriteString("[^")
	} else {
		b.WriteString("[")
	}
	for _, rg := range c.Ranges {
		if rg[0] == rg[1] {
			fmt.Fprintf(&b, "%c", rg[0])
		} else {
			fmt.Fprintf(&b, "%c-%c", rg[0], rg[1])
		}
	}
	for range c.Cats {
		b.WriteString(`\p{...}`)
	}
	b.WriteString("]")
	return b.String()
}

// Node is one AST node.
type Node struct {
	Kind     Kind
	Runes    []rune  // KLiteral
	Class    *Class  // KClass
	Children []*Node // KConcat, KAlt
	Child    *Node   // KRepeat, KGroup, KAssert
	Min      int     // KRepeat
	Max      int     // KRepeat, -1 = unbounded
	Greedy   bool    // KRepeat
	Index    int     // KGroup (0 = non-capturing), KBackref
	Name     string  // KGroup, KBackref
	Assert   AssertKind
	Anchor   AnchorKind
	Negate   bool // KWordBoundary (\B)
}

// String renders a compact, stable form of the AST for display.
func (n *Node) String() string {
	switch n.Kind {
	case KEmpty:
		return "Empty"
	case KLiteral:
		return fmt.Sprintf("Lit(%q)", string(n.Runes))
	case KClass:
		return "Class" + n.Class.String()
	case KAny:
		return "Any"
	case KConcat:
		parts := make([]string, len(n.Children))
		for i, c := range n.Children {
			parts[i] = c.String()
		}
		return "Concat(" + strings.Join(parts, " ") + ")"
	case KAlt:
		parts := make([]string, len(n.Children))
		for i, c := range n.Children {
			parts[i] = c.String()
		}
		return "Alt(" + strings.Join(parts, " | ") + ")"
	case KRepeat:
		max := "inf"
		if n.Max >= 0 {
			max = fmt.Sprint(n.Max)
		}
		mode := "greedy"
		if !n.Greedy {
			mode = "lazy"
		}
		return fmt.Sprintf("Repeat{%d,%s,%s}(%s)", n.Min, max, mode, n.Child.String())
	case KGroup:
		if n.Index == 0 {
			return "Group(?)(" + n.Child.String() + ")"
		}
		if n.Name != "" {
			return fmt.Sprintf("Group#%d<%s>(%s)", n.Index, n.Name, n.Child.String())
		}
		return fmt.Sprintf("Group#%d(%s)", n.Index, n.Child.String())
	case KBackref:
		if n.Name != "" {
			return fmt.Sprintf("Backref<%s>", n.Name)
		}
		return fmt.Sprintf("Backref#%d", n.Index)
	case KAssert:
		names := []string{"Lookahead", "NegLookahead", "Lookbehind", "NegLookbehind"}
		return names[n.Assert] + "(" + n.Child.String() + ")"
	case KAnchor:
		if n.Anchor == BeginText {
			return "Anchor(^)"
		}
		return "Anchor($)"
	case KWordBoundary:
		if n.Negate {
			return `Boundary(\B)`
		}
		return `Boundary(\b)`
	}
	return "?"
}

// MaxExpansion caps AST growth when unrolling bounded repeats, protecting
// the compiler from patterns like ((a{9}){9}){9}.
const MaxExpansion = 20000
