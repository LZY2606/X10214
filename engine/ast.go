package engine

// SyntaxVersion pins the dialect of pattern language implemented by this
// project. Every parse/run/diff result carries it.
const SyntaxVersion = "CTR-syntax-1.0"

// NodeKind enumerates AST node kinds produced by the parser.
type NodeKind string

const (
	NEmpty     NodeKind = "empty"
	NChar      NodeKind = "char"
	NClass     NodeKind = "class"
	NDot       NodeKind = "dot"
	NConcat    NodeKind = "concat"
	NAlt       NodeKind = "alt"
	NRepeat    NodeKind = "repeat"
	NGroup     NodeKind = "group"
	NBackref   NodeKind = "backref"
	NLook      NodeKind = "look"
	NAnchor    NodeKind = "anchor"
	NFlags     NodeKind = "flags"
	NRef       NodeKind = "ref"
)

// ClassTerm is one element inside a character class ([]).
type ClassTerm struct {
	Kind string `json:"kind"` // "rune","range","perl","cat"
	R1   rune   `json:"r1,omitempty"`
	R2   rune   `json:"r2,omitempty"`
	Name string `json:"name,omitempty"` // unicode property name for "cat"
	Neg  bool   `json:"neg,omitempty"`  // \D \W \S or \P{...}
}

// Node is an AST node. Ids are assigned in parse order (depth-first,
// pre-order) so they are stable across identical parses.
type Node struct {
	ID       int        `json:"id"`
	Kind     NodeKind   `json:"kind"`
	Children []*Node    `json:"children,omitempty"`
	R        rune       `json:"r,omitempty"`
	Class    []ClassTerm `json:"class,omitempty"`
	ClassNeg bool       `json:"classNeg,omitempty"`
	Min      int        `json:"min,omitempty"`
	Max      int        `json:"max,omitempty"` // -1 = unlimited
	Greedy   bool       `json:"greedy,omitempty"`
	GroupIdx int        `json:"groupIdx,omitempty"`
	GroupNm  string     `json:"groupName,omitempty"`
	Noncap   bool       `json:"noncap,omitempty"`
	RefIdx   int        `json:"refIdx,omitempty"`
	Ahead    bool       `json:"ahead,omitempty"`
	Positive bool       `json:"positive,omitempty"`
	Anchor   string     `json:"anchor,omitempty"`
	Flags    string     `json:"flags,omitempty"`
}

// Pattern is a parsed pattern: AST plus group table metadata.
type Pattern struct {
	AST        *Node   `json:"ast"`
	NumGroups  int     `json:"numGroups"`
	Names      map[string]int `json:"names"`
	GroupNames map[int]string `json:"groupNames"`
}

// GroupName returns the display name of a capture group ("1","2",... for
// unnamed groups, the explicit name otherwise).
func (p *Pattern) GroupName(i int) string {
	if n, ok := p.GroupNames[i]; ok {
		return n
	}
	return itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [24]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
