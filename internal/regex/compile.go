package regex

import "fmt"

// Op is a program instruction opcode.
type Op int

const (
	OpRune    Op = iota // match rune Arg (Fold = case-insensitive)
	OpClass             // match class Classes[Arg]
	OpAny               // match any rune; Arg != 0 means dot-all
	OpSplit             // prefer X, defer Y
	OpJmp               // jump to X
	OpSave              // record current position into capture slot Arg
	OpMatch             // accept
	OpBackref           // match text previously captured by group Arg
	OpAssert            // zero-width assertion of kind Arg (AnchorKind)
	OpLook              // lookaround: Arg = index into Looks; Ahead/Negative flags
	OpLoop              // zero-width loop guard: Arg = loop id, X = exit target
	OpLoopEnd           // clear loop guard Arg
)

// Inst is one compiled instruction.
type Inst struct {
	Op       Op   `json:"op"`
	Arg      int  `json:"arg,omitempty"`
	Fold     bool `json:"fold,omitempty"`
	Ahead    bool `json:"ahead,omitempty"`
	Negative bool `json:"negative,omitempty"`
	X        int  `json:"x,omitempty"`
	Y        int  `json:"y,omitempty"`
}

// Prog is a compiled program.
type Prog struct {
	Insts      []Inst         `json:"insts"`
	Classes    []*Class       `json:"classes,omitempty"`
	Looks      []*Prog        `json:"looks,omitempty"`
	NumGroups  int            `json:"numGroups"`
	GroupNames map[string]int `json:"groupNames,omitempty"`
	NumLoops   int            `json:"numLoops"`
	Start      int            `json:"start"`
}

// maxUnroll bounds counted-repeat expansion.
const maxUnroll = 1000

type compiler struct {
	prog *Prog
}

// Compile lowers an AST into a Prog. numGroups and names come from Parse.
func Compile(root *Node, numGroups int, names map[string]int) (*Prog, error) {
	c := &compiler{prog: &Prog{NumGroups: numGroups, GroupNames: names}}
	if err := c.emit(root); err != nil {
		return nil, err
	}
	c.emitInst(Inst{Op: OpMatch})
	c.prog.Start = 0
	return c.prog, nil
}

func (c *compiler) emitInst(in Inst) int {
	c.prog.Insts = append(c.prog.Insts, in)
	return len(c.prog.Insts) - 1
}

func (c *compiler) patchX(at, target int) { c.prog.Insts[at].X = target }
func (c *compiler) patchY(at, target int) { c.prog.Insts[at].Y = target }

func (c *compiler) emit(n *Node) error {
	switch n.Kind {
	case KEmpty:
		return nil
	case KLiteral:
		c.emitInst(Inst{Op: OpRune, Arg: int(n.Rune), Fold: n.Fold})
	case KClass:
		idx := len(c.prog.Classes)
		c.prog.Classes = append(c.prog.Classes, n.Class)
		c.emitInst(Inst{Op: OpClass, Arg: idx})
	case KAny:
		arg := 0
		if n.DotAll {
			arg = 1
		}
		c.emitInst(Inst{Op: OpAny, Arg: arg})
	case KConcat:
		for _, ch := range n.Children {
			if err := c.emit(ch); err != nil {
				return err
			}
		}
	case KAlt:
		// Chain of splits; left (earlier) branch is always preferred.
		var jmpEnds []int
		for i, ch := range n.Children {
			if i < len(n.Children)-1 {
				sp := c.emitInst(Inst{Op: OpSplit})
				c.patchX(sp, len(c.prog.Insts))
				if err := c.emit(ch); err != nil {
					return err
				}
				jmpEnds = append(jmpEnds, c.emitInst(Inst{Op: OpJmp}))
				c.patchY(sp, len(c.prog.Insts))
			} else {
				if err := c.emit(ch); err != nil {
					return err
				}
			}
		}
		end := len(c.prog.Insts)
		for _, j := range jmpEnds {
			c.patchX(j, end)
		}
	case KRepeat:
		return c.emitRepeat(n)
	case KGroup:
		if n.Index > 0 {
			c.emitInst(Inst{Op: OpSave, Arg: 2 * n.Index})
		}
		if err := c.emit(n.Children[0]); err != nil {
			return err
		}
		if n.Index > 0 {
			c.emitInst(Inst{Op: OpSave, Arg: 2*n.Index + 1})
		}
	case KBackref:
		c.emitInst(Inst{Op: OpBackref, Arg: n.Index})
	case KAnchor:
		c.emitInst(Inst{Op: OpAssert, Arg: int(n.Anchor)})
	case KLook:
		sub, err := Compile(n.Children[0], c.prog.NumGroups, nil)
		if err != nil {
			return err
		}
		idx := len(c.prog.Looks)
		c.prog.Looks = append(c.prog.Looks, sub)
		c.emitInst(Inst{Op: OpLook, Arg: idx, Ahead: n.Ahead, Negative: n.Negative})
	default:
		return fmt.Errorf("cannot compile node kind %v", n.Kind)
	}
	return nil
}

func (c *compiler) emitRepeat(n *Node) error {
	min, max := n.Min, n.Max
	if max >= 0 && max < min {
		return fmt.Errorf("invalid repeat bounds {%d,%d}", min, max)
	}
	if max > maxUnroll || min > maxUnroll {
		return fmt.Errorf("repeat count too large (max %d)", maxUnroll)
	}
	body := n.Children[0]
	// Required copies.
	for i := 0; i < min; i++ {
		if err := c.emit(body); err != nil {
			return err
		}
	}
	if max == -1 {
		// Trailing star loop.
		return c.emitStar(body, n.Greedy)
	}
	// Optional copies: (max-min) chained splits so backtracking order matches
	// the greedy/lazy preference.
	count := max - min
	splits := make([]int, 0, count)
	for i := 0; i < count; i++ {
		sp := c.emitInst(Inst{Op: OpSplit})
		splits = append(splits, sp)
		if n.Greedy {
			c.patchX(sp, len(c.prog.Insts))
		} else {
			// Lazy: prefer exit; X is patched to the end later, Y is the body.
			c.patchY(sp, len(c.prog.Insts))
		}
		if err := c.emit(body); err != nil {
			return err
		}
	}
	end := len(c.prog.Insts)
	for _, sp := range splits {
		if n.Greedy {
			c.patchY(sp, end)
		} else {
			c.patchX(sp, end)
		}
	}
	return nil
}

// emitStar compiles body* with a zero-width loop guard so empty-body
// iterations terminate.
func (c *compiler) emitStar(body *Node, greedy bool) error {
	loopID := c.prog.NumLoops
	c.prog.NumLoops++
	loopPC := c.emitInst(Inst{Op: OpLoop, Arg: loopID}) // X patched to exit
	sp := c.emitInst(Inst{Op: OpSplit})
	bodyPC := len(c.prog.Insts)
	if err := c.emit(body); err != nil {
		return err
	}
	c.emitInst(Inst{Op: OpJmp, X: loopPC})
	endPC := c.emitInst(Inst{Op: OpLoopEnd, Arg: loopID})
	c.patchX(loopPC, endPC)
	if greedy {
		c.patchX(sp, bodyPC)
		c.patchY(sp, endPC)
	} else {
		c.patchX(sp, endPC)
		c.patchY(sp, bodyPC)
	}
	return nil
}
