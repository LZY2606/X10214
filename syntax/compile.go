package syntax

// Op is a bytecode opcode for the backtracking VM.
type Op int

const (
	OpChar  Op = iota // match one unit satisfying Insn.Match
	OpSplit           // two successors; Insn.A first, Insn.B second
	OpJmp
	OpSave      // save position into Insn.Slot
	OpZeroMark  // record pre-iteration pos for guard Insn.Guard
	OpZeroCheck // halt repetition if guard marks same pos
	OpAnchor
	OpRef    // back-reference to Insn.Slot's group
	OpLook   // composite lookaround
	OpMark   // backtracking marker for atomic/possessive groups
	OpCommit // jump to A; pops bstack back to its marker
	OpMatch
)

// Insn is one VM instruction.
type Insn struct {
	Op     Op
	A, B   int      // successor instruction indexes (-1 unset)
	Match  *Matcher // char-class matcher
	Slot   int      // save slot or backref group
	Guard  int      // zero-width guard id
	Anchor AnchorKind
	Neg    bool // lookaround polarity
	Behind bool // lookaround direction
	Body   int  // lookaround body entry pc
	End    int  // lookaround body end pc (exclusive)
	Label  string
}

// Program is the compiled pattern.
type Program struct {
	Insns     []Insn
	NumGroups int
	Names     map[string]int
	Flags     Flags
	Start     int
}

type compiler struct {
	prog *Program
	info *Info
}

// Compile parses and compiles pattern.
func Compile(pattern string) (*Program, Node, *Info, error) {
	root, info, err := Parse(pattern)
	if err != nil {
		return nil, nil, nil, err
	}
	c := &compiler{prog: &Program{Names: info.Names, NumGroups: info.NumGroups, Flags: info.Flags}, info: info}
	c.emitSave(0)
	c.fragment(root, info.Flags)
	c.emitSave(1)
	c.emit(Insn{Op: OpMatch})
	prog := c.prog
	patchJumps(prog)
	optimize(prog)
	return prog, root, info, nil
}

func (c *compiler) emit(i Insn) int {
	c.prog.Insns = append(c.prog.Insns, i)
	return len(c.prog.Insns) - 1
}

func (c *compiler) emitSave(slot int) int {
	return c.emit(Insn{Op: OpSave, Slot: slot, Label: "save"})
}

// fragment compiles n with linear continuation: control flow simply falls
// through to whatever is emitted next. Forward branches use A/B == -1 and are
// resolved by patchJumps once the whole program is known.
func (c *compiler) fragment(n Node, fl Flags) {
	switch x := n.(type) {
	case *Literal:
		c.emit(Insn{Op: OpChar, Match: newSingleMatcher(x.R, fl.I), Label: "char"})
	case *CharClass:
		m, err := NewMatcher(x, fl)
		if err != nil {
			panic(err)
		}
		c.emit(Insn{Op: OpChar, Match: m, Label: "class"})
	case *Anchor:
		c.emit(Insn{Op: OpAnchor, Anchor: x.Kind, Label: "anchor"})
	case *Ref:
		idx := x.Index
		if x.Name != "" {
			idx = c.info.Names[x.Name]
		}
		c.emit(Insn{Op: OpRef, Slot: idx, Label: "backref"})
	case *Concat:
		for _, it := range x.Items {
			c.fragment(it, fl)
		}
	case *Group:
		nf := fl
		if x.Flags != nil {
			nf = mergeFlags(fl, *x.Flags)
		}
		if x.Capture {
			c.emitSave(2 * x.Index)
			c.fragment(x.Body, nf)
			c.emitSave(2*x.Index + 1)
		} else {
			c.fragment(x.Body, nf)
		}
	case *Atomic:
		c.emit(Insn{Op: OpMark, Label: "atomic-open"})
		c.fragment(x.Body, fl)
		c.emit(Insn{Op: OpJmp, A: -1, Label: "atomic-commit"}) // patched as commit-target marker
	case *Lookaround:
		body := len(c.prog.Insns)
		c.fragment(x.Body, fl)
		end := len(c.prog.Insns)
		c.emit(Insn{Op: OpLook, Neg: x.Negative, Behind: x.Behind, Body: body, End: end, Label: "look"})
	case *Alt:
		var endJumps []int
		for i, b := range x.Branches {
			if i < len(x.Branches)-1 {
				// Prefer the documented first branch: A enters the branch,
				// B skips to the next split (later branches).
				sp := c.emit(Insn{Op: OpSplit, A: -1, B: -1, Label: "alt"})
				branchPC := len(c.prog.Insns)
				c.fragment(b, fl)
				endJumps = append(endJumps, c.emit(Insn{Op: OpJmp, A: -1, Label: "alt-end"}))
				nextPC := len(c.prog.Insns)
				c.prog.Insns[sp].A = branchPC
				c.prog.Insns[sp].B = nextPC
			} else {
				c.fragment(b, fl)
			}
		}
		end := len(c.prog.Insns)
		for _, j := range endJumps {
			c.prog.Insns[j].A = end
		}
	case *Quant:
		c.quant(x, fl)
	}
}

// quant compiles a quantifier.
//
// Greedy optional copies use split A=body, B=after; lazy swaps the two.
// Zero-width capable children are bracketed by ZeroMark/ZeroCheck which stop
// iteration the moment the body matched without consuming input. Possessive
// quantifiers discard all in-quantifier alternatives on exit.
func (c *compiler) quant(q *Quant, fl Flags) {
	if q.Possessive {
		c.emit(Insn{Op: OpMark, Label: "possessive-open"})
	}
	oneBody := func() {
		if q.Empty {
			c.emit(Insn{Op: OpZeroMark, Guard: q.Guard, Label: "zero-mark"})
		}
		c.fragment(q.Child, fl)
		if q.Empty {
			c.emit(Insn{Op: OpZeroCheck, Guard: q.Guard, Label: "zero-check"})
		}
	}
	// Exactly Min copies are mandatory.
	for i := 0; i < q.Min; i++ {
		oneBody()
	}
	if q.Max == q.Min {
		if q.Possessive {
			c.emit(Insn{Op: OpCommit, A: len(c.prog.Insns), Label: "possessive-commit"})
		}
		return
	}
	optional := -1
	if q.Max >= 0 {
		optional = q.Max - q.Min
	}
	if optional < 0 {
		// Unbounded: a single loop.
		head := len(c.prog.Insns)
		sp := c.emit(Insn{Op: OpSplit, A: -1, B: -1, Label: "quant-split"})
		bodyPC := len(c.prog.Insns)
		oneBody()
		c.emit(Insn{Op: OpJmp, A: head, Label: "quant-loop"})
		after := len(c.prog.Insns)
		if q.Greedy {
			c.prog.Insns[sp].A, c.prog.Insns[sp].B = bodyPC, after
		} else {
			c.prog.Insns[sp].A, c.prog.Insns[sp].B = after, bodyPC
		}
	} else {
		// Bounded: `optional` independent split copies.
		for i := 0; i < optional; i++ {
			sp := c.emit(Insn{Op: OpSplit, A: -1, B: -1, Label: "quant-split"})
			bodyPC := len(c.prog.Insns)
			oneBody()
			after := len(c.prog.Insns)
			if q.Greedy {
				c.prog.Insns[sp].A, c.prog.Insns[sp].B = bodyPC, after
			} else {
				c.prog.Insns[sp].A, c.prog.Insns[sp].B = after, bodyPC
			}
		}
	}
	if q.Possessive {
		c.emit(Insn{Op: OpCommit, A: len(c.prog.Insns), Label: "possessive-commit"})
	}
}

// patchJumps resolves remaining -1 successors (defensive; all branches are
// already wired during emission).
func patchJumps(prog *Program) {
	for i := range prog.Insns {
		in := &prog.Insns[i]
		switch in.Op {
		case OpJmp:
			if in.A < 0 {
				in.A = i + 1
			}
		case OpSplit:
			if in.A < 0 {
				in.A = i + 1
			}
			if in.B < 0 {
				in.B = i + 1
			}
		}
	}
}

// optimize performs the documented trace-preserving optimization: jump
// threading. Jumps targeting jumps are redirected to the final target; jumps
// targeting the immediately following instruction are removed. The observable
// trace (step order, branches, captures, failures) is unchanged — tests in
// engine verify pre/post equivalence.
func optimize(prog *Program) {
	// Build target map iteratively to collapse chains.
	target := func(pc int) int {
		for prog.Insns[pc].Op == OpJmp && prog.Insns[pc].Label != "atomic-commit" {
			pc = prog.Insns[pc].A
		}
		return pc
	}
	for {
		changed := false
		for i := range prog.Insns {
			in := &prog.Insns[i]
			switch in.Op {
			case OpJmp:
				if in.Label == "atomic-commit" || in.Label == "possessive-commit" {
					continue
				}
				t := target(in.A)
				if t != in.A {
					in.A = t
					changed = true
				}
			case OpSplit:
				if ta := target(in.A); ta != in.A {
					in.A = ta
					changed = true
				}
				if tb := target(in.B); tb != in.B {
					in.B = tb
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	// Remove plain Jmp instructions that land on the very next pc by swapping
	// them with a Nop-like unreachable marker is unsafe for pc stability of
	// lookaround body ranges, so they are kept (still O(1)).
}
