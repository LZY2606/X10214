package engine

type Node interface { astNode() }

type GroupSpec struct {
	Index int
	Name  string
}

type SeqNode struct{ Nodes []Node }
type AltNode struct{ Options []Node }
type LiteralNode struct {
	Rune rune
	Byte byte
	Raw  bool
}
type AnyNode struct{}
type ClassNode struct {
	Negated bool
	Items   []ClassItem
}
type ClassItem interface{ classItem() }
type ClassChar struct {
	Rune rune
	Byte byte
	Raw  bool
}
type ClassRange struct{ From, To rune }
type ClassProperty struct {
	Name  string
	Value string
	Negated bool
}
type AnchorNode struct{ Kind string }
type GroupNode struct {
	Spec     *GroupSpec
	Look     string
	Child    Node
}
type RefNode struct {
	Name string
	Num  int
}
type QuantNode struct {
	Child Node
	Min   int
	Max   int
	Lazy  bool
}

func (*SeqNode) astNode()      {}
func (*AltNode) astNode()      {}
func (*LiteralNode) astNode()  {}
func (*AnyNode) astNode()      {}
func (*ClassNode) astNode()    {}
func (*AnchorNode) astNode()   {}
func (*GroupNode) astNode()    {}
func (*RefNode) astNode()      {}
func (*QuantNode) astNode()     {}
func (*ClassChar) classItem()  {}
func (*ClassRange) classItem() {}
func (*ClassProperty) classItem() {}

type ParsedPattern struct {
	Root      Node
	Groups    []GroupSpec
	Names     map[string]int
	Versions  Versions
}
