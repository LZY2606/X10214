package regex

// NodeKind enumerates AST node kinds.
type NodeKind string

const (
	NEmpty      NodeKind = "Empty"
	NLiteral    NodeKind = "Literal"
	NClass      NodeKind = "Class"
	NConcat     NodeKind = "Concat"
	NAlt        NodeKind = "Alt"
	NRepeat     NodeKind = "Repeat"
	NGroup      NodeKind = "Group"
	NBackref    NodeKind = "Backref"
	NAnchor     NodeKind = "Anchor"
	NLookaround NodeKind = "Lookaround"
)

// AnchorKind enumerates zero-width anchors.
type AnchorKind string

const (
	ABeginText   AnchorKind = "BeginText"   // \A
	AEndText     AnchorKind = "EndText"     // \z
	ABoundaryEnd AnchorKind = "BoundaryEnd" // \Z
	ABeginLine   AnchorKind = "BeginLine"   // ^
	AEndLine     AnchorKind = "EndLine"     // $
	AWordBound   AnchorKind = "WordBound"   // \b
	ANotWordBound AnchorKind = "NotWordBound" // \B
	APrevMatch   AnchorKind = "PrevMatch"   // \G
)

// Node is a concrete AST node. Fields that do not apply to a given kind are
// left at zero values; Pos is the pattern byte offset for breakpoint mapping.
type Node struct {
	ID       int      `json:"id"`
	Kind     NodeKind `json:"kind"`
	Pos      int      `json:"pos"`
	Children []*Node  `json:"children,omitempty"`

	// Literal.
	Rune rune `json:"rune,omitempty"`

	// Class.
	Class ClassAST `json:"class,omitempty"`

	// Repeat.
	Min  int    `json:"min"`
	Max  int    `json:"max"` // -1 means unbounded
	Greedy bool `json:"greedy"`
	Possessive bool `json:"possessive"`

	// Group.
	Capture bool   `json:"capture,omitempty"`
	Index   int    `json:"index,omitempty"`
	Name    string `json:"name,omitempty"`
	Flags   *Flags `json:"flags,omitempty"`

	// Backref.
	RefIndex int    `json:"refIndex,omitempty"`
	RefName  string `json:"refName,omitempty"`

	// Anchor.
	Anchor AnchorKind `json:"anchor,omitempty"`

	// Lookaround.
	Behind bool `json:"behind,omitempty"`
	Negate bool `json:"negate,omitempty"`
}

// Flags records per-group inline (?imsx-imsx) settings.
type Flags struct {
	I bool // case-insensitive (ASCII fold; unicode simple fold via unicode.SimpleFold)
	M bool // multiline: ^ $ at line boundaries
	S bool // dotall: dot matches newline
	X bool // free-spacing/verbose: whitespace and unescaped # are ignored
	Negate []string `json:"negate,omitempty"`
}

// ClassItem is one member of a character class.
type ClassItem struct {
	Type     ClassItemType `json:"type"`
	Lo, Hi   rune          `json:"lo,omitempty,hi,omitempty"` // Type == CIRange / CISingle
	Shorthand byte         `json:"shorthand,omitempty"`       // Type == CIShorthand: w d s W D S
	Property string        `json:"property,omitempty"`        // Type == CIProperty
	Negate   bool          `json:"negate,omitempty"`          // \P{...}
}

// ClassItemType enumerates class member types.
type ClassItemType string

const (
	CISingle    ClassItemType = "single"
	CIRange     ClassItemType = "range"
	CIShorthand ClassItemType = "shorthand"
	CIProperty  ClassItemType = "property"
)

// ClassAST is a [...] character class.
type ClassAST struct {
	Negated bool        `json:"negated"`
	Items   []ClassItem `json:"items"`
}

// Pattern is a fully parsed regular expression.
type Pattern struct {
	AST         *Node         `json:"ast"`
	NumGroups   int           `json:"numGroups"`
	Names       map[string]int `json:"names"`
	Syntax      string        `json:"syntaxVersion"`
	UnicodeData string        `json:"unicodeDataVersion"`
}

// Parse parses pattern text using dialect SyntaxVersion.
func Parse(text string) (*Pattern, error) {
	p := &parser{text: text, names: map[string]int{}}
	node, err := p.parseExpr(&Flags{})
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.text) {
		return nil, p.errorf("unexpected trailing input at %d", p.pos)
	}
	return &Pattern{
		AST: node, NumGroups: p.groupCount, Names: p.names,
		Syntax: SyntaxVersion, UnicodeData: UnicodeDataVersion,
	}, nil
}
