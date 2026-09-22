package regex

import "fmt"

// Op is a VM instruction opcode.
type Op int

const (
	OpRune    Op = iota // match one literal rune
	OpClass             // match one rune from a class
	OpAny               // match any rune except '\n'
	OpSplit             // ordered branch: try X, push Y as backtrack point
	OpJmp               // unconditional jump to X
	OpSave              // record current position into capture slot
	OpMatch             // accept
	OpAssert            // zero-width assertion (AnchorKind)
	OpBackRef           // match text captured by group N
	OpLoop              // zero-width-repeat guard (loop id)
	OpLook              // lookaround assertion with sub-program
)

// Inst is one compiled instruction.
type Inst struct {
	Op     Op
	R      rune       // OpRune
	Class  *Class     // OpClass
	X, Y   int        // OpSplit / OpJmp targets
	Slot   int        // OpSave
	N      int        // OpBackRef group index
	Kind   AnchorKind // OpAssert
	ID     int        // OpLoop loop id
	Sub    []Inst     // OpLook sub-program (ends with OpMatch)
	Neg    bool       // OpLook negated
	Behind bool       // OpLook lookbehind
}

// Program is a compiled pattern.
type Program struct {
	Insts      []Inst
	NumGroups  int
	GroupNames map[string]int
}

// compiler lowers AST nodes to instructions.
type compiler struct {
	insts  []Inst
	loopID int
}

// Compile lowers the AST into a Program. When optimize is true the AST
// is simplified first; the documented branch order is preserved.
func Compile(root Node, info *PatternInfo, optimize bool) *Program {
	if optimize {
		root = Optimize(root)
	}
	c := &compiler{}
	c.compile(root)
	c.emit(Inst{Op: OpMatch})
	return &Program{Insts: c.insts, NumGroups: info.GroupCount, GroupNames: info.Groups}
}

func (c *compiler) emit(i Inst) int {
	c.insts = append(c.insts, i)
	return len(c.insts) - 1
}

func (c *compiler) compile(n Node) {
	switch t := n.(type) {
	case *Literal:
		c.emit(Inst{Op: OpRune, R: t.R})
	case *Any:
		c.emit(Inst{Op: OpAny})
	case *Class:
		c.emit(Inst{Op: OpClass, Class: t})
	case *Concat:
		for _, sub := range t.Nodes {
			c.compile(sub)
		}
	case *Alt:
		c.compileAlt(t)
	case *Group:
		if !t.Capture {
			c.compile(t.Child)
			return
		}
		c.emit(Inst{Op: OpSave, Slot: 2 * t.Index})
		c.compile(t.Child)
		c.emit(Inst{Op: OpSave, Slot: 2*t.Index + 1})
	case *Repeat:
		c.compileRepeat(t)
	case *Anchor:
		c.emit(Inst{Op: OpAssert, Kind: t.Kind})
	case *Backref:
		c.emit(Inst{Op: OpBackRef, N: t.Index})
	case *Look:
		sub := &compiler{}
		sub.compile(t.Child)
		sub.emit(Inst{Op: OpMatch})
		c.emit(Inst{Op: OpLook, Sub: sub.insts, Neg: t.Negate, Behind: t.Behind})
	default:
		panic(fmt.Sprintf("compile: unhandled node %T", n))
	}
}

func (c *compiler) compileAlt(a *Alt) {
	var jmpEnds []int
	for i, b := range a.Branches {
		if i < len(a.Branches)-1 {
			s := c.emit(Inst{Op: OpSplit})
			c.insts[s].X = len(c.insts)
			c.compile(b)
			jmpEnds = append(jmpEnds, c.emit(Inst{Op: OpJmp}))
			c.insts[s].Y = len(c.insts)
		} else {
			c.compile(b)
		}
	}
	for _, j := range jmpEnds {
		c.insts[j].X = len(c.insts)
	}
}

func (c *compiler) compileRepeat(r *Repeat) {
	min, max := r.Min, r.Max
	if max == 0 {
		return // x{0} matches empty
	}
	// Unroll the mandatory prefix.
	for i := 0; i < min; i++ {
		c.compile(r.Child)
	}
	switch {
	case max < 0:
		// unbounded tail: star of the child
		c.compileStar(r.Child, r.Lazy)
	case max > min:
		// bounded optional tail: (child (child ...)?)?
		var splits []int
		for i := min; i < max; i++ {
			s := c.emit(Inst{Op: OpSplit})
			if r.Lazy {
				splits = append(splits, s) // X patched to "after"
				c.insts[s].Y = len(c.insts)
			} else {
				c.insts[s].X = len(c.insts)
				splits = append(splits, s) // Y patched to "after"
			}
			c.compile(r.Child)
		}
		for _, s := range splits {
			if r.Lazy {
				c.insts[s].X = len(c.insts)
			} else {
				c.insts[s].Y = len(c.insts)
			}
		}
	}
}

// compileStar emits  L1: Loop id ; Split(body, after) ; body ; Jmp L1 ; after:
// (with the split arms swapped for lazy repeats). The Loop instruction
// terminates zero-width repetitions.
func (c *compiler) compileStar(child Node, lazy bool) {
	c.loopID++
	id := c.loopID
	l1 := c.emit(Inst{Op: OpLoop, ID: id})
	s := c.emit(Inst{Op: OpSplit})
	body := len(c.insts)
	c.compile(child)
	c.emit(Inst{Op: OpJmp, X: l1})
	after := len(c.insts)
	if lazy {
		c.insts[s].X = after
		c.insts[s].Y = body
	} else {
		c.insts[s].X = body
		c.insts[s].Y = after
	}
}

// disassemble renders the program for debugging and the UI.
func (p *Program) disassemble() []string {
	out := make([]string, len(p.Insts))
	for i, in := range p.Insts {
		out[i] = fmt.Sprintf("%3d %s", i, instString(in))
	}
	return out
}

// ProgramText renders the program as one line per instruction.
func ProgramText(p *Program) []string { return p.disassemble() }

func instString(in Inst) string {
	switch in.Op {
	case OpRune:
		return fmt.Sprintf("rune %s", quoteRune(in.R))
	case OpClass:
		return "class " + in.Class.String()
	case OpAny:
		return "any"
	case OpSplit:
		return fmt.Sprintf("split %d, %d", in.X, in.Y)
	case OpJmp:
		return fmt.Sprintf("jmp %d", in.X)
	case OpSave:
		return fmt.Sprintf("save %d", in.Slot)
	case OpMatch:
		return "match"
	case OpAssert:
		return "assert " + (&Anchor{Kind: in.Kind}).String()
	case OpBackRef:
		return fmt.Sprintf("backref #%d", in.N)
	case OpLoop:
		return fmt.Sprintf("loop %d", in.ID)
	case OpLook:
		kind := "?="
		if in.Neg && in.Behind {
			kind = "?<!"
		} else if in.Neg {
			kind = "?!"
		} else if in.Behind {
			kind = "?<="
		}
		return "look " + kind
	}
	return "?"
}
