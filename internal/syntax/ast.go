package syntax

// GrammarVersion pins the pattern syntax recognised by this executor.
const GrammarVersion = "CT-GRAMMAR-1.0"

// Kind enumerates the AST node kinds of the capture-trail grammar.
type Kind int

const (
	KEmpty Kind = iota
	KChar       // single rune literal
	KAny        // .
	KClass      // [...] / .-classes
	KAlt
	KCat
	KRepeat
	KGroup       // capturing or non-capturing (also flag-only groups)
	KFlags       // synthetic scope carrying (?i:...) / (?-i:...) settings
	KAnchor
	KBackref
	KLook        // lookaround subexpression
)

func (k Kind) String() string {
	return kindNames[k]
}

var kindNames = []string{
	"empty", "char", "any", "class", "alt", "cat",
	"repeat", "group", "flags", "anchor", "backref", "look",
}

// Anchor identifies a zero-width assertion in the grammar.
type Anchor int

const (
	ABegin Anchor = iota // ^
	AEnd                 // $
	ABeginText            // \A
	AAtEnd               // \z
	AWordBoundary        // \b
	ANotWordBoundary     // \B
)

func (a Anchor) String() string {
	switch a {
	case ABegin:
		return "^"
	case AEnd:
		return "$"
	case ABeginText:
		return `\A`
	case AAtEnd:
		return `\z`
	case AWordBoundary:
		return `\b`
	case ANotWordBoundary:
		return `\B`
	}
	return "?"
}

// LookDir is lookahead / lookbehind.
type LookDir int

const (
	LookAhead LookDir = iota
	LookBehind
)

func (d LookDir) String() string {
	if d == LookAhead {
		return "ahead"
	}
	return "behind"
}

// CharClass is a parsed character class. Ranges are inclusive rune ranges.
type CharClass struct {
	Negated bool        `json:"negated"`
	Ranges  []RuneRange `json:"ranges"`
	// Mask carries built-in classes present in this bracket: d D s S w W.
	Mask ClassMask `json:"mask,omitempty"`
}

// ClassMask marks which built-in classes appear inside a bracket.
type ClassMask uint8

const (
	ClassD ClassMask = 1 << iota // \d
	ClassS                       // \s
	ClassW                       // \w
)

// RuneRange is an inclusive rune interval.
type RuneRange struct {
	Lo, Hi rune
}

// Node is one AST node. Fields are populated per Kind (see parser docs).
type Node struct {
	ID   int   `json:"id"`
	Kind Kind  `json:"kind"`
	Pos  int   `json:"pos"` // byte offset in pattern
	End  int   `json:"end"`

	Children []*Node `json:"children,omitempty"`

	// KChar
	Rune rune `json:"rune,omitempty"`

	// KClass
	Class *CharClass `json:"class,omitempty"`

	// KRepeat
	Min  int  `json:"min,omitempty"`
	Max  int  `json:"max,omitempty"` // -1 == unbounded
	Lazy bool `json:"lazy,omitempty"`

	// KGroup
	GroupIndex int    `json:"groupIndex,omitempty"` // 0 => non-capturing
	GroupName  string `json:"groupName,omitempty"`

	// KFlags
	SetFlags   FlagSet `json:"setFlags,omitempty"`
	ClearFlags FlagSet `json:"clearFlags,omitempty"`

	// KAnchor
	Anchor Anchor `json:"anchor,omitempty"`

	// KBackref
	BackIndex int    `json:"backIndex,omitempty"`
	BackName  string `json:"backName,omitempty"`

	// KLook
	LookDir    LookDir `json:"lookDir,omitempty"`
	LookNeg    bool    `json:"lookNeg,omitempty"`
	LookWidth  int     `json:"lookWidth,omitempty"` // fixed rune width (lookbehind)
	LookVariable bool  `json:"lookVariable,omitempty"`
}

// FlagSet is a bit set of pattern flags.
type FlagSet uint8

const (
	FlagI FlagSet = 1 << iota // case-insensitive (ASCII folding)
	FlagM                     // multiline anchors
	FlagS                     // dot matches newline
)

func (f FlagSet) Has(g FlagSet) bool { return f&g != 0 }

// Pattern is the parse result: AST plus group metadata.
type Pattern struct {
	Root       *Node
	NumGroups  int
	Names      map[string]int
	MaxBackref int
}
