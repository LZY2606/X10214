package vm

import (
	"fmt"

	"trajex/internal/syntax"
)

type compiler struct {
	prog     *Program
	flags    byte
	guardSeq int
}

// Compile turns a parsed pattern into a bytecode Program.
func Compile(p *syntax.Pattern, optimize bool) (out *Program, err error) {
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(*syntax.ParseError); ok {
				out, err = nil, pe
				return
			}
			panic(r)
		}
	}()
	c := &compiler{prog: &Program{NumGroups: p.NumGroups, Named: p.NamedGroups, Pattern: p.Source}}
	c.compileNode(p.Root)
	c.emit(Instr{Op: OpMatch, Source: p.Root})
	c.resolveRefs(p)
	if optimize {
		n := c.optimize()
		c.prog.Optimized = true
		c.prog.OptimNote = n
	}
	return c.prog, nil
}

func (c *compiler) emit(in Instr) int {
	idx := len(c.prog.Code)
	c.prog.Code = append(c.prog.Code, in)
	return idx
}

func (c *compiler) compileNode(n syntax.Node) {
	switch v := n.(type) {
	case *syntax.Empty:
		// nothing
	case *syntax.Lit:
		c.emit(Instr{Op: OpChar, Arg: int(v.R), Flags: c.flags, Source: n})
	case *syntax.Class:
		c.emit(Instr{Op: OpClass, Class: v, Flags: c.flags, Source: n})
	case *syntax.Anchor:
		c.compileAnchor(v, n)
	case *syntax.Ref:
		idx := v.Index
		if v.Named {
			var ok bool
			idx, ok = c.prog.Named[v.Name]
			if !ok {
				panic(&syntax.ParseError{Offset: v.At, Msg: "unknown named backreference: " + v.Name})
			}
		}
		c.emit(Instr{Op: OpRef, Arg: idx, Flags: c.flags, Source: n})
	case *syntax.Group:
		c.compileGroup(v, n)
	case *syntax.Repeat:
		c.compileRepeat(v)
	case *syntax.Look:
		c.compileLook(v, n)
	case *syntax.Concat:
		for _, it := range v.Items {
			c.compileNode(it)
		}
	case *syntax.Alt:
		c.compileAlt(v, n)
	default:
		panic(fmt.Sprintf("vm: unknown node %T", n))
	}
}

func (c *compiler) compileAnchor(a *syntax.Anchor, src syntax.Node) {
	switch a.Kind {
	case '^':
		c.emit(Instr{Op: OpBeginLine, Flags: c.flags, Source: src})
	case '$':
		c.emit(Instr{Op: OpEndLine, Flags: c.flags, Source: src})
	case 'A':
		c.emit(Instr{Op: OpBeginText, Source: src})
	case 'z':
		c.emit(Instr{Op: OpEndText, Arg: 0, Source: src})
	case 'Z':
		c.emit(Instr{Op: OpEndText, Arg: 1, Source: src})
	case 'b', 'B':
		c.emit(Instr{Op: OpWord, Arg: int(a.Kind), Source: src})
	}
}

func (c *compiler) compileAlt(a *syntax.Alt, src syntax.Node) {
	jumps := []int{}
	for i, b := range a.Branches {
		if i < len(a.Branches)-1 {
			split := c.emit(Instr{Op: OpSplit, Arg: 0, Targets: []int{-1, -1}, Source: src})
			bodyStart := len(c.prog.Code)
			c.prog.Code[split].Targets[0] = bodyStart
			c.compileNode(b)
			j := c.emit(Instr{Op: OpJmp, Targets: []int{-1}, Source: src})
			jumps = append(jumps, j)
			next := len(c.prog.Code)
			c.prog.Code[split].Targets[1] = next
		} else {
			c.compileNode(b)
		}
	}
	end := len(c.prog.Code)
	for _, j := range jumps {
		c.prog.Code[j].Targets[0] = end
	}
}

func (c *compiler) compileGroup(g *syntax.Group, src syntax.Node) {
	old := c.flags
	c.flags = applyFlags(c.flags, g.Flags)
	if g.Capture {
		s := g.Index * 2
		c.emit(Instr{Op: OpSave, Arg: s, Source: src})
		c.compileNode(g.Body)
		c.emit(Instr{Op: OpSave, Arg: s + 1, Source: src})
	} else {
		c.compileNode(g.Body)
	}
	c.flags = old
}

func applyFlags(cur byte, spec string) byte {
	neg := false
	for _, ch := range spec {
		switch ch {
		case '-':
			neg = true
		case 'i':
			if neg {
				cur &^= FlagFold
			} else {
				cur |= FlagFold
			}
		case 'm':
			if neg {
				cur &^= FlagMultiline
			} else {
				cur |= FlagMultiline
			}
		case 's':
			if neg {
				cur &^= FlagDotall
			} else {
				cur |= FlagDotall
			}
		}
	}
	return cur
}

func (c *compiler) guardID() int {
	c.guardSeq++
	c.prog.NumGuards = c.guardSeq
	return c.guardSeq - 1
}

func (c *compiler) compileRepeat(r *syntax.Repeat) {
	max := r.Max
	unbounded := max < 0
	// Fixed repetition: unroll.
	if !unbounded && r.Min == max {
		for i := 0; i < r.Min; i++ {
			c.compileNode(r.Body)
		}
		return
	}
	gid := c.guardID()
	for i := 0; i < r.Min; i++ {
		c.compileNode(r.Body)
	}
	if unbounded {
		// body* loop
		split := c.emit(Instr{Op: OpSplit, Arg: b2i(r.Lazy), Targets: []int{-1, -1}, GuardID: gid, Source: r.Body})
		bodyStart := len(c.prog.Code)
		c.compileNode(r.Body)
		guard := c.emit(Instr{Op: OpGuard, Arg: gid, Targets: []int{-1}, Source: r.Body})
		back := c.emit(Instr{Op: OpJmp, Targets: []int{split}, Source: r.Body})
		exit := len(c.prog.Code)
		if r.Lazy {
			c.prog.Code[split].Targets[0] = exit
			c.prog.Code[split].Targets[1] = bodyStart
		} else {
			c.prog.Code[split].Targets[0] = bodyStart
			c.prog.Code[split].Targets[1] = exit
		}
		c.prog.Code[guard].Targets[0] = exit
		_ = back
		return
	}
	// bounded {min,max}: (max-min) optional copies, each guarded.
	extra := max - r.Min
	for i := 0; i < extra; i++ {
		split := c.emit(Instr{Op: OpSplit, Arg: b2i(r.Lazy), Targets: []int{-1, -1}, GuardID: gid, Source: r.Body})
		bodyStart := len(c.prog.Code)
		c.compileNode(r.Body)
		guard := c.emit(Instr{Op: OpGuard, Arg: gid, Targets: []int{-1}, Source: r.Body})
		exit := len(c.prog.Code)
		if r.Lazy {
			c.prog.Code[split].Targets[0] = exit
			c.prog.Code[split].Targets[1] = bodyStart
		} else {
			c.prog.Code[split].Targets[0] = bodyStart
			c.prog.Code[split].Targets[1] = exit
		}
		c.prog.Code[guard].Targets[0] = exit
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (c *compiler) compileLook(l *syntax.Look, src syntax.Node) {
	begin := c.emit(Instr{Op: OpLBegin, Arg: b2i(l.Behind), Arg2: b2i(l.Negative), Targets: []int{-1}, Source: src})
	bodyStart := len(c.prog.Code)
	c.prog.Code[begin].Targets[0] = bodyStart
	c.compileNode(l.Body)
	c.emit(Instr{Op: OpLEnd, Arg: b2i(l.Behind), Arg2: b2i(l.Negative), Source: src})
}

func (c *compiler) resolveRefs(p *syntax.Pattern) {
	// Named references are resolved during compilation; numeric references to
	// unknown groups are a compile error so they never produce false no-match.
	for i := range c.prog.Code {
		if c.prog.Code[i].Op == OpRef {
			g := c.prog.Code[i].Arg
			if g <= 0 || g > p.NumGroups {
				panic(&syntax.ParseError{Offset: 0, Msg: fmt.Sprintf("backreference to missing group %d", g)})
			}
		}
	}
}
