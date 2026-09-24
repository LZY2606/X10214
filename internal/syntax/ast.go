// Package syntax parses pattern text into an AST. It implements the
// project's own grammar; it never delegates to the host regexp package.
package syntax

// Version pins the syntax grammar version reported in every result.
const Version = "gsb.syntax/1.0.0"

// MaxRepeat caps bounded repeat expansion ({n,m}).
const MaxRepeat = 1000

type Kind int

const (
	Empty Kind = iota
	Literal
	CharClass
	AnyChar
	Concat
	Alternate
	Repeat
	Group   // capturing group
	Backref // backreference by index or name
	Anchor  // ^ $ \A \z \b \B
	Lookaround
)

func (k Kind) String() string {
	switch k {
	case Empty:
		return "Empty"
	case Literal:
		return "Literal"
	case CharClass:
		return "CharClass"
	case AnyChar:
		return "AnyChar"
	case Concat:
		return "Concat"
	case Alternate:
		return "Alternate"
	case Repeat:
		return "Repeat"
	case Group:
		return "Group"
	case Backref:
		return "Backref"
	case Anchor:
		return "Anchor"
	case Lookaround:
		return "Lookaround"
	}
	return "?"
}

type AnchorKind int

const (
	AnchorStart           AnchorKind = iota // ^
	AnchorEnd                               // $
	AnchorTextStart                         // \A
	AnchorTextEnd                           // \z
	AnchorWordBoundary                      // \b
	AnchorNotWordBoundary                   // \B
)

func (a AnchorKind) String() string {
	switch a {
	case AnchorStart:
		return "^"
	case AnchorEnd:
		return "$"
	case AnchorTextStart:
		return `\A`
	case AnchorTextEnd:
		return `\z`
	case AnchorWordBoundary:
		return `\b`
	case AnchorNotWordBoundary:
		return `\B`
	}
	return "?"
}

type LookKind int

const (
	LookAhead     LookKind = iota // (?=...)
	LookAheadNeg                  // (?!...)
	LookBehind                    // (?<=...)
	LookBehindNeg                 // (?<!...)
)

func (l LookKind) String() string {
	switch l {
	case LookAhead:
		return "(?="
	case LookAheadNeg:
		return "(?!"
	case LookBehind:
		return "(?<="
	case LookBehindNeg:
		return "(?<!"
	}
	return "?"
}

// Positive reports whether the assertion requires a match.
func (l LookKind) Positive() bool { return l == LookAhead || l == LookBehind }

// Range is an inclusive rune range inside a character class.
type Range struct {
	Lo, Hi rune
}

type Node struct {
	Kind     Kind
	Lit      string   // Literal
	Ranges   []Range  // CharClass
	Cats     []string // CharClass unicode categories, e.g. "L", "Lu"
	Preds    []string // CharClass builtin predicates: digit, word, space
	Negate   bool     // CharClass negation
	Children []*Node
	Min      int // Repeat lower bound
	Max      int // Repeat upper bound; -1 unbounded
	Lazy     bool
	Index    int        // Group index / Backref target index
	Name     string     // Group name / Backref target name
	Anchor   AnchorKind // Anchor
	Look     LookKind   // Lookaround
}

// Info carries group metadata collected during parsing.
type Info struct {
	Groups int
	Names  map[string]int
}
