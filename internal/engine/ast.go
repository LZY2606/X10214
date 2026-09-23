package engine

// Node is one AST node. The node kinds below are the complete grammar of
// CT-REGEX-1.0. Every node carries its source span for the trace UI.
type Node interface {
	nodeMarker()
	Span() (start, end int)
}

type baseNode struct{ S, E int }

func (b baseNode) Span() (int, end int) { return b.S, b.E }

// Concat is an ordered sequence of nodes.
type Concat struct {
	baseNode
	Nodes []Node `json:"nodes"`
}

// Alt is alternation a|b|c. Branch order is the documented tie-break order
// and must never be reordered by optimizations.
type Alt struct {
	baseNode
	Branches []Node `json:"branches"`
}

// Repeat is a quantified node. Max == -1 means unbounded.
type Repeat struct {
	baseNode
	Child Node `json:"child"`
	Min   int  `json:"min"`
	Max   int  `json:"max"`
	Lazy  bool `json:"lazy"`
}

// Group is a capturing group (Index >= 1) or an anonymous group with Index==0
// (emitted by bare flag directives).
type Group struct {
	baseNode
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
	Child Node   `json:"child"`
}

// Lookaround is (?=), (?!), (?<=), (?<!).
type Lookaround struct {
	baseNode
	Negative bool `json:"negative"`
	Behind   bool `json:"behind"`
	Child    Node `json:"child"`
}

// Char is one literal code point.
type Char struct {
	baseNode
	R  rune `json:"r"`
	CI bool `json:"ci"`
}

// RuneRange is an inclusive rune interval.
type RuneRange struct {
	Lo rune `json:"lo"`
	Hi rune `json:"hi"`
}

// PropRef names a Unicode property/shorthand; Negated inverts this one member.
// Synthetic names __w __d __s represent the \w \d \s shorthand classes.
type PropRef struct {
	Name    string `json:"name"`
	Negated bool   `json:"negated"`
}

// CharClass is a bracket expression, possibly negated (^).
type CharClass struct {
	baseNode
	Negated bool        `json:"negated"`
	CI      bool        `json:"ci"`
	Ranges  []RuneRange `json:"ranges"`
	Props   []PropRef   `json:"props"`
}

// AnyChar is "." (matches every rune except \n unless DotAll).
type AnyChar struct {
	baseNode
	DotAll bool `json:"dotAll"`
}

// AnchorKind enumerates the zero-width assertions.
type AnchorKind string

const (
	AnchorStartLine AnchorKind = "^" // start of text (and after \n in multiline)
	AnchorEndLine   AnchorKind = "$" // end of text (and before \n in multiline)
	AnchorStartText AnchorKind = `\A`
	AnchorEndText   AnchorKind = `\z`
	AnchorEndTextNL AnchorKind = `\Z` // end of text or trailing \n
	AnchorWord      AnchorKind = `\b`
	AnchorNotWord   AnchorKind = `\B`
)

// Anchor is a zero-width position assertion.
type Anchor struct {
	baseNode
	Kind     AnchorKind `json:"kind"`
	Multiline bool      `json:"multiline"`
}

// Ref is a backreference \1 or \k<name>. Group existence is resolved by the
// compiler, not the parser, so forward references are legal.
type Ref struct {
	baseNode
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
	CI    bool   `json:"ci"`
}

func (*Concat) nodeMarker()     {}
func (*Alt) nodeMarker()        {}
func (*Repeat) nodeMarker()     {}
func (*Group) nodeMarker()      {}
func (*Lookaround) nodeMarker() {}
func (*Char) nodeMarker()       {}
func (*CharClass) nodeMarker()  {}
func (*AnyChar) nodeMarker()    {}
func (*Anchor) nodeMarker()     {}
func (*Ref) nodeMarker()        {}

// Pattern is the parse result: root AST plus the capture table.
type Pattern struct {
	Root     Node            `json:"root"`
	Groups   []GroupInfo     `json:"groups"`
	Names    map[string]int  `json:"names"`
	Source   string          `json:"source"`
	Flags    FlagState       `json:"flags"`
}

// GroupInfo describes one capture slot; group 0 is the whole match.
type GroupInfo struct {
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
}

// FlagState is the set of supported modifiers (CT-REGEX-1.0).
type FlagState struct {
	CI        bool `json:"i"` // case-insensitive
	Multiline bool `json:"m"` // ^ $ match at line boundaries
	DotAll    bool `json:"s"` // dot matches newline
}

func (f FlagState) with(on, off string) FlagState {
	g := f
	for _, c := range on {
		switch c {
		case 'i':
			g.CI = true
		case 'm':
			g.Multiline = true
		case 's':
			g.DotAll = true
		}
	}
	for _, c := range off {
		switch c {
		case 'i':
			g.CI = false
		case 'm':
			g.Multiline = false
		case 's':
			g.DotAll = false
		}
	}
	return g
}
