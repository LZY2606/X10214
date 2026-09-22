package syntax

// SyntaxVersion pins the pattern grammar understood by this build.
// Every parse / match / diff result carries this string so persisted
// trajectories can never be misread as a different grammar.
const SyntaxVersion = "CTR-grammar-1.0.0"

// NodeKind enumerates the AST node kinds produced by the parser.
type NodeKind string

const (
	NEmpty       NodeKind = "empty"
	NConcat      NodeKind = "concat"
	NAlt         NodeKind = "alternation"
	NRepeat      NodeKind = "repeat"
	NChar        NodeKind = "char"
	NCharClass   NodeKind = "charclass"
	NAny         NodeKind = "any"
	NAnchor      NodeKind = "anchor"
	NGroup       NodeKind = "group"
	NBackref     NodeKind = "backref"
	NLook        NodeKind = "lookaround"
	NFlags       NodeKind = "flags"
)

// Node is one element of the parsed pattern tree.
type Node struct {
	ID   int      `json:"id"`
	Kind NodeKind `json:"kind"`

	// char
	Rune rune `json:"rune,omitempty"`

	// charclass: Ranges are inclusive [lo,hi] rune pairs.
	Negate bool       `json:"negate,omitempty"`
	Ranges []RuneRange `json:"ranges,omitempty"`
	// Classes are Perl class names such as \d \w \s (lowercase positive).
	Classes []string `json:"classes,omitempty"`

	// anchor: "bol" | "eol" | "wb" | "nwb" | "start" | "end" | "endz"
	Anchor string `json:"anchor,omitempty"`

	// repeat
	Min int  `json:"min,omitempty"`
	Max int  `json:"max,omitempty"` // -1 == unbounded
	Greedy bool `json:"greedy,omitempty"`
	Child *Node `json:"child,omitempty"`

	// group
	Index int    `json:"index,omitempty"` // 0 for non-capturing / flags groups
	Name  string `json:"name,omitempty"`
	Capture bool `json:"capture,omitempty"`

	// backref
	RefIndex int    `json:"refIndex,omitempty"`
	RefName  string `json:"refName,omitempty"`

	// lookaround
	Negative bool  `json:"negative,omitempty"`
	Behind   bool  `json:"behind,omitempty"`

	// flags
	SetFlags   string `json:"setFlags,omitempty"`
	ClearFlags string `json:"clearFlags,omitempty"`
	Fold       bool   `json:"fold,omitempty"` // case-insensitive match at this node

	Children []*Node `json:"children,omitempty"`

	Src string `json:"src,omitempty"` // source fragment for display
}

// RuneRange is an inclusive rune interval.
type RuneRange struct {
	Lo rune `json:"lo"`
	Hi rune `json:"hi"`
}

// Pattern is the result of parsing: tree plus capture-group metadata.
type Pattern struct {
	Version string  `json:"syntaxVersion"`
	Root    *Node   `json:"root"`
	Groups  []Group `json:"groups"`
	Flags   FlagSet `json:"flags"`
	Source  string  `json:"source"`
}

// Group describes one capture slot. Index 0 is the whole match.
type Group struct {
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
}

// FlagSet holds the scoped regex flags (Perl-style letters).
type FlagSet struct {
	I bool `json:"i"` // case-insensitive
	M bool `json:"m"` // multiline: ^ $ at line boundaries
	S bool `json:"s"` // dotall: . matches newline
	U bool `json:"u"` // unicode-aware classes (always on; recorded)
}
