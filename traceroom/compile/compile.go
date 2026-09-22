package compile

import (
	"traceroom/syntax"
)

type compiler struct {
	code      []Instr
	subs      map[int]*Program
	classes   []*syntax.Class
	numCaps   int
	names     map[string]int
	capName   map[int]string
	src       string
	flags     uint8
	nextLoop  int
	nextSub   int
	optimized bool
}

// Compile lowers a parsed pattern into a Program.
func Compile(p *syntax.Pattern, optimize bool) (*Program, error) {
	c := &compiler{
		subs:      map[int]*Program{},
		classes:   nil,
		numCaps:   p.NumCaps,
		names:     p.Names,
		capName:   p.CapNames,
		src:       p.Source,
		flags:     p.Flags,
		optimized: optimize,
	}
	c.emitNode(p.Root, p.Flags)
	c.emit(Instr{Op: OpMatch})
	prog := &Program{
		Code:      c.code,
		Subs:      c.subs,
		Classes:   c.classes,
		NumCaps:   p.NumCaps,
		Names:     c.names,
		CapNames:  c.capName,
		Pattern:   c.src,
		Flags:     p.Flags,
		Optimized: optimize,
	}
	if optimize {
		optimizeProgram(prog)
	}
	return prog, nil
}

func (c *compiler) emit(i Instr) int {
	pc := len(c.code)
	c.code = append(c.code, i)
	return pc
}

func (c *compiler) emitNode(n syntax.Node, flags uint8) {
	switch t := n.(type) {
	case *syntax.Seq:
		for _, k := range t.Kids {
			c.emitNode(k, flags)
		}
	case *syntax.Alt:
		c.compileAlt(t, flags)
	case *syntax.Char:
		c.emit(Instr{Op: OpChar, Arg: int(t.R), Lo: t.SrcLo, Hi: t.SrcHi})
	case *syntax.Dot:
		c.emit(Instr{Op: OpAny, Lo: t.SrcLo, Hi: t.SrcHi})
	case *syntax.Class:
		idx := len(c.classes)
		c.classes = append(c.classes, t)
		c.emit(Instr{Op: OpClass, Arg: idx, Lo: t.SrcLo, Hi: t.SrcHi})
	case *syntax.Anchor:
		c.emit(Instr{Op: OpAnchor, Arg: int(t.Kind), Lo: t.SrcLo, Hi: t.SrcHi})
	case *syntax.Backref:
		c.emit(Instr{Op: OpBackref, Arg: t.ID, Lo: t.SrcLo, Hi: t.SrcHi})
	case *syntax.Group:
		if t.CapID > 0 {
			c.emit(Instr{Op: OpSave, Arg: 2 * t.CapID, Lo: t.SrcLo, Hi: t.SrcLo})
			c.emitNode(t.Kid, flags)
			c.emit(Instr{Op: OpSave, Arg: 2*t.CapID + 1, Lo: t.SrcHi, Hi: t.SrcHi})
		} else if t.Kid != nil {
			c.emitNode(t.Kid, flags)
		}
	case *syntax.Flags:
		nf := (flags | t.Set) &^ t.Clear
		if t.Kid != nil {
			c.emitNode(t.Kid, nf)
		} else {
			c.flags = nf
		}
	case *syntax.Atomic:
		cut := c.nextLoop
		c.nextLoop++
		c.emit(Instr{Op: OpOpenCut, Arg: cut, Lo: t.SrcLo, Hi: t.SrcLo})
		c.emitNode(t.Kid, flags)
		c.emit(Instr{Op: OpCloseCut, Arg: cut, Lo: t.SrcHi, Hi: t.SrcHi})
	case *syntax.Look:
		c.compileLook(t, flags)
	case *syntax.Repeat:
		c.compileRepeat(t, flags)
	}
}

func (c *compiler) compileAlt(t *syntax.Alt, flags uint8) {
	var endJumps []int
	for i, br := range t.Branches {
		if i < len(t.Branches)-1 {
			// split: primary = this branch (fall through), secondary = next
			split := c.emit(Instr{Op: OpSplit, Lo: t.SrcLo, Hi: t.SrcHi})
			for _, k := range br {
				c.emitNode(k, flags)
			}
			endJumps = append(endJumps, c.emit(Instr{Op: OpJmp, Lo: t.SrcLo, Hi: t.SrcHi}))
			c.code[split].Arg = len(c.code) // secondary target
		} else {
			for _, k := range br {
				c.emitNode(k, flags)
			}
		}
	}
	end := len(c.code)
	for _, j := range endJumps {
		c.code[j].Arg = end
	}
}
