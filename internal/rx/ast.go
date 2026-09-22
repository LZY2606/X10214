package rx

// AST node kinds used by the parser and compiler.
type NodeKind int

const (
	NEmpty NodeKind = iota
	NConcat
	NAlt
	NGroup // capturing or non-capturing group, optionally with flag changes
	NRepeat
	NChar       // single literal code point
	NClass      // bracket character class
	NDot        // .
	NAnchor     // ^ $ \b \B \A \z
	NBackref    // \1 or \k<name>
	NLookahead  // (?=...) / (?!...)
	NLookbehind // (?<=...) / (?<!...)
	NProperty   // \p{...} / \P{...}
	NQuote      // \Q...\E sequence of literals
)

// Flags is the per-node flag state.
type Flags struct {
	I bool // case-insensitive ASCII/Unicode simple folding
	M bool // multiline
	S bool // dotall
}

// Node is an AST element. IDs are assigned in pre-order starting at 1.
type Node struct {
	Kind     NodeKind
	ID       int
	Beg, End int // byte offsets within the pattern source
	Name     string
	Negate   bool
	Min, Max int // repeat: Max == -1 means unbounded
	Greedy   bool
	Flags    Flags
	AddF     Flags // group local flag adds
	DelF     Flags
	Rune     rune
	Class    *CharClass
	RefIndex int // backref group index, 1-based
	Children []*Node
	Text     string // NQuote literal text
	Anchor   AnchorKind
}

type AnchorKind int

const (
	AnchorBOS AnchorKind = iota // \A
	AnchorEOS                   // \z (absolute end)
	AnchorBOL                   // ^
	AnchorEOL                   // $
	AnchorWB                    // \b
	AnchorNWB                   // \B
)

// CharClass is a parsed bracket expression.
type CharClass struct {
	Negate   bool
	Literals []rune
	Ranges   []RuneRange
	POSIX    []POSIXItem
	Props    []PropItem
}

type RuneRange struct{ Lo, Hi rune }

type POSIXItem struct {
	Name   string
	Negate bool
}

type PropItem struct {
	Name   string
	Negate bool
}

// Pattern is the parse result.
type Pattern struct {
	Source string
	Root   *Node
	Groups []GroupInfo // index 0 unused
	NextID int
	Flags  Flags // final global flags
}

type GroupInfo struct {
	Index int
	Name  string
	Beg   int
	End   int
}

// SyntaxVersion is pinned for every result.
const SyntaxVersion = "traj-syntax-1.0"
