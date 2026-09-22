package engine

import "capture-tracer/internal/syntax"

// OpKind enumerates the virtual machine instructions.
type OpKind int

const (
	OpChar OpKind = iota // Arg= rune, Flags
	OpAny                // Flags (dot semantics)
	OpClass              // Ptr *syntax.CharClass
	OpSave               // Arg = slot to save thread position into (push cap)
	OpBackref            // Arg = group index, Flags
	OpSplit              // A=preferred target, B=alternative target, FlagSplitLazy
	OpJmp                // A target
	OpGuard              // Arg=guard id; fail if frame holds same guard id at same pos; then set
	OpGuardClear         // Arg=guard id; remove frame guard (on exit from quantified body)
	OpAnchor             // Arg syntax.Anchor, Flags
	OpAssert             // A=subprogram index; Arg=dir(0 ahead/1 behind), Arg2=neg, Arg3=width
	OpMatch              // terminal success
	OpMatchFull          // success only if pos==len(input)
	OpNodeEnter          // Arg=node id (trace marker, no consumption)
	OpNodeExit           // Arg=node id (trace marker)
	OpFailNode           // marker for node-exit-on-failure trace (emits Backtrack)
	OpSplitPoint         // Arg=node id marker emitted when an alternative is explored
)

// Op is one bytecode instruction.
type Op struct {
	Kind  OpKind
	Arg   int
	Arg2  int
	Arg3  int
	A, B  int
	Flags syntax.FlagSet
	Ptr   *syntax.CharClass
	Node  int
}

// Sub is one compiled subprogram (main or a lookaround).
type Sub struct {
	Code []Op
	Name string
}

// Program is the compiled, immutable match program.
type Program struct {
	Subs       []Sub
	NumGroups  int
	Names      map[string]int
	NumGuards  int
	Full       bool
	Optimized  int
	Pattern    string
	AST        *syntax.Pattern
}

type compiler struct {
	prog    *Program
	code    []Op
	flags   syntax.FlagSet
	guards  int
	queued  []queuedLook
}

type queuedLook struct {
	node  *syntax.Node
	index int // slot in Subs
}

// CompileFlags tunes compilation.
type CompileFlags struct {
	Full      bool // require full match (used by semantic diff)
	Optimized int  // 0 = naive, 1 = peephole/optimized
}

// Compile turns an AST into VM bytecode.
func Compile(p *syntax.Pattern, cf CompileFlags) *Program {
	main := make([]Op, 0, 64)
	c := &compiler{
		prog: &Program{
			Subs:      []Sub{{Code: main, Name: "main"}},
			NumGroups: p.NumGroups,
			Names:     p.Names,
			Full:      cf.Full,
			Optimized: cf.Optimized,
			AST:       p,
		},
		code: main,
	}
	c.compileNode(p.Root)
	if cf.Full {
		c.emit(Op{Kind: OpMatchFull})
	} else {
		c.emit(Op{Kind: OpMatch})
	}
	c.prog.Subs[0].Code = c.code

	// Compile lookaround subprograms in encounter (queue) order.
	for len(c.queued) > 0 {
		q := c.queued[0]
		c.queued = c.queued[1:]
		// Resolve if a placeholder was appended.
		sub := make([]Op, 0, 32)
		old := c.code
		c.code = sub
		c.compileNode(q.node)
		c.emit(Op{Kind: OpMatch})
		c.prog.Subs[q.index].Code = c.code
		c.prog.Subs[q.index].Name = lookName(q.node)
		c.code = old
	}
	c.prog.NumGuards = c.guards

	if cf.Optimized >= 1 {
		for i := range c.prog.Subs {
			c.prog.Subs[i].Code = peephole(c.prog.Subs[i].Code)
		}
	}
	return c.prog
}

func lookName(n *syntax.Node) string {
	s := "(?"
	if n.LookDir == syntax.LookAhead {
		if n.LookNeg {
			s += "!"
		} else {
			s += "="
		}
	} else {
		if n.LookNeg {
			s += "<!"
		} else {
			s += "<="
		}
	}
	return s + ")"
}

func (c *compiler) emit(o Op) int {
	idx := len(c.code)
	c.code = append(c.code, o)
	return idx
}

func here() int { return -2 }

func (c *compiler) compileNode(n *syntax.Node) {
	if n == nil {
		return
	}
	switch n.Kind {
	case syntax.KEmpty:
		// nothing
	case syntax.KChar:
		c.emit(Op{Kind: OpNodeEnter, Arg: n.ID})
		c.emit(Op{Kind: OpChar, Arg: int(n.Rune), Flags: c.flags, Node: n.ID})
		c.emit(Op{Kind: OpNodeExit, Arg: n.ID})
	case syntax.KAny:
		c.emit(Op{Kind: OpNodeEnter, Arg: n.ID})
		c.emit(Op{Kind: OpAny, Flags: c.flags, Node: n.ID})
		c.emit(Op{Kind: OpNodeExit, Arg: n.ID})
	case syntax.KClass:
		c.emit(Op{Kind: OpNodeEnter, Arg: n.ID})
		c.emit(Op{Kind: OpClass, Ptr: n.Class, Flags: c.flags, Node: n.ID})
		c.emit(Op{Kind: OpNodeExit, Arg: n.ID})
	case syntax.KAnchor:
		c.emit(Op{Kind: OpNodeEnter, Arg: n.ID})
		c.emit(Op{Kind: OpAnchor, Arg: int(n.Anchor), Flags: c.flags, Node: n.ID})
		c.emit(Op{Kind: OpNodeExit, Arg: n.ID})
	case syntax.KBackref:
		c.emit(Op{Kind: OpNodeEnter, Arg: n.ID})
		c.emit(Op{Kind: OpBackref, Arg: n.BackIndex, Flags: c.flags, Node: n.ID})
		c.emit(Op{Kind: OpNodeExit, Arg: n.ID})
	case syntax.KCat:
		c.emit(Op{Kind: OpNodeEnter, Arg: n.ID})
		for _, ch := range n.Children {
			c.compileNode(ch)
		}
		c.emit(Op{Kind: OpNodeExit, Arg: n.ID})
	case syntax.KAlt:
		c.compileAlt(n)
	case syntax.KGroup:
		c.compileGroup(n)
	case syntax.KFlags:
		c.compileFlags(n)
	case syntax.KRepeat:
		c.compileRepeat(n)
	case syntax.KLook:
		c.compileLook(n)
	}
}

func (c *compiler) compileAlt(n *syntax.Node) {
	c.emit(Op{Kind: OpNodeEnter, Arg: n.ID})
	endJumps := []int{}
	for i, br := range n.Children {
		if i > 0 {
			c.emit(Op{Kind: OpSplitPoint, Arg: n.ID})
		}
		c.compileNode(br)
		if i < len(n.Children)-1 {
			endJumps = append(endJumps, c.emit(Op{Kind: OpJmp, A: -1}))
		}
	}
	end := len(c.code)
	for _, j := range endJumps {
		c.code[j].A = end
	}
	c.emit(Op{Kind: OpNodeExit, Arg: n.ID})
}

func (c *compiler) compileGroup(n *syntax.Node) {
	c.emit(Op{Kind: OpNodeEnter, Arg: n.ID})
	if n.GroupIndex > 0 {
		c.emit(Op{Kind: OpSave, Arg: n.GroupIndex * 2, Node: n.ID})
	}
	if len(n.Children) > 0 {
		c.compileNode(n.Children[0])
	}
	if n.GroupIndex > 0 {
		c.emit(Op{Kind: OpSave, Arg: n.GroupIndex*2 + 1, Node: n.ID})
	}
	c.emit(Op{Kind: OpNodeExit, Arg: n.ID})
}

func (c *compiler) compileFlags(n *syntax.Node) {
	saved := c.flags
	c.flags = (c.flags &^ n.ClearFlags) | n.SetFlags
	if len(n.Children) > 0 {
		c.compileNode(n.Children[0])
	}
	c.flags = saved
}

func (c *compiler) compileLook(n *syntax.Node) {
	c.emit(Op{Kind: OpNodeEnter, Arg: n.ID})
	idx := len(c.prog.Subs)
	c.prog.Subs = append(c.prog.Subs, Sub{})
	c.queued = append(c.queued, queuedLook{node: n.Children[0], index: idx})
	dir := 0
	if n.LookDir == syntax.LookBehind {
		dir = 1
	}
	neg := 0
	if n.LookNeg {
		neg = 1
	}
	c.emit(Op{
		Kind: OpAssert, A: idx, Arg: dir, Arg2: neg, Arg3: n.LookWidth,
		Node: n.ID,
	})
	c.emit(Op{Kind: OpNodeExit, Arg: n.ID})
}

func (c *compiler) compileRepeat(n *syntax.Node) {
	c.emit(Op{Kind: OpNodeEnter, Arg: n.ID})
	child := n.Children[0]
	min, max := n.Min, n.Max

	switch {
	case max >= 0 && min == max:
		// e{n}: exact copies.
		for i := 0; i < min; i++ {
			c.compileNode(child)
		}
	case max >= 0:
		// e{n,m}: n mandatory copies, then (max-n) optional copies.
		for i := 0; i < min; i++ {
			c.compileNode(child)
		}
		for i := min; i < max; i++ {
			splitAt := c.emit(Op{Kind: OpSplit, A: -1, B: -1, Node: n.ID})
			c.compileNode(child)
			c.code[splitAt].A = splitAt + 1
			c.code[splitAt].B = len(c.code)
			if n.Lazy {
				c.code[splitAt].A, c.code[splitAt].B = c.code[splitAt].B, c.code[splitAt].A
			}
		}
	default:
		// *, +, {n,}: guarded loop; the guard prevents zero-width re-entry.
		guardID := c.guards
		c.guards++
		for i := 0; i < min; i++ {
			c.compileNode(child)
		}
		loop := len(c.code)
		splitAt := c.emit(Op{Kind: OpSplit, A: -1, B: -1, Node: n.ID})
		c.emit(Op{Kind: OpGuard, Arg: guardID, Node: n.ID})
		bodyStart := len(c.code)
		c.compileNode(child)
		c.emit(Op{Kind: OpGuardClear, Arg: guardID})
		c.emit(Op{Kind: OpJmp, A: loop})
		end := len(c.code)
		c.code[splitAt].A = bodyStart
		c.code[splitAt].B = end
		if n.Lazy {
			c.code[splitAt].A, c.code[splitAt].B = end, bodyStart
		}
}
	c.emit(Op{Kind: OpNodeExit, Arg: n.ID})
}

// peephole applies order-preserving optimizations: jump chains are resolved
// and pairs of markers that enclose nothing are left intact for naive builds.
// The optimized build resolves Jmp chains and folds Jmp-to-Jmp at split edges.
func peephole(code []Op) []Op {
	// Resolve jump targets transitively to canonical destinations.
	target := make([]int, len(code))
	for i := range target {
		target[i] = i
	}
	var resolve func(int) int
	resolve = func(i int) int {
		if target[i] != i {
			target[i] = resolve(target[i])
		}
		return target[i]
	}
	changed := true
	for changed {
		changed = false
		for i := range code {
			if code[i].Kind == OpJmp {
				t := code[i].A
				if code[t].Kind == OpJmp && target[i] != code[t].A {
					target[i] = code[t].A
					changed = true
				}
			}
		}
	}
	out := make([]Op, len(code))
	copy(out, code)
	for i := range out {
		switch out[i].Kind {
		case OpJmp:
			out[i].A = resolve(out[i].A)
		case OpSplit:
			out[i].A = resolve(out[i].A)
			out[i].B = resolve(out[i].B)
		}
	}
	return out
}
