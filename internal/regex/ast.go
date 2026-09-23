package regex

import "fmt"

// Node is a syntax tree node. Pos is the byte offset into the pattern.
type Node interface {
	nodeMarker()
	Pos() int
}

type baseNode struct{ pos int }

func (baseNode) nodeMarker() {}
func (n baseNode) Pos() int  { return n.pos }

// Flags are inline/global options of the grammar.
type Flags struct {
	CaseInsensitive bool // i
	DotAll          bool // s
	Multiline       bool // m
}

func (f Flags) with(toggle string, on bool) (Flags, error) {
	g := f
	for _, c := range toggle {
		switch c {
		case 'i':
			g.CaseInsensitive = on
		case 's':
			g.DotAll = on
		case 'm':
			g.Multiline = on
		default:
			return f, fmt.Errorf("unknown flag %q", string(c))
		}
	}
	return g, nil
}

// Concat is a sequence of nodes.
type Concat struct {
	baseNode
	Items []Node
}

// Alt is an alternation. Order is the documented priority order and is
// preserved end-to-end by the executor.
type Alt struct {
	baseNode
	Branches []Node
}

// Quant applies a quantifier to Child.
type Quant struct {
	baseNode
	Child Node
	Min   int
	Max   int // -1 means unbounded
	Lazy  bool
}

// Group is a capturing or non-capturing group.
type Group struct {
	baseNode
	Child  Node
	Capture bool   // false for (?:...)
	Index   int    // 1-based, 0 when non-capturing
	Name    string // empty when unnamed
	Flags   Flags
	Set     string // flag toggles applied (for (?i:...) the Set is stored verbatim)
}

// Look is a lookaround. Behind=false -> lookahead, true -> lookbehind.
type Look struct {
	baseNode
	Child  Node
	Neg    bool
	Behind bool
}

// AtomLit matches a literal code point (or a single invalid byte).
type AtomLit struct {
	baseNode
	R     rune
	Invalid bool // true: R is a raw invalid byte value 0..255
}

// AtomDot matches any atom; honors the s flag.
type AtomDot struct {
	baseNode
	DotAll bool
}

// AtomGrapheme is the \X extended grapheme cluster matcher.
type AtomGrapheme struct{ baseNode }

// AtomBoundary is a zero-width ^/$/\b/\B matcher.
type AtomBoundary struct {
	baseNode
	Kind BoundaryKind
}

type BoundaryKind int

const (
	BoundaryStart BoundaryKind = iota // ^
	BoundaryEnd                       // $
	BoundaryWord                      // \b
	BoundaryNotWord                   // \B
)

// AtomClass matches a character class.
type AtomClass struct {
	baseNode
	Elems     []ClassElem
	Negate    bool
}

// ClassElem is one element of a class: either a literal rune range,
// a \w/\d/\s style Perl class, or a Unicode property.
type ClassElem struct {
	Lo, Hi   rune // inclusive literal range; Lo=Hi for singleton
	Perl     byte // 'w','d','s', 0 otherwise
	Prop     string
	NegRange bool   // \D,\W,\S or \P{...}: element matches the complement
	Invalid  bool   // raw invalid byte range
}

// AtomBackref references a capturing group by index or name.
type AtomBackref struct {
	baseNode
	Index int
	Name  string
}
