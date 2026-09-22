package regex

// Node is one AST node produced by the parser.
type Node interface{ astNode() }

type (
	// Concat is an ordered sequence.
	Concat struct{ Items []Node }
	// Alt is alternation; branches are tried in source order.
	Alt struct{ Branches []Node }
	// Repeat applies a quantifier to Child.
	Repeat struct {
		Child      Node
		Min, Max   int // Max == -1 means unbounded
		Greedy     bool
		Possessive bool
	}
	// Group is a capturing, lookaround, atomic or flag group.
	Group struct {
		Child Node
		Kind  GroupKind
		Index int    // capture index, 0 for non-capture
		Name  string // named capture, may be empty
		// flag-only groups: add flags to the remainder of this group.
		AddFlags, DelFlags string
	}
	// Char matches one element.
	Char struct {
		Code rune // RuneMax for '.' (any)
		Dot  bool
	}
	// Class matches one element via predicate terms.
	Class struct {
		Negated bool
		Terms   []ClassTerm
	}
	// Anchor is a zero-width assertion.
	Anchor struct{ Kind AnchorKind }
	// Backref refers to a capture group by number or name.
	Backref struct {
		Ref  int
		Name string
	}
	// Empty always matches, zero width.
	Empty struct{}
)

func (*Concat) astNode()  {}
func (*Alt) astNode()     {}
func (*Repeat) astNode()  {}
func (*Group) astNode()   {}
func (*Char) astNode()    {}
func (*Class) astNode()   {}
func (*Anchor) astNode()  {}
func (*Backref) astNode() {}
func (*Empty) astNode()   {}

// GroupKind enumerates parentheses forms.
type GroupKind uint8

const (
	GroupCapture GroupKind = iota
	GroupNonCap
	GroupAtomic
	GroupLookAhead
	GroupNegLookAhead
	GroupLookBehind
	GroupNegLookBehind
	GroupFlags
)

// AnchorKind enumerates zero-width anchors.
type AnchorKind uint8

const (
	AnchorSOL      AnchorKind = iota // ^
	AnchorEOL                        // $
	AnchorStart                      // \A
	AnchorEnd                        // \z
	AnchorEndNL                      // \Z
	AnchorWordB                      // \b
	AnchorNotWordB                   // \B
)

// ClassTerm is one alternative term inside a character class.
type ClassTerm struct {
	Lo, Hi rune // inclusive range; Lo == Hi for a single value
	Kind   ClassTermKind
	Prop   string // \p{...} value, lowercased, underscores removed
}

type ClassTermKind uint8

const (
	TermRange ClassTermKind = iota
	TermProp
)

// Flags are the supported per-pattern / inline flags.
type Flags struct {
	I bool // case insensitive (simple fold)
	M bool // multiline: ^ and $ at internal newlines
	S bool // dotall: . matches newline
	U bool // unicode: \w\d\s and word boundaries use Unicode tables
}

func (fl Flags) has(f byte) bool {
	switch f {
	case 'i':
		return fl.I
	case 'm':
		return fl.M
	case 's':
		return fl.S
	case 'u':
		return fl.U
	}
	return false
}

func applyFlags(fl Flags, add, del string) Flags {
	for _, c := range add {
		switch c {
		case 'i':
			fl.I = true
		case 'm':
			fl.M = true
		case 's':
			fl.S = true
		case 'u':
			fl.U = true
		}
	}
	for _, c := range del {
		switch c {
		case 'i':
			fl.I = false
		case 'm':
			fl.M = false
		case 's':
			fl.S = false
		case 'u':
			fl.U = false
		}
	}
	return fl
}
