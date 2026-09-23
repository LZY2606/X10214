package regex

import "fmt"

const (
	SyntaxVersion      = "trailroom-syntax/1.0.0"
	UnicodeDataVersion = unicodeDataVersion
)

type OpKind int

const (
	OpChar OpKind = iota + 1
	OpAny
	OpClass
	OpBackref
	OpAnchor
	OpSave
	OpSplit
	OpJmp
	OpMatch
	OpLookahead
	OpAssertEnd
	OpLookbehind
	OpLBEnd
	OpLoopStart
	OpLoopEnd
)

type Op struct {
	Kind     OpKind     `json:"kind"`
	NodeID   int        `json:"node_id"`
	Rune     rune       `json:"rune,omitempty"`
	Class    *CharClass `json:"class,omitempty"`
	Group    int        `json:"group,omitempty"`
	SaveEnd  bool       `json:"save_end,omitempty"`
	A        int        `json:"a,omitempty"`
	B        int        `json:"b,omitempty"`
	Target   int        `json:"target,omitempty"`
	Negative bool       `json:"negative,omitempty"`
	EndPC    int        `json:"end_pc,omitempty"`
	Width    int        `json:"width,omitempty"`
	Min      int        `json:"min,omitempty"`
	Max      int        `json:"max,omitempty"`
	HasMax   bool       `json:"has_max,omitempty"`
	Greedy   bool       `json:"greedy,omitempty"`
}

type Program struct {
	Ops    []Op `json:"ops"`
	Groups int  `json:"groups"`
}

type compiler struct {
	ops []Op
}

func Compile(root *Root) (*Program, error) {
	c := &compiler{}
	c.emit(Op{Kind: OpJmp})
	if err := c.compile(root.Expr); err != nil {
		return nil, err
	}
	c.emit(Op{Kind: OpMatch})
	c.patch(0, 1)
	return &Program{Ops: c.ops, Groups: len(root.Groups)}, nil
}

func (c *compiler) emit(op Op) int {
	op.NodeID = op.NodeID
	c.ops = append(c.ops, op)
	return len(c.ops) - 1
}
func (c *compiler) here() int            { return len(c.ops) }
func (c *compiler) patch(pc, target int) { c.ops[pc].Target = target }

func (c *compiler) compile(n Node) error {
	switch v := n.(type) {
	case *Root:
		return c.compile(v.Expr)
	case *Concat:
		for _, item := range v.Items {
			if err := c.compile(item); err != nil {
				return err
			}
		}
	case *Alternation:
		jumps := []int{}
		for i, opt := range v.Options {
			if i < len(v.Options)-1 {
				split := c.emit(Op{Kind: OpSplit, NodeID: v.GetID(), A: c.here() + 1, B: 0, Greedy: true})
				if err := c.compile(opt); err != nil {
					return err
				}
				jumps = append(jumps, c.emit(Op{Kind: OpJmp}))
				next := c.here()
				c.ops[split].B = next
			} else if err := c.compile(opt); err != nil {
				return err
			}
		}
		merge := c.here()
		for _, pc := range jumps {
			c.patch(pc, merge)
		}
	case *Literal:
		c.emit(Op{Kind: OpChar, NodeID: v.GetID(), Rune: v.Rune})
	case *AnyChar:
		c.emit(Op{Kind: OpAny, NodeID: v.GetID()})
	case *CharClass:
		c.emit(Op{Kind: OpClass, NodeID: v.GetID(), Class: v})
	case *Backref:
		c.emit(Op{Kind: OpBackref, NodeID: v.GetID(), Group: v.GroupID})
	case *Anchor:
		c.emit(Op{Kind: OpAnchor, NodeID: v.GetID(), Rune: anchorCode(v.Kind)})
	case *GroupExpr:
		return c.compileGroup(v)
	case *Repeat:
		return c.compileRepeat(v)
	default:
		return fmt.Errorf("unsupported AST node %T", n)
	}
	return nil
}

func (c *compiler) compileGroup(v *GroupExpr) error {
	switch v.Kind {
	case GroupCapture:
		c.emit(Op{Kind: OpSave, NodeID: v.GetID(), Group: v.GroupID})
		if err := c.compile(v.Expr); err != nil {
			return err
		}
		c.emit(Op{Kind: OpSave, NodeID: v.GetID(), Group: v.GroupID, SaveEnd: true})
	case GroupNonCapture:
		if err := c.compile(v.Expr); err != nil {
			return err
		}
	case LookaheadPositive, LookaheadNegative:
		body := c.here() + 1
		la := c.emit(Op{Kind: OpLookahead, NodeID: v.GetID(), Negative: v.Kind == LookaheadNegative, A: body, B: 0})
		if err := c.compile(v.Expr); err != nil {
			return err
		}
		end := c.emit(Op{Kind: OpAssertEnd, NodeID: v.GetID()})
		c.ops[la].EndPC = end
		c.ops[la].B = c.here()
	case LookbehindPositive, LookbehindNegative:
		width, ok := fixedWidth(v.Expr)
		if !ok {
			return &ParseError{Offset: v.StartByte, Msg: "only fixed-width lookbehind is supported"}
		}
		body := c.here() + 1
		lb := c.emit(Op{Kind: OpLookbehind, NodeID: v.GetID(), Negative: v.Kind == LookbehindNegative, Width: width, A: body, B: 0})
		if err := c.compile(v.Expr); err != nil {
			return err
		}
		c.emit(Op{Kind: OpLBEnd, NodeID: v.GetID(), Width: width})
		c.ops[lb].B = c.here()
	}
	return nil
}

func (c *compiler) compileRepeat(v *Repeat) error {
	start := c.here()
	c.emit(Op{Kind: OpLoopStart, NodeID: v.GetID(), Min: v.Min, Max: v.Max, HasMax: v.HasMax, Greedy: v.Greedy, Target: start})
	body := c.here()
	if err := c.compile(v.Expr); err != nil {
		return err
	}
	le := c.emit(Op{Kind: OpLoopEnd, NodeID: v.GetID(), Min: v.Min, Max: v.Max, HasMax: v.HasMax, Greedy: v.Greedy, Target: start, A: body})
	jmp := c.emit(Op{Kind: OpJmp, Target: start})
	after := c.here()
	c.ops[start].EndPC = after
	c.ops[le].EndPC = after
	_ = jmp
	return nil
}

func anchorCode(kind string) rune {
	switch kind {
	case "start":
		return '^'
	case "end":
		return '$'
	case "absolute_start":
		return 'A'
	case "absolute_end":
		return 'z'
	case "word_boundary":
		return 'b'
	case "not_word_boundary":
		return 'B'
	}
	return 0
}

func fixedWidth(n Node) (int, bool) {
	switch v := n.(type) {
	case *Literal:
		return utf8RuneLen(v.Rune), true
	case *AnyChar, *CharClass:
		return 1, true
	case *Backref, *Anchor:
		return 0, true
	case *Concat:
		total := 0
		for _, item := range v.Items {
			w, ok := fixedWidth(item)
			if !ok {
				return 0, false
			}
			total += w
		}
		return total, true
	case *Alternation:
		if len(v.Options) == 0 {
			return 0, true
		}
		w, ok := fixedWidth(v.Options[0])
		if !ok {
			return 0, false
		}
		for _, item := range v.Options[1:] {
			w2, ok := fixedWidth(item)
			if !ok || w2 != w {
				return 0, false
			}
		}
		return w, true
	case *Repeat:
		if !v.HasMax {
			return 0, false
		}
		w, ok := fixedWidth(v.Expr)
		return w * v.Max, ok
	case *GroupExpr:
		if v.Kind != GroupCapture && v.Kind != GroupNonCapture {
			return 0, false
		}
		return fixedWidth(v.Expr)
	}
	return 0, false
}

func utf8RuneLen(r rune) int {
	switch {
	case r < 0x80:
		return 1
	case r < 0x800:
		return 2
	case r < 0x10000:
		return 3
	default:
		return 4
	}
}
