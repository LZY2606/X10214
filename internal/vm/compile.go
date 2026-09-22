package vm

import (
	"trajectoryroom/internal/syntax"
)

type compiler struct {
	prog    *Program
	loopSeq int
	lookSeq int
	maxLook []int // indexed by lookID; lookID 0 unused
}

// Compile turns a parsed pattern into executable bytecode.
func Compile(p *syntax.Pattern) *Program {
	c := &compiler{prog: &Program{GroupCount: len(p.Groups)}}
	if len(p.Groups) > 0 {
		c.prog.GroupNames = map[int]string{}
		for _, g := range p.Groups {
			if g.Name != "" {
				c.prog.GroupNames[g.Index] = g.Name
			}
		}
	}
	flags := p.Flags
	c.compileNode(p.Root, flags)
	c.emit(Instr{Op: OpMatch, NID: p.Root.ID})
	// Patch look length table length.
	c.prog.MaxLookLen = c.maxLook
	return c.prog
}

func (c *compiler) emit(i Instr) int {
	i.Pc = len(c.prog.Code)
	c.prog.Code = append(c.prog.Code, i)
	return i.Pc
}

func (c *compiler) here() int { return len(c.prog.Code) }

func (c *compiler) compileNode(n *syntax.Node, flags syntax.FlagSet) {
	if n == nil {
		return
	}
	switch n.Kind {
	case syntax.NEmpty:
		// nothing
	case syntax.NChar:
		c.emit(Instr{Op: OpChar, NID: n.ID, Rune: n.Rune, Fold: flags.I || n.Fold})
	case syntax.NAny:
		c.emit(Instr{Op: OpAny, NID: n.ID, DotAll: flags.S})
	case syntax.NAnchor:
		a := AnchorBOL
		switch n.Anchor {
		case "eol":
			a = AnchorEOL
		case "wb":
			a = AnchorWB
		case "nwb":
			a = AnchorNWB
		}
		c.emit(Instr{Op: OpAnchor, NID: n.ID, A: a, Multiline: flags.M})
	case syntax.NCharClass:
		cp := &ClassPred{Negate: n.Negate, Fold: flags.I}
		for _, rg := range n.Ranges {
			cp.Ranges = append(cp.Ranges, RuneRange{Lo: rg.Lo, Hi: rg.Hi})
		}
		cp.Classes = append(cp.Classes, n.Classes...)
		c.emit(Instr{Op: OpClass, NID: n.ID, Class: cp})
	case syntax.NConcat:
		for _, ch := range n.Children {
			c.compileNode(ch, flags)
		}
	case syntax.NAlt:
		var jmps []int
		for i, br := range n.Children {
			if i > 0 {
				// Emit the jump, but record its slice index so the target can
				// be patched once the branch layout is known.
				jpc := c.emit(Instr{Op: OpJmp, NID: n.ID})
				jmps = append(jmps, jpc)
			}
			c.compileNode(br, flags)
		}
		end := c.here()
		for _, j := range jmps {
			c.prog.Code[j].Target = end
		}
	case syntax.NGroup:
		if n.Capture {
			c.emit(Instr{Op: OpGroupBegin, NID: n.ID, A: n.Index})
			c.compileNode(n.Children[0], flags)
			c.emit(Instr{Op: OpGroupEnd, NID: n.ID, A: n.Index})
		} else {
			c.compileNode(n.Children[0], flags)
		}
	case syntax.NFlags:
		flags = syntax.FlagSet{
			I: applyFlag(flags.I, n.SetFlags, n.ClearFlags, 'i'),
			M: applyFlag(flags.M, n.SetFlags, n.ClearFlags, 'm'),
			S: applyFlag(flags.S, n.SetFlags, n.ClearFlags, 's'),
			U: applyFlag(flags.U, n.SetFlags, n.ClearFlags, 'u'),
		}
	case syntax.NBackref:
		c.emit(Instr{Op: OpBackref, NID: n.ID, A: n.RefIndex})
	case syntax.NRepeat:
		c.compileRepeat(n, flags)
	case syntax.NLook:
		c.compileLook(n, flags)
	}
}

func applyFlag(cur bool, set, clear string, ch byte) bool {
	for i := 0; i < len(set); i++ {
		if set[i] == ch {
			return true
		}
	}
	for i := 0; i < len(clear); i++ {
		if clear[i] == ch {
			return false
		}
	}
	return cur
}

func (c *compiler) compileRepeat(n *syntax.Node, flags syntax.FlagSet) {
	loopID := c.loopSeq
	c.loopSeq++
	// Begin carries bounds.
	begin := c.emit(Instr{Op: OpRepeatBegin, NID: n.ID, A: loopID, B: n.Min, C: n.Max, Greedy: n.Greedy})
	// Uniform layout for greedy and lazy; only the split preference differs:
	//   greedy prefers the fall-through body (splitPc+1), lazy prefers after.
	splitPc := c.emit(Instr{Op: OpSplit, NID: n.ID, A: loopID, Greedy: n.Greedy})
	c.compileNode(n.Child, flags)
	post := c.emit(Instr{Op: OpRepeatPost, NID: n.ID, A: loopID})
	jmp := c.emit(Instr{Op: OpJmp, NID: n.ID})
	after := c.here()
	c.emit(Instr{Op: OpRepeatAfter, NID: n.ID, A: loopID})
	c.prog.Code[splitPc].Target = after
	c.prog.Code[jmp].Target = splitPc
	_ = begin
	_ = post
}

func (c *compiler) compileLook(n *syntax.Node, flags syntax.FlagSet) {
	lookID := c.lookSeq
	c.lookSeq++
	for len(c.maxLook) <= lookID {
		c.maxLook = append(c.maxLook, 0)
	}
	maxLen := maxMatchLen(n.Children[0])
	c.maxLook[lookID] = maxLen
	push := c.emit(Instr{Op: OpLookPush, NID: n.ID, A: lookID, B: maxLen, Neg: n.Negative, Behind: n.Behind})
	c.compileNode(n.Children[0], flags)
	endPc := c.emit(Instr{Op: OpLookEnd, NID: n.ID, A: lookID})
	c.prog.Code[push].C = endPc + 1
	_ = push
}
