// Package syntax parses capture-tracer-room patterns into an explicit AST.
//
// Syntax version: see SyntaxVersion. The grammar is documented in
// docs/syntax.md; the supported surface syntax includes character classes,
// greedy/lazy/possessive quantifiers, alternation, anchors, named groups,
// backreferences and lookaround. Nothing in the matcher shells out to a host
// regular-expression library.
package syntax

// SyntaxVersion pins the pattern grammar understood by this parser.
const SyntaxVersion = "captraceroom-regex-1.0"

// RepeatExpand is the largest bounded repetition upper bound that the grammar
// accepts; bounded counts beyond it are rejected instead of being expanded.
const RepeatExpand = 1000

// Pos is a byte offset inside the pattern source.
type Pos int

// Flags carries the in-scope pattern modifiers.
type Flags struct {
	CaseInsensitive bool // i
	DotAll          bool // s
	MultiLine       bool // m
	Unicode         bool // u (accepts strictly Unicode semantics; tracked for version pinning)
}

func (f Flags) with(on, off string) Flags {
	out := f
	for _, c := range on {
		switch c {
		case 'i':
			out.CaseInsensitive = true
		case 's':
			out.DotAll = true
		case 'm':
			out.MultiLine = true
		case 'u':
			out.Unicode = true
		}
	}
	for _, c := range off {
		switch c {
		case 'i':
			out.CaseInsensitive = false
		case 's':
			out.DotAll = false
		case 'm':
			out.MultiLine = false
		case 'u':
			out.Unicode = false
		}
	}
	return out
}

// Node is every AST element.
type Node interface {
	node()
	Position() Pos
}

type EmptyNode struct{ P Pos }
type DotNode struct{ P Pos }
type LitNode struct {
	P Pos
	R rune
}

// ClassItem is one alternative inside a bracket expression. Exactly one mode
// is active: a single rune (Single), a rune range (Lo..Hi) or a property
// escape (Property non-empty).
type ClassItem struct {
	Single     bool
	R          rune
	Range      bool
	Lo, Hi     rune
	Property   string // "d","w","s" or a Unicode category code
	ItemNegate bool   // negated property (\D, \P{...})
}

type ClassNode struct {
	P      Pos
	Negate bool
	Items  []ClassItem
}

type AnchorKind int

const (
	AnchorTextStart AnchorKind = iota // \A
	AnchorTextEnd                     // \z
	AnchorEndOrFinalNL                // \Z
	AnchorLineStart                   // ^
	AnchorLineEnd                     // $
	AnchorWordBoundary                // \b
	AnchorNotWordBoundary             // \B
)

type AnchorNode struct {
	P    Pos
	Kind AnchorKind
}

type GroupKind int

const (
	GroupCapture GroupKind = iota
	GroupNamed
	GroupNonCapture
	GroupAtomic
	GroupLookahead
	GroupNegLookahead
	GroupLookbehind
	GroupNegLookbehind
)

type GroupNode struct {
	P     Pos
	Kind  GroupKind
	Index int // 1-based capture index; 0 for non-capturing groups
	Name  string
	Body  Node
	Local Flags
}

type AltNode struct {
	P       Pos
	Options []Node
}

type CatNode struct {
	P     Pos
	Nodes []Node
}

type RepNode struct {
	P          Pos
	X          Node
	Min        int
	Max        int // -1 means unbounded
	Lazy       bool
	Possessive bool
}

type RefNode struct {
	P     Pos
	Index int // 1-based; 0 when Name is used and unresolved
	Name  string
}

// FlagNode is a mid-pattern directive such as (?i) or (?-s).
type FlagNode struct {
	P     Pos
	On    string
	Off   string
	Flags Flags
}

func (EmptyNode) node()  {}
func (DotNode) node()    {}
func (LitNode) node()    {}
func (ClassNode) node()  {}
func (AnchorNode) node() {}
func (GroupNode) node()  {}
func (AltNode) node()    {}
func (CatNode) node()    {}
func (RepNode) node()    {}
func (RefNode) node()    {}
func (FlagNode) node()   {}

func (n EmptyNode) Position() Pos  { return n.P }
func (n DotNode) Position() Pos    { return n.P }
func (n LitNode) Position() Pos    { return n.P }
func (n ClassNode) Position() Pos  { return n.P }
func (n AnchorNode) Position() Pos { return n.P }
func (n GroupNode) Position() Pos  { return n.P }
func (n AltNode) Position() Pos   { return n.P }
func (n CatNode) Position() Pos   { return n.P }
func (n RepNode) Position() Pos   { return n.P }
func (n RefNode) Position() Pos   { return n.P }
func (n FlagNode) Position() Pos  { return n.P }

// Regex is a parsed pattern.
type Regex struct {
	Pattern    string
	Root       Node
	NumGroups  int
	GroupNames map[string]int
	Flags      Flags
}
