package regex

// SyntaxVersion pins the pattern grammar and execution semantics.
const SyntaxVersion = "traceroom-syntax/1.0"

// Node is an AST node produced by the pattern parser.
type Node interface{ nodeKind() string }

type Alt struct {
	Branches []Node
}
type Concat struct {
	Nodes []Node
}
type Repeat struct {
	Child       Node
	Min, Max    int // Max == -1 means unbounded
	Lazy        bool
}
type Group struct {
	Index int // 1-based capturing index, 0 for non-capturing
	Name  string
	Child Node
}

type LookKind int

const (
	LookAheadPos LookKind = iota
	LookAheadNeg
	LookBehindPos
	LookBehindNeg
)

type Look struct {
	Kind  LookKind
	Child Node
}

type AnchorKind int

const (
	AnchorBeginText AnchorKind = iota // \A and ^
	AnchorEndText                     // \z and $
	AnchorWordBoundary                // \b
	AnchorNotWordBoundary             // \B
)

type Anchor struct {
	Kind AnchorKind
}

type Backref struct {
	Index int
	Name  string
}

type Lit struct {
	R rune
}

type Dot struct{}

// Class is a pre-resolved character class.
type Class struct {
	Negated bool
	Ranges  []RuneRange // sorted, merged
	// Pre is a set of built-in sets active for this class: d,w,s (and their
	// inverses are represented with flags).
	HasDigit bool
	HasWord  bool
	HasSpace bool
}

type RuneRange struct {
	Lo, Hi rune
}

func (Alt) nodeKind() string    { return "alt" }
func (Concat) nodeKind() string { return "concat" }
func (Repeat) nodeKind() string { return "repeat" }
func (Group) nodeKind() string  { return "group" }
func (Look) nodeKind() string   { return "look" }
func (Anchor) nodeKind() string { return "anchor" }
func (Backref) nodeKind() string { return "backref" }
func (Lit) nodeKind() string    { return "lit" }
func (Dot) nodeKind() string    { return "dot" }
func (Class) nodeKind() string  { return "class" }
