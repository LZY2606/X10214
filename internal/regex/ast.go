package regex

import (
	"fmt"
	"strings"
)

// Node is one AST node of a parsed pattern.
type Node struct {
	Kind Kind
	// Literal / class payload.
	Rune    rune
	Ranges  [][2]rune // inclusive ranges for KindClass
	Negated bool      // KindClass
	// Quantifier payload.
	Min, Max int  // Max < 0 means unbounded
	Lazy     bool // non-greedy
	// Group payload.
	GroupIdx int    // 1-based capture index, 0 for non-capturing
	Name     string // named group name
	// Backref payload.
	RefIdx  int
	RefName string
	// Lookaround payload.
	Ahead    bool // lookahead vs lookbehind
	Positive bool
	// Children.
	Kids []*Node
}

// Kind enumerates AST node kinds.
type Kind int

const (
	KindEmpty Kind = iota
	KindLiteral
	KindAny // dot (matches any rune except '\n')
	KindClass
	KindConcat
	KindAlt
	KindRepeat
	KindGroup
	KindAnchorStart // ^
	KindAnchorEnd   // $
	KindBackref
	KindLook
)

func (k Kind) String() string {
	switch k {
	case KindEmpty:
		return "Empty"
	case KindLiteral:
		return "Literal"
	case KindAny:
		return "Any"
	case KindClass:
		return "Class"
	case KindConcat:
		return "Concat"
	case KindAlt:
		return "Alt"
	case KindRepeat:
		return "Repeat"
	case KindGroup:
		return "Group"
	case KindAnchorStart:
		return "AnchorStart"
	case KindAnchorEnd:
		return "AnchorEnd"
	case KindBackref:
		return "Backref"
	case KindLook:
		return "Look"
	}
	return "?"
}

// Dump renders the AST as an indented tree (stable, used by UI and tests).
func (n *Node) Dump() string {
	var b strings.Builder
	n.dump(&b, 0)
	return b.String()
}

func (n *Node) dump(b *strings.Builder, depth int) {
	indent := strings.Repeat("  ", depth)
	switch n.Kind {
	case KindLiteral:
		fmt.Fprintf(b, "%sLiteral %q\n", indent, n.Rune)
	case KindClass:
		fmt.Fprintf(b, "%sClass negated=%v %s\n", indent, n.Negated, formatRanges(n.Ranges))
	case KindRepeat:
		max := "∞"
		if n.Max >= 0 {
			max = fmt.Sprint(n.Max)
		}
		fmt.Fprintf(b, "%sRepeat {%d,%s} lazy=%v\n", indent, n.Min, max, n.Lazy)
	case KindGroup:
		fmt.Fprintf(b, "%sGroup #%d name=%q\n", indent, n.GroupIdx, n.Name)
	case KindBackref:
		fmt.Fprintf(b, "%sBackref #%d name=%q\n", indent, n.RefIdx, n.RefName)
	case KindLook:
		dir := "ahead"
		if !n.Ahead {
			dir = "behind"
		}
		sign := "positive"
		if !n.Positive {
			sign = "negative"
		}
		fmt.Fprintf(b, "%sLook %s %s\n", indent, dir, sign)
	default:
		fmt.Fprintf(b, "%s%s\n", indent, n.Kind)
	}
	for _, k := range n.Kids {
		k.dump(b, depth+1)
	}
}

func formatRanges(rs [][2]rune) string {
	var parts []string
	for _, r := range rs {
		if r[0] == r[1] {
			parts = append(parts, fmt.Sprintf("%q", r[0]))
		} else {
			parts = append(parts, fmt.Sprintf("%q-%q", r[0], r[1]))
		}
	}
	return "[" + strings.Join(parts, " ") + "]"
}
