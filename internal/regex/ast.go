package regex

import (
	"fmt"
	"strings"
)

// SyntaxVersion is pinned into every result so traces are reproducible.
const SyntaxVersion = "gsb.re/1.0"

// AnchorKind identifies zero-width assertions.
type AnchorKind int

const (
	AnchorBeginText        AnchorKind = iota // \A
	AnchorEndText                            // \z
	AnchorEndTextOrFinalNL                   // $  (end of text or before a trailing \n)
	AnchorBeginTextCaret                     // ^  (beginning of text; no multiline flag in v1)
	AnchorWordBoundary                       // \b
	AnchorNonWordBoundary                    // \B
)

// Node is the interface implemented by all AST nodes.
type Node interface {
	node()
	String() string
}

// Literal is a single rune.
type Literal struct{ R rune }

// Any matches any rune except '\n'.
type Any struct{}

// ClassItem is one item of a character class: either an inclusive rune
// range, or a named Unicode range table (see unicode.go). Neg inverts
// this single item (used for \D \W \S \P{...} inside a class).
type ClassItem struct {
	Lo, Hi rune
	Table  string // "" when Lo/Hi are used
	Neg    bool
}

// Class is a (possibly negated) character class.
type Class struct {
	Negated bool
	Items   []ClassItem
}

// Concat is an ordered sequence.
type Concat struct{ Nodes []Node }

// Alt is an ordered alternation. Branches are tried left to right.
type Alt struct{ Branches []Node }

// Repeat repeats Child between Min and Max times; Max == -1 means
// unbounded. Greedy repeats prefer more iterations, lazy ones fewer.
type Repeat struct {
	Child    Node
	Min, Max int
	Lazy     bool
}

// Group is a parenthesized sub-expression. Capture groups carry an
// Index (1-based; 0 is the whole match) and an optional Name.
type Group struct {
	Child   Node
	Index   int
	Name    string
	Capture bool
}

// Backref matches the text previously captured by a group. An unset
// group makes the backreference fail (documented semantics).
type Backref struct {
	Index int
	Name  string
}

// Anchor is a zero-width assertion.
type Anchor struct{ Kind AnchorKind }

// Look is a lookaround assertion. Captures inside a positive look are
// kept; captures inside a negative look are discarded.
type Look struct {
	Child  Node
	Negate bool
	Behind bool
}

func (*Literal) node() {}
func (*Any) node()     {}
func (*Class) node()   {}
func (*Concat) node()  {}
func (*Alt) node()     {}
func (*Repeat) node()  {}
func (*Group) node()   {}
func (*Backref) node() {}
func (*Anchor) node()  {}
func (*Look) node()    {}

func (l *Literal) String() string { return fmt.Sprintf("Lit(%s)", quoteRune(l.R)) }
func (*Any) String() string       { return "Any" }

func (c *Class) String() string {
	var b strings.Builder
	b.WriteString("Class(")
	if c.Negated {
		b.WriteString("^")
	}
	for _, it := range c.Items {
		if it.Table != "" {
			b.WriteString("\\p{" + it.Table + "}")
		} else if it.Lo == it.Hi {
			b.WriteString(quoteRune(it.Lo))
		} else {
			b.WriteString(quoteRune(it.Lo) + "-" + quoteRune(it.Hi))
		}
	}
	b.WriteString(")")
	return b.String()
}

func (c *Concat) String() string {
	parts := make([]string, len(c.Nodes))
	for i, n := range c.Nodes {
		parts[i] = n.String()
	}
	return "Concat(" + strings.Join(parts, " ") + ")"
}

func (a *Alt) String() string {
	parts := make([]string, len(a.Branches))
	for i, n := range a.Branches {
		parts[i] = n.String()
	}
	return "Alt(" + strings.Join(parts, " | ") + ")"
}

func (r *Repeat) String() string {
	max := ""
	if r.Max < 0 {
		max = "inf"
	} else {
		max = fmt.Sprint(r.Max)
	}
	lazy := ""
	if r.Lazy {
		lazy = " lazy"
	}
	return fmt.Sprintf("Repeat(%s {%d,%s}%s)", r.Child.String(), r.Min, max, lazy)
}

func (g *Group) String() string {
	if !g.Capture {
		return "Group(?:" + g.Child.String() + ")"
	}
	if g.Name != "" {
		return fmt.Sprintf("Group(%d <%s> %s)", g.Index, g.Name, g.Child.String())
	}
	return fmt.Sprintf("Group(%d %s)", g.Index, g.Child.String())
}

func (b *Backref) String() string {
	if b.Name != "" {
		return fmt.Sprintf("Backref(<%s> #%d)", b.Name, b.Index)
	}
	return fmt.Sprintf("Backref(#%d)", b.Index)
}

func (a *Anchor) String() string {
	switch a.Kind {
	case AnchorBeginText:
		return "Anchor(\\A)"
	case AnchorEndText:
		return "Anchor(\\z)"
	case AnchorEndTextOrFinalNL:
		return "Anchor($)"
	case AnchorBeginTextCaret:
		return "Anchor(^)"
	case AnchorWordBoundary:
		return "Anchor(\\b)"
	case AnchorNonWordBoundary:
		return "Anchor(\\B)"
	}
	return "Anchor(?)"
}

func (l *Look) String() string {
	kind := "?="
	if l.Negate && l.Behind {
		kind = "?<!"
	} else if l.Negate {
		kind = "?!"
	} else if l.Behind {
		kind = "?<="
	}
	return fmt.Sprintf("Look(%s %s)", kind, l.Child.String())
}

func quoteRune(r rune) string {
	if r >= 0x21 && r <= 0x7e && r != '\\' {
		return string(r)
	}
	return fmt.Sprintf("\\u{%04X}", r)
}

// Contains reports whether r is in the class.
func (c *Class) Contains(r rune) bool {
	in := false
	for _, it := range c.Items {
		hit := false
		if it.Table != "" {
			hit = inUnicodeTable(it.Table, r)
		} else {
			hit = r >= it.Lo && r <= it.Hi
		}
		if it.Neg {
			hit = !hit
		}
		if hit {
			in = true
			break
		}
	}
	if c.Negated {
		return !in
	}
	return in
}
