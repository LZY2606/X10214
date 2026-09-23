package engine

import (
	"fmt"
	"sort"
	"unicode"
)

const SyntaxVersion = "TrajectoryRegex/1.0.0"

type Node interface {
	nodeType() string
	GetID() string
	setID(string)
}

type baseNode struct{ ID string }

func (b *baseNode) GetID() string  { return b.ID }
func (b *baseNode) setID(v string) { b.ID = v }

type LiteralNode struct {
	baseNode
	Rune    rune
	Invalid bool
}

func (*LiteralNode) nodeType() string { return "literal" }

type DotNode struct{ baseNode }

func (*DotNode) nodeType() string { return "dot" }

type CharClassNode struct {
	baseNode
	Negated       bool
	Ranges        [][2]rune
	ASCIIClass    string
	Categories    []*unicode.RangeTable
	CategoryNames []string
}

func (*CharClassNode) nodeType() string { return "class" }

type AnchorNode struct {
	baseNode
	Kind string
}

func (*AnchorNode) nodeType() string { return "anchor" }

type GroupNode struct {
	baseNode
	Index   int
	Name    string
	Capture bool
	Atomic  bool
	Look    string
	Child   Node
}

func (*GroupNode) nodeType() string { return "group" }

type ConcatNode struct {
	baseNode
	Items []Node
}

func (*ConcatNode) nodeType() string { return "concat" }

type AltNode struct {
	baseNode
	Branches []Node
}

func (*AltNode) nodeType() string { return "alternation" }

type RepeatNode struct {
	baseNode
	Child      Node
	Min        int
	Max        int
	Lazy       bool
	Possessive bool
}

func (*RepeatNode) nodeType() string { return "repeat" }

type BackrefNode struct {
	baseNode
	Index int
	Name  string
}

func (*BackrefNode) nodeType() string { return "backref" }

type LookaroundNode struct {
	baseNode
	Child   Node
	Behind  bool
	Negated bool
}

func (*LookaroundNode) nodeType() string { return "lookaround" }

type Pattern struct {
	Root       Node
	Groups     []GroupName
	Names      map[string]int
	GroupCount int
}

type GroupName struct {
	Index int
	Name  string
}

type ASTNodeView struct {
	ID         string        `json:"id"`
	Type       string        `json:"type"`
	Rune       rune          `json:"rune,omitempty"`
	Invalid    bool          `json:"invalid,omitempty"`
	Negated    bool          `json:"negated,omitempty"`
	Ranges     [][2]rune     `json:"ranges,omitempty"`
	ASCIIClass string        `json:"asciiClass,omitempty"`
	Categories []string      `json:"categories,omitempty"`
	Kind       string        `json:"kind,omitempty"`
	Index      int           `json:"index,omitempty"`
	Name       string        `json:"name,omitempty"`
	Capture    bool          `json:"capture,omitempty"`
	Atomic     bool          `json:"atomic,omitempty"`
	Look       string        `json:"look,omitempty"`
	Behind     bool          `json:"behind,omitempty"`
	Min        int           `json:"min,omitempty"`
	Max        *int          `json:"max,omitempty"`
	Lazy       bool          `json:"lazy,omitempty"`
	Possessive bool          `json:"possessive,omitempty"`
	Children   []ASTNodeView `json:"children,omitempty"`
}

func ASTView(n Node) ASTNodeView {
	v := ASTNodeView{ID: n.GetID(), Type: n.nodeType()}
	switch x := n.(type) {
	case *LiteralNode:
		v.Rune, v.Invalid = x.Rune, x.Invalid
	case *DotNode:
	case *CharClassNode:
		v.Negated, v.Ranges = x.Negated, x.Ranges
		v.ASCIIClass, v.Categories = x.ASCIIClass, append([]string(nil), x.CategoryNames...)
	case *AnchorNode:
		v.Kind = x.Kind
	case *GroupNode:
		v.Index, v.Name, v.Capture, v.Atomic = x.Index, x.Name, x.Capture, x.Atomic
		if x.Child != nil {
			v.Children = []ASTNodeView{ASTView(x.Child)}
		}
	case *ConcatNode:
		for _, c := range x.Items {
			v.Children = append(v.Children, ASTView(c))
		}
	case *AltNode:
		for _, c := range x.Branches {
			v.Children = append(v.Children, ASTView(c))
		}
	case *RepeatNode:
		v.Min, v.Lazy, v.Possessive = x.Min, x.Lazy, x.Possessive
		if x.Max >= 0 {
			max := x.Max
			v.Max = &max
		}
		v.Children = []ASTNodeView{ASTView(x.Child)}
	case *BackrefNode:
		v.Index, v.Name = x.Index, x.Name
	case *LookaroundNode:
		v.Behind, v.Negated = x.Behind, x.Negated
		v.Children = []ASTNodeView{ASTView(x.Child)}
	default:
		panic(fmt.Sprintf("unknown node %T", n))
	}
	return v
}

func AssignIDs(p *Pattern) {
	counter := 0
	var walk func(Node)
	walk = func(n Node) {
		counter++
		n.setID(fmt.Sprintf("n%d", counter))
		for _, c := range children(n) {
			walk(c)
		}
	}
	walk(p.Root)
}

func children(n Node) []Node {
	switch x := n.(type) {
	case *GroupNode:
		if x.Child == nil {
			return nil
		}
		return []Node{x.Child}
	case *ConcatNode:
		return x.Items
	case *AltNode:
		return x.Branches
	case *RepeatNode:
		return []Node{x.Child}
	case *LookaroundNode:
		return []Node{x.Child}
	default:
		return nil
	}
}

func SortedGroupNames(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
