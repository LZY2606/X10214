package compile

import "traceroom/syntax"

// compileRepeat emits backtracking loop code.
//
// Semantics:
//   - greedy:  continue iteration is the primary branch (tried first);
//   - lazy:    exit is the primary branch (OpSplit Arg2 == 1 tells engine to
//     swap the two priorities);
//   - possessive: the entire loop region is atomic; once an iteration choice
//     is committed it cannot be re-entered.
//
// Zero-width termination: a single LoopStart id anchors the position before
// the loop; LoopGuard after every body compares the new position with the
// anchor of the current iteration. LoopStart re-anchors each iteration, so
// e.g. (a*)* terminates: the inner loop may match empty once, then the outer
// LoopGuard detects no progress.
func (c *compiler) compileRepeat(t *syntax.Repeat, flags uint8) {
	lo, hi := t.SrcLo, t.SrcHi

	cutID := -1
	if t.Possessive {
		cutID = c.nextLoop
		c.nextLoop++
		c.emit(Instr{Op: OpOpenCut, Arg: cutID, Lo: lo, Hi: lo})
	}

	for i := 0; i < t.Min; i++ {
		c.emitNode(t.Kid, flags)
	}

	if t.Max == -1 || t.Max > t.Min {
		loop := c.nextLoop
		c.nextLoop++
		head := len(c.code)
		c.emit(Instr{Op: OpLoopStart, Arg: loop, Lo: lo, Hi: lo})
		// split primary=fallthrough(continue), secondary=exit
		split := c.emit(Instr{Op: OpSplit, Lo: lo, Hi: hi})
		if !t.Greedy {
			split = split // keep
		}
		c.emitNode(t.Kid, flags)
		c.emit(Instr{Op: OpLoopGuard, Arg: loop, Lo: lo, Hi: hi})
		back := c.emit(Instr{Op: OpJmp, Arg: head, Lo: lo, Hi: hi})
		exit := len(c.code)
		c.code[split].Arg = exit
		if !t.Greedy {
			c.code[split].Arg2 = 1
		}
		_ = back
	}

	if cutID >= 0 {
		c.emit(Instr{Op: OpCloseCut, Arg: cutID, Lo: hi, Hi: hi})
	}
}
