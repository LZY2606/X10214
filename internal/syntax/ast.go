package syntax

import "fmt"

// NodeKind 枚举 AST 节点种类。所有节点自带 NodeID，供断点与轨迹定位使用。
type NodeKind int

const (
	KEmpty NodeKind = iota
	KConcat
	KAlt
	KRepeat
	KChar
	KClass
	KAny
	KAnchor
	KGroup
	KFlagGroup
	KBackRef
	KLookaround
)

// Anchor 种类。
type Anchor int

const (
	ABegin Anchor = iota // \A 或无 m 标志的 ^：仅文本起点
	ABeginLine
	AEnd
	AEndLine      // \Z：文本终点前允许单个换行
	AEndOptNL     // $ 无 m：文本终点，或终点前的换行
	AWordBoundary // \b
	ANonWordBound // \B
)

// Flags 为模式/分组生效的编译标志。
type Flags struct {
	I bool // 大小写不敏感
	M bool // 多行：^ $ 按行
	S bool // dotall：. 匹配换行
}

// Node 为正则模式解析后的抽象语法树节点。
type Node struct {
	ID   NodeID
	Kind NodeKind

	// 重复
	Child  *Node
	Min    int
	Max    int // -1 表示无限
	Greedy bool

	// 连接与分支
	Children []*Node

	// 字符 / 任意字符
	Rune  rune
	Class *ClassNode
	Flags Flags

	// 锚点
	Anchor Anchor

	// 捕获组
	Capture  bool
	GroupIdx int
	Name     string

	// 标志组
	Inner    *Node
	AddFlags Flags
	DelFlags Flags

	// 反向引用
	RefIdx  int
	RefName string

	// lookaround
	Neg    bool
	Behind bool

	SrcPos int // 模式字节偏移（用于报错与展示）
	SrcLen int
}

// NodeID 为 AST 节点的稳定编号。
type NodeID int

// ClassNode 为字符类（[...]）的可执行描述。
type ClassNode struct {
	Negate  bool
	Ranges  []RuneRange
	Classes []byte // 'd','D','w','W','s','S'
	Unicode []UnicodeProp
}

// RuneRange 为闭区间 [Lo, Hi]，Lo==Hi 表示单字符。
type RuneRange struct {
	Lo, Hi rune
}

// UnicodeProp 为 \p{...} 属性匹配项，Negate 表示 \P{...}。
type UnicodeProp struct {
	Name   string
	Negate bool
}

func (n *Node) label() string {
	switch n.Kind {
	case KEmpty:
		return "Empty"
	case KConcat:
		return "Concat"
	case KAlt:
		return "Alt"
	case KRepeat:
		max := "∞"
		if n.Max >= 0 {
			max = fmt.Sprintf("%d", n.Max)
		}
		g := "greedy"
		if !n.Greedy {
			g = "lazy"
		}
		return fmt.Sprintf("Repeat{%d,%s} %s", n.Min, max, g)
	case KChar:
		return fmt.Sprintf("Char %q", n.Rune)
	case KClass:
		return "Class"
	case KAny:
		return "Any"
	case KAnchor:
		return "Anchor " + n.Anchor.String()
	case KGroup:
		if n.Capture {
			if n.Name != "" {
				return fmt.Sprintf("Capture #%d <%s>", n.GroupIdx, n.Name)
			}
			return fmt.Sprintf("Capture #%d", n.GroupIdx)
		}
		return "Group"
	case KFlagGroup:
		return "Flags"
	case KBackRef:
		if n.RefName != "" {
			return fmt.Sprintf("BackRef <%s>", n.RefName)
		}
		return fmt.Sprintf("BackRef #%d", n.RefIdx)
	case KLookaround:
		s := "Lookahead"
		if n.Behind {
			s = "Lookbehind"
		}
		if n.Neg {
			s = "Negative" + s
		} else {
			s = "Positive" + s
		}
		return s
	}
	return "?"
}

func (a Anchor) String() string {
	switch a {
	case ABegin:
		return "\\A"
	case ABeginLine:
		return "^"
	case AEnd:
		return "\\z"
	case AEndLine:
		return "\\Z"
	case AEndOptNL:
		return "$"
	case AWordBoundary:
		return "\\b"
	case ANonWordBound:
		return "\\B"
	}
	return "?"
}

// Describe 返回节点用于页面展示的简短中文描述。
func (n *Node) Describe() string {
	switch n.Kind {
	case KEmpty:
		return "空（零宽）"
	case KConcat:
		return "连接"
	case KAlt:
		return "分支 alternation"
	case KRepeat:
		max := "无限"
		if n.Max >= 0 {
			max = fmt.Sprintf("%d", n.Max)
		}
		g := "贪婪"
		if !n.Greedy {
			g = "惰性"
		}
		return fmt.Sprintf("重复 %d..%s（%s）", n.Min, max, g)
	case KChar:
		return fmt.Sprintf("字符 %q", n.Rune)
	case KClass:
		return "字符类"
	case KAny:
		return "任意字符 ."
	case KAnchor:
		return "锚点 " + n.Anchor.String()
	case KGroup:
		if n.Capture {
			if n.Name != "" {
				return fmt.Sprintf("命名捕获组 #%d %q", n.GroupIdx, n.Name)
			}
			return fmt.Sprintf("捕获组 #%d", n.GroupIdx)
		}
		return "非捕获组"
	case KFlagGroup:
		return "标志组"
	case KBackRef:
		if n.RefName != "" {
			return fmt.Sprintf("反向引用 %q", n.RefName)
		}
		return fmt.Sprintf("反向引用 #%d", n.RefIdx)
	case KLookaround:
		s := "正向"
		if n.Neg {
			s = "负向"
		}
		if n.Behind {
			s += "后顾"
		} else {
			s += "先行"
		}
		return s
	}
	return ""
}
