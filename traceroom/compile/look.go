package compile

import "traceroom/syntax"

// compileLook emits an OpLook whose subprogram is compiled independently.
// Captures inside the subprogram use the same slot numbering so successful
// positive lookaheads can commit their capture updates; the VM snapshots and
// restores slots as required by polarity.
func (c *compiler) compileLook(t *syntax.Look, flags uint8) {
	sub := &compiler{
		subs:      map[int]*Program{},
		numCaps:   c.numCaps,
		names:     c.names,
		capName:   c.capName,
		src:       c.src,
		flags:     flags,
		nextLoop:  c.nextLoop,
		optimized: c.optimized,
	}
	sub.emitNode(t.Kid, flags)
	sub.emit(Instr{Op: OpSubEnd, Lo: t.SrcHi, Hi: t.SrcHi})
	prog := &Program{
		Code:      sub.code,
		Subs:      sub.subs,
		Classes:   sub.classes,
		NumCaps:   c.numCaps,
		Names:     c.names,
		CapNames:  c.capName,
		Pattern:   c.src,
		Flags:     flags,
		Optimized: c.optimized,
	}
	// classes from the subprogram must be visible to the main program. Merge
	// by remapping OpClass args to the new base offset.
	base := len(c.classes)
	for i := range prog.Code {
		if prog.Code[i].Op == OpClass {
			prog.Code[i].Arg += base
		}
	}
	c.classes = append(c.classes, prog.Classes...)
	prog.Classes = nil
	id := c.nextSub
	c.nextSub++
	for _, nested := range prog.Subs {
		for i := range nested.Code {
			if nested.Code[i].Op == OpClass {
				nested.Code[i].Arg += base
			}
		}
		c.classes = append(c.classes, nested.Classes...)
		nested.Classes = nil
	}
	c.subs[id] = prog
	lf := 0
	if t.Behind {
		lf |= LookBehind
	}
	if t.Neg {
		lf |= LookNegative
	}
	c.emit(Instr{Op: OpLook, Arg: id, Arg2: lf, Lo: t.SrcLo, Hi: t.SrcHi})
}
