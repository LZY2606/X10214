package syntax

import "fmt"

// NodeType 标识 AST 节点种类。
type NodeType string

const (
	NEmpty      NodeType = "empty"
	NConcat     NodeType = "concat"
	NAlt        NodeType = "alternation"
	NRepeat     NodeType = "repeat"
	NCharClass  NodeType = "charclass"
	NAnchor     NodeType = "anchor"
	NCapture    NodeType = "capture"
	NBackref    NodeType = "backref"
	NLookahead  NodeType = "lookahead"
	NLookbehind NodeType = "lookbehind"
)

// Quant 描述 {min,max} 量词。max 为 -1 表示无上界。
type Quant struct {
	Min      int
	Max      int
	Lazy     bool
	HasBound bool
}

// Node 是模式 AST 的节点。
type Node struct {
	Type NodeType `json:"type"`
	ID   int      `json:"id"`
	// 模式源码区间（字节偏移）。
	SrcFrom int `json:"srcFrom"`
	SrcTo   int `json:"srcTo"`

	// Concat / Alternation
	Children []*Node `json:"children,omitempty"`

	// Repeat
	Child *Node  `json:"child,omitempty"`
	Quant Quant `json:"quant,omitempty"`

	// CharClass：归一化后的区间集合（rune 区间），Neg 表示求补。
	Ranges []RuneRange `json:"ranges,omitempty"`
	Neg    bool        `json:"neg,omitempty"`
	Single bool       `json:"single,omitempty"` // 单字符字面量（也记录区间）

	// Anchor
	AnchorKind string `json:"anchorKind,omitempty"` // bos,eos,bosNonLine,eosNonLine,wordBoundary,notWordBoundary

	// Capture
	Index int    `json:"index,omitempty"`
	Name  string `json:"name,omitempty"`

	// Backref
	RefIndex int `json:"refIndex,omitempty"`

	// Lookahead / Lookbehind
	Negative bool `json:"negative,omitempty"`
}

// RuneRange 是闭区间 [Lo,Hi]；Hi == Lo 表示单个 rune。
type RuneRange struct {
	Lo rune
	Hi rune
}

func (n *Node) String() string {
	if n == nil {
		return "<nil>"
	}
	switch n.Type {
	case NEmpty:
		return "ε"
	case NConcat:
		s := ""
		for i, c := range n.Children {
			if i > 0 {
				s += "·"
			}
			s += c.String()
		}
		return s
	case NAlt:
		s := ""
		for i, c := range n.Children {
			if i > 0 {
				s += "|"
			}
			s += c.String()
		}
		return "(" + s + ")"
	case NRepeat:
		q := ""
		switch {
		case n.Quant.Min == 0 && n.Quant.Max == -1:
			q = "*"
		case n.Quant.Min == 1 && n.Quant.Max == -1:
			q = "+"
		case n.Quant.Min == 0 && n.Quant.Max == 1:
			q = "?"
		case n.Quant.Max == -1:
			q = fmt.Sprintf("{%d,}", n.Quant.Min)
		case n.Quant.Min == n.Quant.Max:
			q = fmt.Sprintf("{%d}", n.Quant.Min)
		default:
			q = fmt.Sprintf("{%d,%d}", n.Quant.Min, n.Quant.Max)
		}
		if n.Quant.Lazy {
			q += "?"
		}
		return n.Child.String() + q
	case NCharClass:
		return fmt.Sprintf("class(id=%d,neg=%v,n=%d)", n.ID, n.Neg, len(n.Ranges))
	case NAnchor:
		return "<" + n.AnchorKind + ">"
	case NCapture:
		name := ""
		if n.Name != "" {
			name = "?" + n.Name + " "
		}
		return fmt.Sprintf("(<%s#%d>%s)", name, n.Index, n.Child.String())
	case NBackref:
		return fmt.Sprintf("\\#%d", n.RefIndex)
	case NLookahead:
		s := "?="
		if n.Negative {
			s = "?!"
		}
		return "(:" + s + n.Child.String() + ")"
	case NLookbehind:
		s := "?<="
		if n.Negative {
			s = "?<!"
		}
		return "(:" + s + n.Child.String() + ")"
	}
	return string(n.Type)
}
