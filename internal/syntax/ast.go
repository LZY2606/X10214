package syntax

// Flags records the active expression flags. The syntax version is pinned in
// version.go; parsing semantics never change without bumping it.
type Flags struct {
	I bool // case-insensitive
	M bool // multiline: ^ and $ match at line boundaries
	S bool // dotall: '.' matches a line terminator
	U bool // Unicode classes expanded for \w \d \s (default on; (?-u) restricts to ASCII)
}

// FlagBits maps flag letters to bit numbers.
const (
	FlagI = 1 << iota
	FlagM
	FlagS
	FlagU
)

func (f Flags) Bits() int {
	b := 0
	if f.I {
		b |= FlagI
	}
	if f.M {
		b |= FlagM
	}
	if f.S {
		b |= FlagS
	}
	if f.U {
		b |= FlagU
	}
	return b
}

func BitsToFlags(b int) Flags {
	return Flags{I: b&FlagI != 0, M: b&FlagM != 0, S: b&FlagS != 0, U: b&FlagU != 0}
}

// NodeKind enumerates AST node types.
type NodeKind string

const (
	KindEmpty    NodeKind = "Empty"
	KindConcat   NodeKind = "Concat"
	KindAlt      NodeKind = "Alt"
	KindLit      NodeKind = "Lit"
	KindAny      NodeKind = "Any"
	KindClass    NodeKind = "Class"
	KindAnchor   NodeKind = "Anchor"
	KindGroup    NodeKind = "Group"
	KindRepeat   NodeKind = "Repeat"
	KindBackref  NodeKind = "Backref"
	KindLook     NodeKind = "Look"
)

// Span is one element of a character class.
type Span struct {
	Lo      rune
	Hi      rune
	Kind    SpanKind
	Negate  bool   // for KindProperty
	Prop    string // for KindProperty: general category alias, e.g. "L", "Lu", "N"
}

type SpanKind int

const (
	SpanRune SpanKind = iota
	SpanProperty
)

type Node interface {
	Kind() NodeKind
	GetID() int
	SetID(int)
}

type baseNode struct{ id int }

func (b *baseNode) GetID() int      { return b.id }
func (b *baseNode) SetID(id int)    { b.id = id }

type Empty struct{ baseNode }

func (Empty) Kind() NodeKind { return KindEmpty }

type Concat struct {
	baseNode
	Items []Node
}
func (Concat) Kind() NodeKind { return KindConcat }

type Alt struct {
	baseNode
	Branches []Node
}
func (Alt) Kind() NodeKind { return KindAlt }

type Lit struct {
	baseNode
	R      rune
	Flags  Flags
}
func (Lit) Kind() NodeKind { return KindLit }

type Any struct {
	baseNode
	Flags Flags
}
func (Any) Kind() NodeKind { return KindAny }

type Class struct {
	baseNode
	Negate bool
	Spans  []Span
	Flags  Flags
}
func (Class) Kind() NodeKind { return KindClass }

type AnchorKind int

const (
	AnchorStartLine AnchorKind = iota // ^
	AnchorEndLine                     // $
	AnchorStartText                   // \A
	AnchorEndText                     // \z
	AnchorEndTextOptNL                // \Z
	AnchorWordBoundary
	AnchorNotWordBoundary
)

type Anchor struct {
	baseNode
	A     AnchorKind
	Flags Flags
}
func (Anchor) Kind() NodeKind { return KindAnchor }

type Group struct {
	baseNode
	Capture bool
	Number  int // assigned by the compiler numbering pass
	Name    string
	Body    Node
	// FlagSet/FlagClear are non-zero only for flag-only scoping groups, e.g. (?i:...), (?-s:...).
	FlagSet   int
	FlagClear int
}
func (Group) Kind() NodeKind { return KindGroup }

type Repeat struct {
	baseNode
	Min       int
	Max       int // -1 means unbounded
	Greedy    bool
	Possessive bool
	Body      Node
}
func (Repeat) Kind() NodeKind { return KindRepeat }

type Backref struct {
	baseNode
	Number int
	Name   string // empty when numeric
	Flags  Flags
}
func (Backref) Kind() NodeKind { return KindBackref }

type Look struct {
	baseNode
	Behind bool
	Negate bool
	Body   Node
	Flags  Flags
}
func (Look) Kind() NodeKind { return KindLook }
