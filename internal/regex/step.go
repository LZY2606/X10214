package regex

// Step executes exactly one instruction and returns false when the run is
// halted or paused (breakpoint / budget). On a paused budget overrun the
// outcome is indeterminate and Frontier captures the resumable state.
func (v *VM) Step() bool {
	if v.halted || v.paused {
		return false
	}
	if v.bp[v.pc] {
		v.paused = true
		v.record(EvHalt, Instr{}, "breakpoint", "")
		return false
	}
	// Step budget is charged for each instruction attempted (including
	// backtracking failures counted separately in fail()).
	v.used.Steps++
	if v.budgets.Steps > 0 && v.used.Steps > v.budgets.Steps {
		return v.suspend("step budget exhausted")
	}
	if v.pc < 0 || v.pc >= len(v.prog.Code) {
		return v.fail("ran past program")
	}
	in := v.prog.Code[v.pc]
	switch in.Op {
	case OpChar:
		if v.pos < v.in.Len() && in.Match.matches(v.in.Code[v.pos]) {
			v.record(EvStep, in, "consume", "")
			v.pos++
			v.pc++
		} else {
			v.record(EvStep, in, "mismatch", "")
			v.failBack("char mismatch")
		}
	case OpJmp:
		v.record(EvStep, in, "jmp", "")
		v.pc = in.A
	case OpSplit:
		first, second := in.A, in.B
		branch := "A"
		if !in.PreferA {
			first, second = second, first
			branch = "B"
		}
		fr := &frame{kind: fSplit, pc: v.pc, altPC: second, trace: true}
		if !v.pushFrame(fr) {
			return v.suspend("state or snapshot budget exhausted at split")
		}
		v.record(EvChoose, in, "preferred branch "+branch, branch)
		v.pc = first
	case OpLoopBegin:
		if p, ok := v.loops[in.A]; ok && p == v.pos {
			// Body already entered an iteration at this exact position:
			// stop repeating to break a zero-width loop.
			v.record(EvStep, in, "zero-width guard", "")
			v.loopExit(in)
		} else {
			v.loops[in.A] = v.pos
			v.record(EvStep, in, "loop enter", "")
			v.pc++
		}
	case OpLoopEnd:
		v.record(EvStep, in, "loop repeat", "")
		v.pc = in.A
	case OpCapStart:
		v.setCap(in.A, v.pos, false)
		v.record(EvCapUpdate, in, "capture start", "")
		v.pc++
	case OpCapEnd:
		v.setCap(in.A, v.pos, true)
		v.record(EvCapUpdate, in, "capture end", "")
		v.pc++
	case OpAdvance:
		v.record(EvStep, in, "advance start", "")
		if v.pos >= v.in.Len() {
			v.failBack("no more start positions")
			return true
		}
		fr := &frame{kind: fSplit, pc: v.pc, altPC: v.pc, trace: false}
		if !v.pushFrame(fr) {
			return v.suspend("snapshot budget exhausted on advance")
		}
		v.pos++
		v.caps = make([]*[2]int, len(v.caps))
		v.loops = map[int]int{}
		v.pc = 0
	case OpMatch:
		v.finishMatch(in)
	case OpFail:
		v.failBack("explicit fail")
	case OpAnchor:
		if v.anchorOK(in) {
			v.record(EvStep, in, "anchor holds", "")
			v.pc++
		} else {
			v.record(EvStep, in, "anchor fails", "")
			v.failBack("anchor fails")
		}
	case OpBackref:
		if v.backrefOK(in) {
			v.record(EvStep, in, "backref matches", "")
		} else {
			v.record(EvStep, in, "backref fails", "")
			v.failBack("backref fails")
		}
	case OpCut:
		v.doCut(in)
	case OpLAStart, OpNLAStart, OpLBStart, OpNLBStart:
		v.startLook(in)
	case OpLAEnd, OpLBEnd:
		v.endLook(in)
	}
	return true
}

func (v *VM) loopExit(in Instr) {
	// jump to the split's exit: pattern is loopBegin [cut?] split body loopEnd exit.
	pc := v.pc + 1
	for pc < len(v.prog.Code) {
		if v.prog.Code[pc].Op == OpSplit {
			v.pc = v.prog.Code[pc].B
			return
		}
		if v.prog.Code[pc].Op == OpCut {
			pc++
			continue
		}
		pc++
	}
	v.failBack("malformed loop")
}
