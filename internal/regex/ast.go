package regex

type Node interface {
	node()
	GetID() int
	setID(int)
	Span() (int, int)
}

type baseNode struct {
	ID        int `json:"id"`
	StartByte int `json:"start_byte"`
	EndByte   int `json:"end_byte"`
}

func (b baseNode) GetID() int       { return b.ID }
func (b *baseNode) setID(id int)    { b.ID = id }
func (b baseNode) Span() (int, int) { return b.StartByte, b.EndByte }

type Root struct {
	baseNode
	Expr   Node    `json:"expr"`
	Groups []Group `json:"groups"`
}

func (*Root) node() {}

type Concat struct {
	baseNode
	Items []Node `json:"items"`
}

func (*Concat) node() {}

type Alternation struct {
	baseNode
	Options []Node `json:"options"`
}

func (*Alternation) node() {}

type Literal struct {
	baseNode
	Rune rune `json:"rune"`
}

func (*Literal) node() {}

type AnyChar struct{ baseNode }

func (*AnyChar) node() {}

type ClassRange struct {
	Lo       rune   `json:"lo"`
	Hi       rune   `json:"hi"`
	Property string `json:"property,omitempty"`
	Negated  bool   `json:"negated,omitempty"`
}

type CharClass struct {
	baseNode
	Negated bool         `json:"negated"`
	Ranges  []ClassRange `json:"ranges"`
}

func (*CharClass) node() {}

type Repeat struct {
	baseNode
	Expr   Node `json:"expr"`
	Min    int  `json:"min"`
	Max    int  `json:"max"`
	HasMax bool `json:"has_max"`
	Greedy bool `json:"greedy"`
}

func (*Repeat) node() {}

const (
	GroupCapture       = "capture"
	GroupNonCapture    = "non_capture"
	LookaheadPositive  = "lookahead_positive"
	LookaheadNegative  = "lookahead_negative"
	LookbehindPositive = "lookbehind_positive"
	LookbehindNegative = "lookbehind_negative"
)

type Group struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
	Kind string `json:"kind"`
}

type GroupExpr struct {
	baseNode
	GroupID int    `json:"group_id"`
	Name    string `json:"name,omitempty"`
	Kind    string `json:"kind"`
	Expr    Node   `json:"expr"`
}

func (*GroupExpr) node() {}

type Backref struct {
	baseNode
	GroupID int    `json:"group_id"`
	Name    string `json:"name,omitempty"`
}

func (*Backref) node() {}

type Anchor struct {
	baseNode
	Kind string `json:"kind"`
}

func (*Anchor) node() {}

func Walk(n Node, fn func(Node) bool) {
	if n == nil || !fn(n) {
		return
	}
	switch v := n.(type) {
	case *Root:
		Walk(v.Expr, fn)
	case *Concat:
		for _, child := range v.Items {
			Walk(child, fn)
		}
	case *Alternation:
		for _, child := range v.Options {
			Walk(child, fn)
		}
	case *Repeat:
		Walk(v.Expr, fn)
	case *GroupExpr:
		Walk(v.Expr, fn)
	}
}

func NumberNodes(n Node) int {
	next := 1
	Walk(n, func(node Node) bool {
		node.setID(next)
		next++
		return true
	})
	return next - 1
}
