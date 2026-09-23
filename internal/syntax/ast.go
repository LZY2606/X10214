// Package syntax parses grammar v1 regular expressions into an AST.
package syntax

// Node is an AST node.
type Node interface {
	node()
	Pos() int
}

// Empty is the zero-length expression.
type Empty struct{ At int }

func (e *Empty) node()     {}
func (e Empty) Pos() int   { return e.At }

// Lit is a single literal code point (rune index into the source).
type Lit struct {
	At int
	R  rune
}

func (l *Lit) node()    {}
func (l Lit) Pos() int  { return l.At }

// ClassItem is one member of a character class: a literal, a range, a Perl
// class (\d) or a Unicode property (\p{...}).
type ClassItem struct {
	Kind     byte // 'l' literal, 'r' range, 'p' perl, 'u' unicode prop
	R1, R2   rune
	Perl     byte // d/w/s
	Negated  bool // for Perl uppercase classes
	Property string
	NegProp  bool
}

// Class is a bracket expression [..].
type Class struct {
	At      int
	Negated bool
	Items   []ClassItem
}

func (c *Class) node()   {}
func (c Class) Pos() int { return c.At }

// Anchor is a zero-width anchor. Kind is one of ^ $ A z Z b B.
type Anchor struct {
	At   int
	Kind byte
}

func (a *Anchor) node()    {}
func (a Anchor) Pos() int  { return a.At }

// Ref is a backreference.
type Ref struct {
	At     int
	Index  int
	Name   string
	Named  bool
}

func (r *Ref) node()    {}
func (r Ref) Pos() int  { return r.At }

// Group is a capturing or non-capturing group. Inline flag changes live here.
type Group struct {
	At       int
	Capture  bool
	Index    int
	Name     string
	Named    bool
	Flags    string // e.g. "i", "-i"; empty means no change
	Body     Node
}

func (g *Group) node()   {}
func (g Group) Pos() int { return g.At }

// Repeat is a quantified subexpression. Min/Max (Max==-1 means unbounded),
// Lazy selects non-greedy behavior.
type Repeat struct {
	At   int
	Body Node
	Min  int
	Max  int
	Lazy bool
}

func (r *Repeat) node()   {}
func (r Repeat) Pos() int { return r.At }

// Look is a lookaround assertion.
type Look struct {
	At       int
	Behind   bool
	Negative bool
	Body     Node
}

func (l *Look) node()    {}
func (l Look) Pos() int  { return l.At }

// Concat is a sequence of nodes.
type Concat struct {
	At    int
	Items []Node
}

func (c *Concat) node()   {}
func (c Concat) Pos() int { return c.At }

// Alt is an ordered alternation; leftmost branch has highest priority.
type Alt struct {
	At      int
	Branches []Node
}

func (a *Alt) node()   {}
func (a Alt) Pos() int { return a.At }

// Pattern is the parse result.
type Pattern struct {
	Root        Node
	NumGroups   int
	NamedGroups map[string]int
	Source      string
}
