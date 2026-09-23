package regex

// Kind enumerates the AST node types produced by the parser.
type Kind int

const (
	KEmpty Kind = iota
	KConcat
	KAlt
	KRepeat
	KGroup
	KChar
	KAny // dot
	KClass
	KBeginLine    // ^
	KEndLine      // $
	KBeginText    // \A
	KEndText      // \z
	KEndTextOptNL // \Z
	KWordBoundary // \b
	KNotWordBoundary
	KBackref
	KLookaround
)

func (k Kind) String() string {
	names := [...]string{
		"empty", "concat", "alt", "repeat", "group", "char", "any", "class",
		"begin-line", "end-line", "begin-text", "end-text", "end-text-opt-nl",
		"word-boundary", "not-word-boundary", "backref", "lookaround",
	}
	if int(k) < 0 || int(k) >= len(names) {
		return "?"
	}
	return names[k]
}

// Node is a deliberately uniform AST element. Fields unused by a given Kind
// stay at zero values; the tree is easy to serialize for the UI.
type Node struct {
	Kind Kind `json:"kind"`
	// Children for concat/alt/group/lookaround.
	Children []*Node `json:"children,omitempty"`
	// Char is the matched rune for KChar.
	Char rune `json:"char,omitempty"`
	// Class for KClass.
	Class *CharClass `json:"class,omitempty"`
	// Repeat for KRepeat.
	Repeat *Quant `json:"repeat,omitempty"`
	// Group metadata for KGroup.
	Index    int    `json:"index,omitempty"`
	Name     string `json:"name,omitempty"`
	Capture  bool   `json:"capture,omitempty"`
	OnFlags  Flags  `json:"onFlags,omitempty"`
	OffFlags Flags  `json:"offFlags,omitempty"`
	// Backref target for KBackref.
	RefIndex int `json:"refIndex,omitempty"`
	// Lookaround metadata for KLookaround.
	LA *Lookaround `json:"lookaround,omitempty"`
	// Src is the source slice that produced the node (for the UI).
	Src TextRange `json:"src"`
}

// TextRange is a byte range inside the original pattern.
type TextRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Flags are pattern options. Only i (case insensitive), m (multiline) and
// s (dot-all) are recognized in CT/1.0.
type Flags uint8

const (
	FIgnoreCase Flags = 1 << iota
	FMultiline
	FDotAll
)

func (f Flags) Has(g Flags) bool   { return f&g == g }
func (f Flags) With(g Flags) Flags { return f | g }
func (f Flags) Without(g Flags) Flags {
	return f &^ g
}

func (f Flags) Slice() []string {
	var out []string
	if f.Has(FIgnoreCase) {
		out = append(out, "i")
	}
	if f.Has(FMultiline) {
		out = append(out, "m")
	}
	if f.Has(FDotAll) {
		out = append(out, "s")
	}
	return out
}

// Quant is a counted quantifier with eagerness.
type Quant struct {
	Min    int  `json:"min"`
	Max    int  `json:"max"` // -1 means unbounded
	Greedy bool `json:"greedy"`
}

// Lookaround describes an assertion node.
type Lookaround struct {
	Positive bool `json:"positive"`
	Behind   bool `json:"behind"`
}

// ClassElem is one member of a character class.
type ClassElem struct {
	// Rune ranges.
	Lo rune `json:"lo"`
	Hi rune `json:"hi"`
	// Predicate classes: \d \w \s and complements, plus Unicode categories.
	Named string `json:"named,omitempty"` // "d","D","w","W","s","S","p"
	Prop  string `json:"prop,omitempty"`  // for \p{...}
}

// CharClass is a bracket expression.
type CharClass struct {
	Negate bool        `json:"negate"`
	Elems  []ClassElem `json:"elems"`
}

// AST is a parsed pattern ready for compilation.
type AST struct {
	Root  *Node  `json:"root"`
	Src   string `json:"src"`
	Flags Flags  `json:"flags"`
	// GroupCount is the number of capturing groups (indices 1..GroupCount).
	GroupCount int            `json:"groupCount"`
	GroupNames map[int]string `json:"groupNames"`
}
